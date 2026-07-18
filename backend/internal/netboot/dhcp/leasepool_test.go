package dhcp

import (
	"net"
	"testing"
	"time"
)

// Pure allocation logic is unit-tested here; the Valkey-backed grant/scan
// paths are covered by the DHCP integration test against a real Valkey.

func mustCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIP4Arithmetic(t *testing.T) {
	start := net.ParseIP("192.168.90.10")
	if got := uintToIP4(ip4ToUint(start)); !got.Equal(start) {
		t.Errorf("round trip = %s, want %s", got, start)
	}
	next := uintToIP4(ip4ToUint(start) + 5)
	if next.String() != "192.168.90.15" {
		t.Errorf("start+5 = %s, want 192.168.90.15", next)
	}
}

func TestSubnetFor(t *testing.T) {
	pool := NewLeasePool(nil, []Subnet{{
		Network:    mustCIDR(t, "192.168.90.0/24"),
		RangeStart: net.ParseIP("192.168.90.10"),
		RangeEnd:   net.ParseIP("192.168.90.20"),
	}}, nil, time.Hour)

	if _, ok := pool.SubnetFor(net.ParseIP("192.168.90.15")); !ok {
		t.Error("expected 192.168.90.15 to be in subnet")
	}
	if _, ok := pool.SubnetFor(net.ParseIP("10.0.0.1")); ok {
		t.Error("10.0.0.1 must not match subnet")
	}
}

func TestReservationLookup(t *testing.T) {
	reservedIP := net.ParseIP("192.168.90.5")
	pool := NewLeasePool(nil, nil,
		[]Reservation{{MAC: "52:54:00:aa:bb:cc", IP: reservedIP}}, time.Hour)

	got, ok := pool.reservations["52:54:00:aa:bb:cc"]
	if !ok || !got.Equal(reservedIP) {
		t.Errorf("reservation not registered: got=%v ok=%v", got, ok)
	}
	if _, ok := pool.reservations["52:54:00:ff:ff:ff"]; ok {
		t.Error("unexpected reservation for unknown mac")
	}
}
