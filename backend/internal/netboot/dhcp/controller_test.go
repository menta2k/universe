package dhcp

import (
	"io"
	"log/slog"
	"testing"

	"github.com/menta2k/universe/backend/internal/biz"
)

func TestBuildPoolInputs(t *testing.T) {
	cfg := &biz.DhcpConfig{
		LeaseTTLSeconds: 3600,
		Subnets: []biz.DhcpSubnet{
			{Network: "192.168.90.0/24", RangeStart: "192.168.90.100",
				RangeEnd: "192.168.90.200", Gateway: "192.168.90.1", DNS: []string{"1.1.1.1", "bad"}},
			{Network: "not-a-cidr", RangeStart: "1.1.1.1", RangeEnd: "1.1.1.2"}, // skipped
		},
	}
	subnets, reservations := buildPoolInputs(cfg)
	if len(subnets) != 1 {
		t.Fatalf("expected 1 valid subnet, got %d", len(subnets))
	}
	if subnets[0].Gateway.String() != "192.168.90.1" {
		t.Errorf("gateway = %v", subnets[0].Gateway)
	}
	if len(subnets[0].DNS) != 1 { // "bad" dropped
		t.Errorf("expected 1 valid DNS, got %d", len(subnets[0].DNS))
	}
	if reservations != nil {
		t.Errorf("expected no reservations, got %v", reservations)
	}
}

func TestParseIPs(t *testing.T) {
	got := parseIPs([]string{"10.0.0.1", "garbage", "10.0.0.2"})
	if len(got) != 2 {
		t.Errorf("parseIPs kept %d, want 2", len(got))
	}
}

func TestControllerReloadDisabledIsNoop(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctl := NewController(Config{Interface: "lo", ServerIP: "127.0.0.1"},
		nil, nil, nil, nil, log)
	// Disabled config must not start a server (no panic, running stays false).
	ctl.Reload(&biz.DhcpConfig{Enabled: false})
	if ctl.running {
		t.Error("controller should not run for disabled config")
	}
	ctl.Reload(nil)
	if ctl.running {
		t.Error("controller should not run for nil config")
	}
	if err := ctl.Stop(t.Context()); err != nil {
		t.Errorf("stop: %v", err)
	}
}
