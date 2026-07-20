//go:build realiso

// Opt-in test that exercises the fetcher against the real Ubuntu mirror.
// Run with: go test -tags realiso ./internal/netboot/bootfiles/ -run RealISO -v
package bootfiles

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/menta2k/universe/backend/internal/biz"
)

func TestRealISOExtractsKernelInitrd(t *testing.T) {
	store := newMemStore()
	f := New(store, Config{Releases: []biz.UbuntuRelease{biz.ReleaseNoble}},
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := f.EnsureRelease(context.Background(), biz.ReleaseNoble); err != nil {
		t.Fatalf("EnsureRelease(noble): %v", err)
	}
	k := store.saved[key(biz.ReleaseNoble, biz.ArtifactKernel)]
	i := store.saved[key(biz.ReleaseNoble, biz.ArtifactInitrd)]
	t.Logf("kernel=%d bytes initrd=%d bytes", len(k), len(i))
	if len(k) < 1_000_000 {
		t.Errorf("kernel suspiciously small: %d bytes", len(k))
	}
	if len(i) < 10_000_000 {
		t.Errorf("initrd suspiciously small: %d bytes", len(i))
	}
}
