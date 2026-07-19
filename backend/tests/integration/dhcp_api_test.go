package integration

import (
	"context"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"

	v1 "github.com/menta2k/universe/backend/api/netboot/v1"
	"github.com/menta2k/universe/backend/internal/biz"
	"github.com/menta2k/universe/backend/internal/data"
	"github.com/menta2k/universe/backend/internal/service"
	"github.com/menta2k/universe/backend/tests/integration/testenv"
)

func newDhcpService(t *testing.T, env *testenv.Env) *service.DhcpService {
	t.Helper()
	repo := data.NewDhcpConfigRepo(env.Data)
	uc := biz.NewDhcpConfigUsecase(repo, data.NewLeaseRepo(env.Data), repo, nil, testLog())
	return service.NewDhcpService(uc)
}

func TestDhcpServiceContract(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	svc := newDhcpService(t, env)

	// Defaults: disabled (FR-016).
	cfg, err := svc.GetDhcpConfig(ctx, nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if cfg.Enabled {
		t.Error("dhcp must default to disabled")
	}

	// Invalid update -> 422; running config keeps its previous version.
	_, err = svc.UpdateDhcpConfig(ctx, &v1.UpdateDhcpConfigRequest{
		LeaseTtlSeconds: 3600,
		Subnets: []*v1.DhcpSubnet{{
			Network: "192.168.90.0/24", RangeStart: "10.0.0.1", RangeEnd: "10.0.0.9"}},
	})
	if r := kerrors.FromError(err).Reason; r != "VALIDATION_FAILED" {
		t.Errorf("invalid update reason = %s, want VALIDATION_FAILED", r)
	}
	after, _ := svc.GetDhcpConfig(ctx, nil)
	if after.Version != cfg.Version {
		t.Errorf("failed update changed version: %d -> %d", cfg.Version, after.Version)
	}

	// Valid update bumps version and stores the subnet.
	updated, err := svc.UpdateDhcpConfig(ctx, &v1.UpdateDhcpConfigRequest{
		LeaseTtlSeconds: 7200,
		Subnets: []*v1.DhcpSubnet{{
			Network: "192.168.90.0/24", RangeStart: "192.168.90.100",
			RangeEnd: "192.168.90.200", Gateway: "192.168.90.1", Dns: []string{"1.1.1.1"}}},
	})
	if err != nil {
		t.Fatalf("valid update: %v", err)
	}
	if updated.Version != cfg.Version+1 || len(updated.Subnets) != 1 {
		t.Errorf("unexpected updated config: %+v", updated)
	}
	if updated.LeaseTtlSeconds != 7200 {
		t.Errorf("lease ttl = %d, want 7200", updated.LeaseTtlSeconds)
	}

	// Enable / disable flips the flag.
	if en, _ := svc.EnableDhcp(ctx, nil); !en.Enabled {
		t.Error("enable did not set flag")
	}
	if dis, _ := svc.DisableDhcp(ctx, nil); dis.Enabled {
		t.Error("disable did not clear flag")
	}

	// Leases empty on a fresh Valkey.
	leases, err := svc.ListLeases(ctx, &v1.PageRequest{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("list leases: %v", err)
	}
	if leases.Meta.Total != 0 {
		t.Errorf("expected 0 leases, got %d", leases.Meta.Total)
	}
}
