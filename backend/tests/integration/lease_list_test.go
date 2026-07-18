package integration

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"universe/backend/internal/biz"
	"universe/backend/internal/data"
	"universe/backend/internal/netboot/dhcp"
	"universe/backend/tests/integration/testenv"
)

// TestLeaseRepoListsActiveLeases seeds a lease through the pool (which writes
// both lease:<ip> and the lease:mac:<mac> reverse index) and verifies the
// repo lists the forward lease while skipping the reverse-index key.
func TestLeaseRepoListsActiveLeases(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()

	_, network, _ := net.ParseCIDR("192.168.90.0/24")
	pool := dhcp.NewLeasePool(env.Data.Valkey, []dhcp.Subnet{{
		Network:    network,
		RangeStart: net.ParseIP("192.168.90.100"),
		RangeEnd:   net.ParseIP("192.168.90.110"),
	}}, nil, time.Hour)

	if _, err := pool.Allocate(ctx, "52:54:00:le:as:01", "m1"); err != nil {
		t.Fatalf("allocate: %v", err)
	}

	leases, total, err := data.NewLeaseRepo(env.Data).ListLeases(ctx, 1, 50)
	if err != nil {
		t.Fatalf("list leases: %v", err)
	}
	if total != 1 || len(leases) != 1 {
		t.Fatalf("expected exactly 1 forward lease (reverse index skipped), got total=%d len=%d", total, len(leases))
	}
	if leases[0].MAC != "52:54:00:le:as:01" || leases[0].IP == "" {
		t.Errorf("unexpected lease: %+v", leases[0])
	}
}

// TestTFTPFileSourceOpensArtifact covers the artifact-backed TFTP source.
func TestTFTPFileSourceOpensArtifact(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	store, err := data.NewArtifactStore(env.Data, t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(ctx,
		&biz.Artifact{Kind: biz.ArtifactIPXEBin, Filename: "custom.kpxe"},
		strings.NewReader("IPXE-BINARY")); err != nil {
		t.Fatal(err)
	}
	src := data.NewTFTPFileSource(store)
	rc, size, err := src.Open(ctx, "custom.kpxe")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer rc.Close()
	if size != int64(len("IPXE-BINARY")) {
		t.Errorf("size = %d", size)
	}
	if _, _, err := src.Open(ctx, "missing.bin"); err == nil {
		t.Error("expected error opening missing artifact")
	}
}
