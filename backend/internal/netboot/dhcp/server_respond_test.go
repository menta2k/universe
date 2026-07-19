package dhcp

import (
	"context"
	"net"
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv4"

	"github.com/menta2k/universe/backend/internal/biz"
)

// captureEvents records every event handed to Record for assertions.
type captureEvents struct {
	events []biz.Event
}

func (c *captureEvents) Record(_ context.Context, e biz.Event) {
	c.events = append(c.events, e)
}

func TestDhcpAddrDefault(t *testing.T) {
	if got := (Config{}).dhcpAddr(); got != ":67" {
		t.Errorf("empty addr = %q, want :67", got)
	}
	if got := (Config{Addr: "127.0.0.1:6767"}).dhcpAddr(); got != "127.0.0.1:6767" {
		t.Errorf("explicit addr = %q, want 127.0.0.1:6767", got)
	}
}

func TestResolveUDP(t *testing.T) {
	t.Run("host and port", func(t *testing.T) {
		got, err := resolveUDP("127.0.0.1:67")
		if err != nil {
			t.Fatalf("resolveUDP: %v", err)
		}
		if !got.IP.Equal(net.ParseIP("127.0.0.1")) || got.Port != 67 {
			t.Errorf("resolved = %v, want 127.0.0.1:67", got)
		}
	})
	t.Run("empty host means all interfaces", func(t *testing.T) {
		got, err := resolveUDP(":67")
		if err != nil {
			t.Fatalf("resolveUDP: %v", err)
		}
		if !got.IP.Equal(net.IPv4zero) || got.Port != 67 {
			t.Errorf("resolved = %v, want 0.0.0.0:67", got)
		}
	})
	t.Run("missing port separator is an error", func(t *testing.T) {
		if _, err := resolveUDP("nonsense"); err == nil {
			t.Error("expected error for addr without port")
		}
	})
	t.Run("non-numeric port is an error", func(t *testing.T) {
		if _, err := resolveUDP("127.0.0.1:http"); err == nil {
			t.Error("expected error for non-numeric port")
		}
	})
}

func TestApplySubnetOptions(t *testing.T) {
	_, network, _ := net.ParseCIDR("192.168.90.0/24")
	subnet := Subnet{
		Network: network,
		Gateway: net.ParseIP("192.168.90.1"),
		DNS:     []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("8.8.8.8")},
	}
	reply, _ := dhcpv4.New()
	applySubnetOptions(reply, subnet)

	if mask := reply.SubnetMask(); mask == nil || net.IP(mask).String() != "255.255.255.0" {
		t.Errorf("subnet mask = %v, want 255.255.255.0", mask)
	}
	routers := reply.Router()
	if len(routers) != 1 || !routers[0].Equal(net.ParseIP("192.168.90.1")) {
		t.Errorf("router = %v, want [192.168.90.1]", routers)
	}
	dns := reply.DNS()
	if len(dns) != 2 || !dns[0].Equal(net.ParseIP("1.1.1.1")) || !dns[1].Equal(net.ParseIP("8.8.8.8")) {
		t.Errorf("dns = %v, want [1.1.1.1 8.8.8.8]", dns)
	}
}

func TestApplySubnetOptionsOmitsEmptyFields(t *testing.T) {
	_, network, _ := net.ParseCIDR("10.0.0.0/24")
	reply, _ := dhcpv4.New()
	applySubnetOptions(reply, Subnet{Network: network}) // no gateway, no dns

	if len(reply.Router()) != 0 {
		t.Errorf("router should be unset, got %v", reply.Router())
	}
	if len(reply.DNS()) != 0 {
		t.Errorf("dns should be unset, got %v", reply.DNS())
	}
}

func TestRecordDHCPEmitsPhasePerMessageType(t *testing.T) {
	cases := []struct {
		name    string
		msgType dhcpv4.MessageType
		want    biz.Phase
	}{
		{"discover -> offer phase", dhcpv4.MessageTypeOffer, biz.PhaseDHCPOffer},
		{"request -> ack phase", dhcpv4.MessageTypeAck, biz.PhaseDHCPAck},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ce := &captureEvents{}
			srv := &Server{events: ce}
			srv.recordDHCP(context.Background(), tc.msgType, "52:54:00:aa:bb:cc", "192.168.90.100")

			if len(ce.events) != 1 {
				t.Fatalf("recorded %d events, want 1", len(ce.events))
			}
			e := ce.events[0]
			if e.Phase != tc.want {
				t.Errorf("phase = %v, want %v", e.Phase, tc.want)
			}
			if e.MachineMAC != "52:54:00:aa:bb:cc" || e.Outcome != biz.OutcomeOK {
				t.Errorf("event = %+v", e)
			}
			if ip, _ := e.Detail["ip"].(string); ip != "192.168.90.100" {
				t.Errorf("detail ip = %v, want 192.168.90.100", e.Detail["ip"])
			}
		})
	}
}

func TestReplyAddrBroadcastPaths(t *testing.T) {
	t.Run("unicast peer broadcasts to its port", func(t *testing.T) {
		reply, _ := dhcpv4.New()
		peer := &net.UDPAddr{IP: net.ParseIP("192.168.90.100"), Port: 68}
		addr := replyAddr(peer, reply).(*net.UDPAddr)
		if !addr.IP.Equal(net.IPv4bcast) || addr.Port != 68 {
			t.Errorf("addr = %v, want 255.255.255.255:68", addr)
		}
	})
	t.Run("nil peer broadcasts to bootpc", func(t *testing.T) {
		reply, _ := dhcpv4.New()
		addr := replyAddr(nil, reply).(*net.UDPAddr)
		if !addr.IP.Equal(net.IPv4bcast) || addr.Port != 68 {
			t.Errorf("addr = %v, want 255.255.255.255:68", addr)
		}
	})
	t.Run("unspecified peer ip falls back to bootpc broadcast", func(t *testing.T) {
		reply, _ := dhcpv4.New()
		peer := &net.UDPAddr{IP: net.IPv4zero, Port: 68}
		addr := replyAddr(peer, reply).(*net.UDPAddr)
		if !addr.IP.Equal(net.IPv4bcast) || addr.Port != 68 {
			t.Errorf("addr = %v, want 255.255.255.255:68", addr)
		}
	})
}
