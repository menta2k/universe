package server

import (
	"log/slog"
	"testing"
)

func TestNewLogger(t *testing.T) {
	if NewLogger(slog.LevelInfo) == nil {
		t.Fatal("NewLogger returned nil")
	}
}

func TestNewMetricsRegistersCollectors(t *testing.T) {
	m := NewMetrics()
	if m.Registry == nil || m.DHCPPackets == nil || m.TFTPTransfers == nil ||
		m.BootHTTP == nil || m.APIRequests == nil || m.SessionsTotal == nil {
		t.Fatal("metrics not fully initialized")
	}
	// Pre-initialized label series should be gatherable.
	families, err := m.Registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	if len(families) == 0 {
		t.Error("expected pre-registered metric families")
	}
}
