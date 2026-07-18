package biz

import (
	"context"
	"errors"
	"testing"
)

type fakeDhcpRepo struct {
	cfg         *DhcpConfig
	replaceErr  error
	replaceSeen bool
}

func (f *fakeDhcpRepo) Get(context.Context) (*DhcpConfig, error) { return f.cfg, nil }

func (f *fakeDhcpRepo) Replace(_ context.Context, ttl int, subnets []DhcpSubnet) (*DhcpConfig, error) {
	if f.replaceErr != nil {
		return nil, f.replaceErr
	}
	f.replaceSeen = true
	f.cfg = &DhcpConfig{Enabled: f.cfg.Enabled, Version: f.cfg.Version + 1, LeaseTTLSeconds: ttl, Subnets: subnets}
	return f.cfg, nil
}

func (f *fakeDhcpRepo) SetEnabled(_ context.Context, enabled bool) (*DhcpConfig, error) {
	f.cfg.Enabled = enabled
	return f.cfg, nil
}

type recordingNotifier struct{ reloads int }

func (r *recordingNotifier) Reload(*DhcpConfig) { r.reloads++ }

func newDhcpUC(t *testing.T) (*DhcpConfigUsecase, *fakeDhcpRepo, *recordingNotifier) {
	t.Helper()
	repo := &fakeDhcpRepo{cfg: &DhcpConfig{Version: 1, LeaseTTLSeconds: 3600}}
	n := &recordingNotifier{}
	return NewDhcpConfigUsecase(repo, nil, nil, n, testLogger()), repo, n
}

func goodSubnet() DhcpSubnet {
	return DhcpSubnet{Network: "192.168.90.0/24", RangeStart: "192.168.90.100",
		RangeEnd: "192.168.90.200", Gateway: "192.168.90.1"}
}

func TestDhcpValidation(t *testing.T) {
	uc, repo, _ := newDhcpUC(t)
	cases := []struct {
		name    string
		ttl     int
		subnets []DhcpSubnet
		field   string
	}{
		{"ttl too low", 60, []DhcpSubnet{goodSubnet()}, "lease_ttl_seconds"},
		{"range outside subnet", 3600, []DhcpSubnet{{Network: "192.168.90.0/24", RangeStart: "10.0.0.1", RangeEnd: "10.0.0.9"}}, "subnets[0].range"},
		{"reversed range", 3600, []DhcpSubnet{{Network: "192.168.90.0/24", RangeStart: "192.168.90.200", RangeEnd: "192.168.90.100"}}, "subnets[0].range"},
		{"bad cidr", 3600, []DhcpSubnet{{Network: "nonsense", RangeStart: "1.1.1.1", RangeEnd: "1.1.1.2"}}, "subnets[0].network"},
		{"overlap", 3600, []DhcpSubnet{{Network: "192.168.0.0/16", RangeStart: "192.168.1.1", RangeEnd: "192.168.1.9"}, {Network: "192.168.90.0/24", RangeStart: "192.168.90.1", RangeEnd: "192.168.90.9"}}, "subnets[1].network"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := uc.Update(context.Background(), tc.ttl, tc.subnets)
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("expected ValidationError, got %v", err)
			}
			if ve.Fields[tc.field] == "" {
				t.Errorf("expected field %q in %v", tc.field, ve.Fields)
			}
			if repo.replaceSeen {
				t.Error("invalid config must not reach the repo (last-valid-config)")
			}
		})
	}
}

func TestDhcpUpdateAppliesAndReloadsWhenEnabled(t *testing.T) {
	uc, repo, n := newDhcpUC(t)
	repo.cfg.Enabled = true
	cfg, err := uc.Update(context.Background(), 7200, []DhcpSubnet{goodSubnet()})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if cfg.Version != 2 || cfg.LeaseTTLSeconds != 7200 {
		t.Errorf("unexpected cfg: %+v", cfg)
	}
	if n.reloads != 1 {
		t.Errorf("expected reload when enabled, got %d", n.reloads)
	}
}

func TestDhcpEnableDisable(t *testing.T) {
	uc, _, n := newDhcpUC(t)
	if cfg, _ := uc.Enable(context.Background()); !cfg.Enabled {
		t.Error("enable did not set flag")
	}
	if cfg, _ := uc.Disable(context.Background()); cfg.Enabled {
		t.Error("disable did not clear flag")
	}
	if n.reloads != 2 {
		t.Errorf("enable+disable should each reload, got %d", n.reloads)
	}
}
