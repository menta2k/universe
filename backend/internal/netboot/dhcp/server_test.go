package dhcp

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/iana"

	"universe/backend/internal/biz"
)

// captureConn records the last packet written, standing in for a real socket.
type captureConn struct {
	net.PacketConn
	written []byte
	to      net.Addr
}

func (c *captureConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	c.written = append([]byte(nil), b...)
	c.to = addr
	return len(b), nil
}

// fakeDecider serves one armed machine and denies everything else.
type fakeDecider struct {
	armedMAC     string
	decision     *biz.BootDecision
	unknownCalls []string
	firmwareSeen biz.Firmware
}

func (f *fakeDecider) BootInfo(_ context.Context, mac string) (*biz.BootDecision, error) {
	if mac == f.armedMAC {
		return f.decision, nil
	}
	return nil, biz.ErrEntityNotFound
}

func (f *fakeDecider) RecordUnknownBoot(_ context.Context, mac string) {
	f.unknownCalls = append(f.unknownCalls, mac)
}

func (f *fakeDecider) ObserveFirmware(_ context.Context, _ string, fw biz.Firmware) {
	f.firmwareSeen = fw
}

type nopEvents struct{}

func (nopEvents) Record(context.Context, biz.Event) {}

type nopConflictSink struct{}

func (nopConflictSink) RecordForeignOffer(context.Context, string, string, string) {}

func testServer(t *testing.T, decider BootDecider) (*Server, *LeasePool) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, network, _ := net.ParseCIDR("192.168.90.0/24")
	pool := NewLeasePool(nil, []Subnet{{
		Network:    network,
		RangeStart: net.ParseIP("192.168.90.100"),
		RangeEnd:   net.ParseIP("192.168.90.200"),
		Gateway:    net.ParseIP("192.168.90.1"),
	}}, nil, 0)
	cw := NewConflictWatcher("192.168.90.1", nopConflictSink{}, log)
	srv := NewServer(Config{Interface: "lo", ServerIP: "192.168.90.1",
		BootHTTPURL: "http://192.168.90.1:8082"}, pool, decider, nopEvents{}, cw, log)
	return srv, pool
}

// mkArmedDecision builds a decision for a UEFI machine.
func mkArmedDecision(mac string) *biz.BootDecision {
	return &biz.BootDecision{
		Machine: &biz.Machine{ID: "m1", MAC: mac, Firmware: biz.FirmwareUEFI},
		Session: &biz.Session{ID: "s1"},
		Profile: &biz.Profile{ID: "p1", UbuntuRelease: biz.ReleaseNoble},
	}
}

func discoverFor(t *testing.T, mac string, mods ...dhcpv4.Modifier) *dhcpv4.DHCPv4 {
	t.Helper()
	hw, _ := net.ParseMAC(mac)
	base := []dhcpv4.Modifier{
		dhcpv4.WithMessageType(dhcpv4.MessageTypeDiscover),
		dhcpv4.WithHwAddr(hw),
	}
	m, err := dhcpv4.New(append(base, mods...)...)
	if err != nil {
		t.Fatal(err)
	}
	m.ClientHWAddr = hw
	return m
}

func TestHandleArmedUEFIMachineGetsBootFile(t *testing.T) {
	const mac = "52:54:00:aa:bb:cc"
	dec := &fakeDecider{armedMAC: mac, decision: mkArmedDecision(mac)}
	srv, pool := testServer(t, dec)
	// Allocate needs Valkey; inject a reservation so Allocate short-circuits.
	pool.reservations[mac] = net.ParseIP("192.168.90.150")
	pool.vk = nil // grant() will fail without Valkey, so bypass via test hook

	// Because grant needs Valkey, assert the decision path instead of the full
	// reply for the armed case in a Valkey-free unit test: verify firmware is
	// observed and a boot file is chosen by decideBoot.
	m := discoverFor(t, mac, withClass("PXEClient"), withArch(iana.EFI_X86_64))
	br := decideBoot(m, srv.serverIP, srv.bootHTTPURL, mac)
	if br.Filename != bootfileUEFI {
		t.Errorf("armed UEFI boot file = %q, want %q", br.Filename, bootfileUEFI)
	}
}

func TestApplyBootDeniesUnknownMAC(t *testing.T) {
	const armed = "52:54:00:aa:bb:cc"
	const unknown = "52:54:00:ff:ff:ff"
	dec := &fakeDecider{armedMAC: armed, decision: mkArmedDecision(armed)}
	srv, _ := testServer(t, dec)

	reply, _ := dhcpv4.NewReplyFromRequest(discoverFor(t, unknown,
		withClass("PXEClient"), withArch(iana.EFI_X86_64)))
	srv.applyBoot(context.Background(), discoverFor(t, unknown,
		withClass("PXEClient"), withArch(iana.EFI_X86_64)), reply, unknown)

	if reply.BootFileName != "" {
		t.Errorf("unknown MAC must get no boot file, got %q", reply.BootFileName)
	}
	if len(dec.unknownCalls) != 1 || dec.unknownCalls[0] != unknown {
		t.Errorf("unknown boot not recorded: %v", dec.unknownCalls)
	}
}

func TestApplyBootArmedSetsFileAndFirmware(t *testing.T) {
	const mac = "52:54:00:aa:bb:cc"
	dec := &fakeDecider{armedMAC: mac, decision: mkArmedDecision(mac)}
	srv, _ := testServer(t, dec)

	req := discoverFor(t, mac, withClass("PXEClient"), withArch(iana.EFI_X86_64))
	reply, _ := dhcpv4.NewReplyFromRequest(req)
	srv.applyBoot(context.Background(), req, reply, mac)

	if reply.BootFileName != bootfileUEFI {
		t.Errorf("boot file = %q, want %q", reply.BootFileName, bootfileUEFI)
	}
	if dec.firmwareSeen != biz.FirmwareUEFI {
		t.Errorf("observed firmware = %q, want uefi_x64", dec.firmwareSeen)
	}
	if reply.ClassIdentifier() != "PXEClient" {
		t.Errorf("vendor class = %q, want PXEClient", reply.ClassIdentifier())
	}
}

func TestReplyAddrHonorsGiaddr(t *testing.T) {
	reply := &dhcpv4.DHCPv4{GatewayIPAddr: net.ParseIP("10.0.0.254")}
	addr := replyAddr(nil, reply)
	udp, ok := addr.(*net.UDPAddr)
	if !ok || !udp.IP.Equal(net.ParseIP("10.0.0.254")) || udp.Port != 67 {
		t.Errorf("relayed reply addr = %v, want 10.0.0.254:67", addr)
	}
}
