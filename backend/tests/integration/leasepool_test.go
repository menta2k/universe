package integration

import (
	"context"
	"net"
	"testing"
	"time"

	"universe/backend/internal/netboot/dhcp"
	"universe/backend/tests/integration/testenv"
)

// leaseSubnets is a small pool: two usable addresses (.100 and .101).
func leaseSubnets(t *testing.T) []dhcp.Subnet {
	t.Helper()
	_, network, err := net.ParseCIDR("192.168.90.0/24")
	if err != nil {
		t.Fatal(err)
	}
	return []dhcp.Subnet{{
		Network:    network,
		RangeStart: net.ParseIP("192.168.90.100"),
		RangeEnd:   net.ParseIP("192.168.90.101"),
		Gateway:    net.ParseIP("192.168.90.1"),
		DNS:        []net.IP{net.ParseIP("1.1.1.1")},
	}}
}

func vkGet(t *testing.T, env *testenv.Env, key string) (string, bool) {
	t.Helper()
	v, err := env.Data.Valkey.Do(context.Background(),
		env.Data.Valkey.B().Get().Key(key).Build()).ToString()
	if err != nil {
		return "", false
	}
	return v, true
}

func vkTTL(t *testing.T, env *testenv.Env, key string) int64 {
	t.Helper()
	ttl, err := env.Data.Valkey.Do(context.Background(),
		env.Data.Valkey.B().Ttl().Key(key).Build()).AsInt64()
	if err != nil {
		t.Fatalf("ttl %s: %v", key, err)
	}
	return ttl
}

func TestLeasePoolAllocate(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	const mac = "52:54:00:aa:bb:cc"

	pool := dhcp.NewLeasePool(env.Data.Valkey, leaseSubnets(t), nil, time.Hour)

	// First allocation for a MAC grants the first free range address and writes
	// both the forward lease and the reverse MAC index.
	lease, err := pool.Allocate(ctx, mac, "m1")
	if err != nil {
		t.Fatalf("first allocate: %v", err)
	}
	if lease.IP != "192.168.90.100" {
		t.Errorf("first free ip = %q, want 192.168.90.100", lease.IP)
	}
	if lease.MAC != mac || lease.MachineID != "m1" {
		t.Errorf("lease fields = %+v", lease)
	}

	// lease:<ip> holds the JSON, lease:mac:<mac> holds the ip.
	if _, ok := vkGet(t, env, "lease:192.168.90.100"); !ok {
		t.Error("lease:<ip> key not written")
	}
	if ip, ok := vkGet(t, env, "lease:mac:"+mac); !ok || ip != "192.168.90.100" {
		t.Errorf("lease:mac index = %q ok=%v, want 192.168.90.100", ip, ok)
	}

	// TTL is set on both keys (> 0, <= configured hour).
	if ttl := vkTTL(t, env, "lease:192.168.90.100"); ttl <= 0 || ttl > 3600 {
		t.Errorf("lease ttl = %d, want (0,3600]", ttl)
	}
	if ttl := vkTTL(t, env, "lease:mac:"+mac); ttl <= 0 || ttl > 3600 {
		t.Errorf("mac-index ttl = %d, want (0,3600]", ttl)
	}

	// Re-allocating the SAME mac returns the SAME ip (currentForMAC path).
	again, err := pool.Allocate(ctx, mac, "m1")
	if err != nil {
		t.Fatalf("second allocate: %v", err)
	}
	if again.IP != lease.IP {
		t.Errorf("re-allocate ip = %q, want stable %q", again.IP, lease.IP)
	}
}

func TestLeasePoolReservationAlwaysWins(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	const mac = "52:54:00:re:se:rv"
	reservedIP := net.ParseIP("192.168.90.50")

	pool := dhcp.NewLeasePool(env.Data.Valkey, leaseSubnets(t),
		[]dhcp.Reservation{{MAC: mac, IP: reservedIP}}, time.Hour)

	// Reserved MAC always gets its reserved IP, not a range address.
	lease, err := pool.Allocate(ctx, mac, "res")
	if err != nil {
		t.Fatalf("reserved allocate: %v", err)
	}
	if lease.IP != "192.168.90.50" {
		t.Errorf("reserved ip = %q, want 192.168.90.50", lease.IP)
	}

	// Even a second call keeps the reservation (reservation short-circuits
	// before currentForMAC).
	again, err := pool.Allocate(ctx, mac, "res")
	if err != nil {
		t.Fatalf("reserved re-allocate: %v", err)
	}
	if again.IP != "192.168.90.50" {
		t.Errorf("reserved re-allocate ip = %q, want 192.168.90.50", again.IP)
	}
}

func TestLeasePoolDistinctMACsGetDistinctIPs(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()

	pool := dhcp.NewLeasePool(env.Data.Valkey, leaseSubnets(t), nil, time.Hour)

	a, err := pool.Allocate(ctx, "52:54:00:00:00:01", "a")
	if err != nil {
		t.Fatalf("allocate a: %v", err)
	}
	b, err := pool.Allocate(ctx, "52:54:00:00:00:02", "b")
	if err != nil {
		t.Fatalf("allocate b: %v", err)
	}
	if a.IP == b.IP {
		t.Fatalf("two MACs got the same ip %q (double allocation)", a.IP)
	}
	if a.IP != "192.168.90.100" || b.IP != "192.168.90.101" {
		t.Errorf("nextFree did not advance: a=%q b=%q", a.IP, b.IP)
	}
}

func TestLeasePoolExhaustion(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()

	_, network, _ := net.ParseCIDR("192.168.90.0/24")
	single := []dhcp.Subnet{{
		Network:    network,
		RangeStart: net.ParseIP("192.168.90.100"),
		RangeEnd:   net.ParseIP("192.168.90.100"), // one address only
	}}
	pool := dhcp.NewLeasePool(env.Data.Valkey, single, nil, time.Hour)

	if _, err := pool.Allocate(ctx, "52:54:00:00:00:11", "first"); err != nil {
		t.Fatalf("first allocate should succeed: %v", err)
	}
	// The sole address is taken; a different MAC exhausts the pool.
	if _, err := pool.Allocate(ctx, "52:54:00:00:00:22", "second"); err == nil {
		t.Fatal("expected pool exhaustion error, got nil")
	}
}
