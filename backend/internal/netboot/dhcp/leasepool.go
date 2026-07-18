package dhcp

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/valkey-io/valkey-go"
)

// Subnet is one address range with its boot/network options.
type Subnet struct {
	Network    *net.IPNet
	RangeStart net.IP
	RangeEnd   net.IP
	Gateway    net.IP
	DNS        []net.IP
}

// Reservation binds a MAC to a fixed IP.
type Reservation struct {
	MAC string
	IP  net.IP
}

// Lease is an assigned address with ownership and expiry.
type Lease struct {
	IP        string    `json:"ip"`
	MAC       string    `json:"mac"`
	MachineID string    `json:"machine_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// LeasePool allocates addresses from configured subnets, honoring
// reservations, with active leases stored in Valkey (TTL = lease time).
type LeasePool struct {
	vk           valkey.Client
	subnets      []Subnet
	reservations map[string]net.IP // MAC -> reserved IP
	ttl          time.Duration
}

func NewLeasePool(vk valkey.Client, subnets []Subnet, reservations []Reservation, ttl time.Duration) *LeasePool {
	resMap := make(map[string]net.IP, len(reservations))
	for _, r := range reservations {
		resMap[r.MAC] = r.IP
	}
	return &LeasePool{vk: vk, subnets: subnets, reservations: resMap, ttl: ttl}
}

func leaseKey(ip string) string { return "lease:" + ip }
func macKey(mac string) string  { return "lease:mac:" + mac }

// Allocate returns an address for mac: its reservation if any, otherwise its
// current lease, otherwise the next free address in a matching subnet.
func (p *LeasePool) Allocate(ctx context.Context, mac, machineID string) (*Lease, error) {
	if ip, ok := p.reservations[mac]; ok {
		return p.grant(ctx, ip, mac, machineID)
	}
	if existing, err := p.currentForMAC(ctx, mac); err == nil {
		return p.grant(ctx, net.ParseIP(existing.IP), mac, machineID)
	}
	ip, err := p.nextFree(ctx)
	if err != nil {
		return nil, err
	}
	return p.grant(ctx, ip, mac, machineID)
}

// grant writes the lease and its reverse index with the TTL.
func (p *LeasePool) grant(ctx context.Context, ip net.IP, mac, machineID string) (*Lease, error) {
	if ip == nil {
		return nil, fmt.Errorf("no address available")
	}
	lease := &Lease{
		IP: ip.String(), MAC: mac, MachineID: machineID,
		ExpiresAt: time.Now().Add(p.ttl).UTC(),
	}
	raw, err := json.Marshal(lease)
	if err != nil {
		return nil, fmt.Errorf("marshal lease: %w", err)
	}
	if err := p.vk.Do(ctx, p.vk.B().Set().Key(leaseKey(lease.IP)).Value(string(raw)).Ex(p.ttl).Build()).Error(); err != nil {
		return nil, fmt.Errorf("store lease: %w", err)
	}
	if err := p.vk.Do(ctx, p.vk.B().Set().Key(macKey(mac)).Value(lease.IP).Ex(p.ttl).Build()).Error(); err != nil {
		return nil, fmt.Errorf("store lease index: %w", err)
	}
	return lease, nil
}

func (p *LeasePool) currentForMAC(ctx context.Context, mac string) (*Lease, error) {
	ip, err := p.vk.Do(ctx, p.vk.B().Get().Key(macKey(mac)).Build()).ToString()
	if err != nil {
		return nil, fmt.Errorf("no current lease")
	}
	return &Lease{IP: ip, MAC: mac}, nil
}

// nextFree scans subnet ranges for an unleased address.
func (p *LeasePool) nextFree(ctx context.Context) (net.IP, error) {
	for _, s := range p.subnets {
		start := ip4ToUint(s.RangeStart)
		end := ip4ToUint(s.RangeEnd)
		for v := start; v <= end && v != 0; v++ {
			ip := uintToIP4(v)
			exists, err := p.vk.Do(ctx, p.vk.B().Exists().Key(leaseKey(ip.String())).Build()).AsBool()
			if err != nil {
				return nil, fmt.Errorf("check lease: %w", err)
			}
			if !exists {
				return ip, nil
			}
		}
	}
	return nil, fmt.Errorf("address pool exhausted")
}

// SubnetFor returns the subnet containing ip, if any.
func (p *LeasePool) SubnetFor(ip net.IP) (Subnet, bool) {
	for _, s := range p.subnets {
		if s.Network.Contains(ip) {
			return s, true
		}
	}
	return Subnet{}, false
}

func ip4ToUint(ip net.IP) uint32 {
	v4 := ip.To4()
	if v4 == nil {
		return 0
	}
	return binary.BigEndian.Uint32(v4)
}

func uintToIP4(v uint32) net.IP {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return net.IP(b)
}
