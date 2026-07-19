package dhcp

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/iana"

	"github.com/menta2k/universe/backend/internal/biz"
	"github.com/menta2k/universe/backend/tests/integration/testenv"
)

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// valkeyServer wires the shared captureConn/fakeDecider helpers to a Server
// backed by a real Valkey lease pool, so respond()/handle() can be exercised
// end to end without opening a raw DHCP socket (server4 needs privileges/an
// interface that is not available in CI).
func valkeyServer(t *testing.T, decider BootDecider) (*Server, *captureEvents) {
	t.Helper()
	vk := testenv.StartValkey(t)
	log := discardLog()
	_, network, _ := net.ParseCIDR("192.168.90.0/24")
	pool := NewLeasePool(vk, []Subnet{{
		Network:    network,
		RangeStart: net.ParseIP("192.168.90.100"),
		RangeEnd:   net.ParseIP("192.168.90.200"),
		Gateway:    net.ParseIP("192.168.90.1"),
		DNS:        []net.IP{net.ParseIP("1.1.1.1")},
	}}, nil, time.Hour)
	events := &captureEvents{}
	cw := NewConflictWatcher("192.168.90.1", nopConflictSink{}, log)
	srv := NewServer(Config{Interface: "lo", ServerIP: "192.168.90.1",
		BootHTTPURL: "http://192.168.90.1:8082"}, pool, decider, events, cw, log)
	return srv, events
}

func TestRespondArmedMachineFullReply(t *testing.T) {
	const mac = "52:54:00:aa:bb:cc"
	dec := &fakeDecider{armedMAC: mac, decision: mkArmedDecision(mac)}
	srv, events := valkeyServer(t, dec)

	req := discoverFor(t, mac, withClass("PXEClient"), withArch(iana.EFI_X86_64))
	conn := &captureConn{}
	peer := &net.UDPAddr{IP: net.IPv4zero, Port: 68}

	srv.respond(context.Background(), conn, peer, req, dhcpv4.MessageTypeOffer)

	if len(conn.written) == 0 {
		t.Fatal("respond wrote no reply")
	}
	reply, err := dhcpv4.FromBytes(conn.written)
	if err != nil {
		t.Fatalf("parse written reply: %v", err)
	}

	// Address comes from the range.
	yip := reply.YourIPAddr
	if yip == nil || !yip.Equal(net.ParseIP("192.168.90.100")) {
		t.Errorf("YourIPAddr = %v, want 192.168.90.100", yip)
	}
	// Subnet options applied.
	if routers := reply.Router(); len(routers) != 1 || !routers[0].Equal(net.ParseIP("192.168.90.1")) {
		t.Errorf("router = %v, want [192.168.90.1]", routers)
	}
	// Boot file for an armed UEFI machine.
	if reply.BootFileName != bootfileUEFI {
		t.Errorf("boot file = %q, want %q", reply.BootFileName, bootfileUEFI)
	}
	if reply.ServerIdentifier() == nil || !reply.ServerIdentifier().Equal(net.ParseIP("192.168.90.1")) {
		t.Errorf("server id = %v, want 192.168.90.1", reply.ServerIdentifier())
	}
	// Firmware observed and an OFFER event recorded.
	if dec.firmwareSeen == "" {
		t.Error("firmware was not observed for armed machine")
	}
	if len(events.events) != 1 || events.events[0].Phase != biz.PhaseDHCPOffer {
		t.Errorf("events = %+v, want a single dhcp_offer", events.events)
	}
}

func TestRespondUnknownMachineGetsPlainLease(t *testing.T) {
	const armed = "52:54:00:aa:bb:cc"
	const unknown = "52:54:00:ff:ff:ff"
	dec := &fakeDecider{armedMAC: armed, decision: mkArmedDecision(armed)}
	srv, _ := valkeyServer(t, dec)

	req := discoverFor(t, unknown, withClass("PXEClient"), withArch(iana.EFI_X86_64))
	conn := &captureConn{}
	srv.respond(context.Background(), conn, &net.UDPAddr{IP: net.IPv4zero, Port: 68},
		req, dhcpv4.MessageTypeOffer)

	reply, err := dhcpv4.FromBytes(conn.written)
	if err != nil {
		t.Fatalf("parse written reply: %v", err)
	}
	if reply.YourIPAddr == nil || reply.YourIPAddr.IsUnspecified() {
		t.Error("unknown machine should still get an address lease")
	}
	if reply.BootFileName != "" {
		t.Errorf("unknown machine must get no boot file, got %q", reply.BootFileName)
	}
	if len(dec.unknownCalls) != 1 || dec.unknownCalls[0] != unknown {
		t.Errorf("unknown boot not recorded: %v", dec.unknownCalls)
	}
}

func TestHandleRequestEmitsAck(t *testing.T) {
	const mac = "52:54:00:aa:bb:cc"
	dec := &fakeDecider{armedMAC: mac, decision: mkArmedDecision(mac)}
	srv, events := valkeyServer(t, dec)

	// A REQUEST routes through handle() to an ACK reply.
	hw, _ := net.ParseMAC(mac)
	req, err := dhcpv4.New(
		dhcpv4.WithMessageType(dhcpv4.MessageTypeRequest),
		dhcpv4.WithHwAddr(hw),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.ClientHWAddr = hw

	conn := &captureConn{}
	srv.handle(conn, &net.UDPAddr{IP: net.IPv4zero, Port: 68}, req)

	if len(conn.written) == 0 {
		t.Fatal("handle wrote no reply for REQUEST")
	}
	if len(events.events) != 1 || events.events[0].Phase != biz.PhaseDHCPAck {
		t.Errorf("events = %+v, want a single dhcp_ack", events.events)
	}
}

func TestHandleNilAndUnhandledTypesAreNoops(t *testing.T) {
	dec := &fakeDecider{}
	srv, events := valkeyServer(t, dec)
	conn := &captureConn{}

	srv.handle(conn, nil, nil) // nil packet: early return
	if len(conn.written) != 0 {
		t.Error("nil packet should not produce a reply")
	}

	// A RELEASE is observed but not answered in v1.
	hw, _ := net.ParseMAC("52:54:00:00:00:aa")
	rel, _ := dhcpv4.New(dhcpv4.WithMessageType(dhcpv4.MessageTypeRelease), dhcpv4.WithHwAddr(hw))
	rel.ClientHWAddr = hw
	srv.handle(conn, &net.UDPAddr{IP: net.IPv4zero, Port: 68}, rel)
	if len(conn.written) != 0 {
		t.Error("RELEASE should not produce a reply")
	}
	if len(events.events) != 0 {
		t.Errorf("unhandled types should record no events, got %+v", events.events)
	}
}
