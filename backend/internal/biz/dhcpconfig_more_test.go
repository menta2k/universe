package biz

import (
	"context"
	"testing"
)

type fakeLeaseReader struct{ leases []Lease }

func (f *fakeLeaseReader) ListLeases(context.Context, int, int) ([]Lease, int64, error) {
	return f.leases, int64(len(f.leases)), nil
}

type fakeForeignReader struct{ servers []ForeignServer }

func (f *fakeForeignReader) ListForeignServers(context.Context, int, int) ([]ForeignServer, int64, error) {
	return f.servers, int64(len(f.servers)), nil
}

func TestDhcpUsecaseReaders(t *testing.T) {
	repo := &fakeDhcpRepo{cfg: &DhcpConfig{Version: 1, LeaseTTLSeconds: 3600}}
	leases := &fakeLeaseReader{leases: []Lease{{IP: "192.168.90.5", MAC: "aa:bb:cc:dd:ee:ff"}}}
	foreign := &fakeForeignReader{servers: []ForeignServer{{ServerID: "10.0.0.1", OffersSeen: 3}}}
	uc := NewDhcpConfigUsecase(repo, leases, foreign, nil, testLogger())

	if cfg, err := uc.Get(context.Background()); err != nil || cfg.Version != 1 {
		t.Errorf("get: %v %+v", err, cfg)
	}
	gotLeases, total, err := uc.ListLeases(context.Background(), 1, 50)
	if err != nil || total != 1 || len(gotLeases) != 1 {
		t.Errorf("list leases: %v total=%d", err, total)
	}
	gotServers, total, err := uc.ListForeignServers(context.Background(), 1, 50)
	if err != nil || total != 1 || gotServers[0].OffersSeen != 3 {
		t.Errorf("list foreign: %v %+v", err, gotServers)
	}
}

func TestDhcpUpdateDoesNotReloadWhenDisabled(t *testing.T) {
	repo := &fakeDhcpRepo{cfg: &DhcpConfig{Version: 1, LeaseTTLSeconds: 3600, Enabled: false}}
	n := &recordingNotifier{}
	uc := NewDhcpConfigUsecase(repo, nil, nil, n, testLogger())
	if _, err := uc.Update(context.Background(), 3600, []DhcpSubnet{goodSubnet()}); err != nil {
		t.Fatal(err)
	}
	if n.reloads != 0 {
		t.Errorf("disabled config must not reload on update, got %d", n.reloads)
	}
}
