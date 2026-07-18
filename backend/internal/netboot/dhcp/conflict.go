package dhcp

import (
	"context"
	"log/slog"
	"net"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

// ConflictSink records observed foreign DHCP offers (biz-backed).
type ConflictSink interface {
	RecordForeignOffer(ctx context.Context, serverID, clientMAC, offeredIP string)
}

// ConflictWatcher flags competing DHCP servers on the segment (FR-016).
type ConflictWatcher struct {
	ourServerID string
	sink        ConflictSink
	log         *slog.Logger
}

func NewConflictWatcher(ourServerID string, sink ConflictSink, log *slog.Logger) *ConflictWatcher {
	return &ConflictWatcher{ourServerID: ourServerID, sink: sink, log: log}
}

// Observe inspects an incoming packet for OFFER/ACK from another server id.
func (w *ConflictWatcher) Observe(m *dhcpv4.DHCPv4) {
	if w == nil || m == nil {
		return
	}
	switch m.MessageType() {
	case dhcpv4.MessageTypeOffer, dhcpv4.MessageTypeAck:
	default:
		return
	}
	sid := m.ServerIdentifier()
	if sid == nil || sid.IsUnspecified() || sid.String() == w.ourServerID {
		return
	}
	offered := ""
	if m.YourIPAddr != nil {
		offered = m.YourIPAddr.String()
	}
	w.log.Warn("foreign dhcp server detected",
		"server_id", sid.String(), "client", m.ClientHWAddr.String())
	w.sink.RecordForeignOffer(context.Background(), sid.String(), m.ClientHWAddr.String(), offered)
}

var _ = net.IPv4zero
