package dhcp

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

type captureSink struct{ calls []string }

func (c *captureSink) RecordForeignOffer(_ context.Context, serverID, mac, ip string) {
	c.calls = append(c.calls, serverID+"|"+mac+"|"+ip)
}

func TestConflictWatcherObserve(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sink := &captureSink{}
	w := NewConflictWatcher("192.168.90.1", sink, log)

	hw, _ := net.ParseMAC("52:54:00:aa:bb:cc")

	// A foreign OFFER (different server id) is recorded.
	foreign, _ := dhcpv4.New(
		dhcpv4.WithMessageType(dhcpv4.MessageTypeOffer),
		dhcpv4.WithHwAddr(hw),
		dhcpv4.WithOption(dhcpv4.OptServerIdentifier(net.ParseIP("192.168.90.254"))),
	)
	foreign.YourIPAddr = net.ParseIP("192.168.90.50")
	w.Observe(foreign)
	if len(sink.calls) != 1 {
		t.Fatalf("expected 1 foreign offer recorded, got %d", len(sink.calls))
	}

	// Our own server id is ignored.
	ours, _ := dhcpv4.New(
		dhcpv4.WithMessageType(dhcpv4.MessageTypeOffer),
		dhcpv4.WithHwAddr(hw),
		dhcpv4.WithOption(dhcpv4.OptServerIdentifier(net.ParseIP("192.168.90.1"))),
	)
	w.Observe(ours)
	if len(sink.calls) != 1 {
		t.Error("own server id must not be recorded as a conflict")
	}

	// A DISCOVER (not offer/ack) is ignored, and nil is safe.
	disc, _ := dhcpv4.New(dhcpv4.WithMessageType(dhcpv4.MessageTypeDiscover), dhcpv4.WithHwAddr(hw))
	w.Observe(disc)
	w.Observe(nil)
	if len(sink.calls) != 1 {
		t.Error("non-offer/nil packets must not record conflicts")
	}
}
