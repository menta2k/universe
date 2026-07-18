package dhcp

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/server4"

	"universe/backend/internal/biz"
)

// BootDecider resolves a booting MAC to its armed session (biz.MachineUsecase).
type BootDecider interface {
	BootInfo(ctx context.Context, mac string) (*biz.BootDecision, error)
	RecordUnknownBoot(ctx context.Context, mac string)
	ObserveFirmware(ctx context.Context, machineID string, fw biz.Firmware)
}

// EventSink records provisioning events (biz.EventRecorder).
type EventSink interface {
	Record(ctx context.Context, e biz.Event)
}

// Server is the authoritative DHCPv4 service. It only starts when explicitly
// enabled by the operator (FR-016); the caller guards on the enabled flag.
type Server struct {
	iface       string
	serverIP    string
	bootHTTPURL string
	pool        *LeasePool
	decider     BootDecider
	events      EventSink
	conflicts   *ConflictWatcher
	log         *slog.Logger

	srv *server4.Server
}

// Config holds the runtime parameters for a DHCP server instance.
type Config struct {
	Interface   string
	ServerIP    string
	BootHTTPURL string
	Addr        string // listen address, e.g. ":67"
}

func (c Config) dhcpAddr() string {
	if c.Addr == "" {
		return ":67"
	}
	return c.Addr
}

func NewServer(cfg Config, pool *LeasePool, decider BootDecider, events EventSink, conflicts *ConflictWatcher, log *slog.Logger) *Server {
	return &Server{
		iface: cfg.Interface, serverIP: cfg.ServerIP, bootHTTPURL: cfg.BootHTTPURL,
		pool: pool, decider: decider, events: events, conflicts: conflicts, log: log,
	}
}

// ListenAndServe binds :67 on the configured interface and serves until Shutdown.
func (s *Server) ListenAndServe(addr string) error {
	laddr, err := resolveUDP(addr)
	if err != nil {
		return err
	}
	srv, err := server4.NewServer(s.iface, laddr, s.handle)
	if err != nil {
		return fmt.Errorf("create dhcp server: %w", err)
	}
	s.srv = srv
	s.log.Info("dhcp server listening", "iface", s.iface, "addr", addr)
	return srv.Serve()
}

// Shutdown stops the server.
func (s *Server) Shutdown() {
	if s.srv != nil {
		_ = s.srv.Close()
	}
}

func resolveUDP(addr string) (*net.UDPAddr, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("parse dhcp addr %q: %w", addr, err)
	}
	ip := net.ParseIP(host)
	if host == "" {
		ip = net.IPv4zero
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return nil, fmt.Errorf("parse dhcp port %q: %w", portStr, err)
	}
	return &net.UDPAddr{IP: ip, Port: port}, nil
}

// handle processes one DHCP packet.
func (s *Server) handle(conn net.PacketConn, peer net.Addr, m *dhcpv4.DHCPv4) {
	if m == nil {
		return
	}
	// Passively watch for competing DHCP servers (FR-016).
	s.conflicts.Observe(m)

	ctx := context.Background()
	switch m.MessageType() {
	case dhcpv4.MessageTypeDiscover:
		s.respond(ctx, conn, peer, m, dhcpv4.MessageTypeOffer)
	case dhcpv4.MessageTypeRequest:
		s.respond(ctx, conn, peer, m, dhcpv4.MessageTypeAck)
	default:
		// Release/Decline/Inform are not handled in v1.
	}
}

func (s *Server) respond(ctx context.Context, conn net.PacketConn, peer net.Addr, m *dhcpv4.DHCPv4, msgType dhcpv4.MessageType) {
	mac := m.ClientHWAddr.String()

	reply, err := dhcpv4.NewReplyFromRequest(m)
	if err != nil {
		s.log.Error("build dhcp reply", "err", err, "mac", mac)
		return
	}
	reply.UpdateOption(dhcpv4.OptMessageType(msgType))
	reply.UpdateOption(dhcpv4.OptServerIdentifier(net.ParseIP(s.serverIP)))

	lease, err := s.pool.Allocate(ctx, mac, "")
	if err != nil {
		s.log.Warn("no lease available", "err", err, "mac", mac)
		return
	}
	reply.YourIPAddr = net.ParseIP(lease.IP)

	if subnet, ok := s.pool.SubnetFor(net.ParseIP(lease.IP)); ok {
		applySubnetOptions(reply, subnet)
	}

	s.applyBoot(ctx, m, reply, mac)
	s.recordDHCP(ctx, msgType, mac, lease.IP)

	if _, err := conn.WriteTo(reply.ToBytes(), replyAddr(peer, reply)); err != nil {
		s.log.Error("send dhcp reply", "err", err, "mac", mac)
	}
}

// applyBoot adds netboot options for armed machines (and denies unknowns).
func (s *Server) applyBoot(ctx context.Context, m, reply *dhcpv4.DHCPv4, mac string) {
	dec, err := s.decider.BootInfo(ctx, mac)
	if err != nil {
		if isUnknownMAC(err) {
			s.decider.RecordUnknownBoot(ctx, mac)
		}
		return // no boot options: plain lease
	}
	s.decider.ObserveFirmware(ctx, dec.Machine.ID, FirmwareOf(m.ClientArch()))

	br := decideBoot(m, s.serverIP, s.bootHTTPURL, mac)
	if br.Filename == "" {
		return
	}
	reply.BootFileName = br.Filename
	reply.UpdateOption(dhcpv4.OptBootFileName(br.Filename))
	if br.NextServer != "" {
		reply.ServerIPAddr = net.ParseIP(br.NextServer)
		reply.UpdateOption(dhcpv4.OptTFTPServerName(br.NextServer))
	}
	// Vendor class must be echoed as PXEClient in the offer (spec requirement).
	reply.UpdateOption(dhcpv4.OptClassIdentifier("PXEClient"))
}

func (s *Server) recordDHCP(ctx context.Context, msgType dhcpv4.MessageType, mac, ip string) {
	phase := biz.PhaseDHCPOffer
	if msgType == dhcpv4.MessageTypeAck {
		phase = biz.PhaseDHCPAck
	}
	s.events.Record(ctx, biz.Event{
		MachineMAC: mac, Phase: phase, Outcome: biz.OutcomeOK,
		Detail: map[string]any{"ip": ip},
	})
}

func applySubnetOptions(reply *dhcpv4.DHCPv4, s Subnet) {
	if mask := s.Network.Mask; mask != nil {
		reply.UpdateOption(dhcpv4.OptSubnetMask(mask))
	}
	if s.Gateway != nil {
		reply.UpdateOption(dhcpv4.OptRouter(s.Gateway))
	}
	if len(s.DNS) > 0 {
		reply.UpdateOption(dhcpv4.OptDNS(s.DNS...))
	}
}

// replyAddr honors giaddr for relayed requests, else broadcasts (RFC 2131).
func replyAddr(peer net.Addr, reply *dhcpv4.DHCPv4) net.Addr {
	if reply.GatewayIPAddr != nil && !reply.GatewayIPAddr.IsUnspecified() {
		return &net.UDPAddr{IP: reply.GatewayIPAddr, Port: 67}
	}
	if peer != nil {
		if udp, ok := peer.(*net.UDPAddr); ok && udp.IP != nil && !udp.IP.IsUnspecified() {
			return &net.UDPAddr{IP: net.IPv4bcast, Port: udp.Port}
		}
	}
	return &net.UDPAddr{IP: net.IPv4bcast, Port: 68}
}

func isUnknownMAC(err error) bool {
	return err != nil && err.Error() == biz.ErrEntityNotFound.Error()
}
