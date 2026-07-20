package bootfiles

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kdomanski/iso9660"

	"github.com/menta2k/universe/backend/internal/biz"
)

// buildISO returns an ISO9660 image containing casper/vmlinuz and casper/initrd
// with the given contents.
func buildISO(t *testing.T, kernel, initrd []byte) []byte {
	t.Helper()
	w, err := iso9660.NewWriter()
	if err != nil {
		t.Fatalf("iso writer: %v", err)
	}
	defer func() { _ = w.Cleanup() }()
	if err := w.AddFile(bytes.NewReader(kernel), "casper/vmlinuz"); err != nil {
		t.Fatalf("add vmlinuz: %v", err)
	}
	if err := w.AddFile(bytes.NewReader(initrd), "casper/initrd"); err != nil {
		t.Fatalf("add initrd: %v", err)
	}
	var buf bytes.Buffer
	if err := w.WriteTo(&buf, "TESTISO"); err != nil {
		t.Fatalf("write iso: %v", err)
	}
	return buf.Bytes()
}

// isoServer serves the ISO bytes with Range support and records request count.
func isoServer(t *testing.T, iso []byte) (*httptest.Server, *int) {
	t.Helper()
	reqs := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs++
		w.Header().Set("Accept-Ranges", "bytes")
		http.ServeContent(w, r, "ubuntu.iso", time.Time{}, bytes.NewReader(iso))
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

// memStore is an in-memory ArtifactStore.
type memStore struct {
	saved map[string][]byte // "release/kind" -> content
}

func newMemStore() *memStore { return &memStore{saved: map[string][]byte{}} }

func key(r biz.UbuntuRelease, k biz.ArtifactKind) string { return string(r) + "/" + string(k) }

func (m *memStore) GetByReleaseKind(_ context.Context, r biz.UbuntuRelease, k biz.ArtifactKind) (*biz.Artifact, error) {
	if _, ok := m.saved[key(r, k)]; ok {
		return &biz.Artifact{Kind: k, UbuntuRelease: r}, nil
	}
	return nil, biz.ErrEntityNotFound
}

func (m *memStore) Save(_ context.Context, meta *biz.Artifact, content io.Reader) (*biz.Artifact, error) {
	b, err := io.ReadAll(content)
	if err != nil {
		return nil, err
	}
	m.saved[key(meta.UbuntuRelease, meta.Kind)] = b
	return meta, nil
}

func newFetcher(store ArtifactStore, isoURL string) *Fetcher {
	f := New(store, Config{
		Releases: []biz.UbuntuRelease{biz.ReleaseNoble},
		ISOURLs:  map[biz.UbuntuRelease]string{biz.ReleaseNoble: isoURL},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return f
}

func TestEnsureReleaseExtractsAndStores(t *testing.T) {
	kernel := bytes.Repeat([]byte("K"), 40_000)
	initrd := bytes.Repeat([]byte("I"), 120_000)
	srv, reqs := isoServer(t, buildISO(t, kernel, initrd))
	store := newMemStore()

	f := newFetcher(store, srv.URL)
	if err := f.EnsureRelease(context.Background(), biz.ReleaseNoble); err != nil {
		t.Fatalf("EnsureRelease: %v", err)
	}

	if got := store.saved[key(biz.ReleaseNoble, biz.ArtifactKernel)]; !bytes.Equal(got, kernel) {
		t.Errorf("kernel content mismatch: got %d bytes", len(got))
	}
	if got := store.saved[key(biz.ReleaseNoble, biz.ArtifactInitrd)]; !bytes.Equal(got, initrd) {
		t.Errorf("initrd content mismatch: got %d bytes", len(got))
	}
	// Ranged reads must not have pulled the ISO many hundreds of times.
	if *reqs > 60 {
		t.Errorf("too many HTTP requests (%d); read-ahead not effective", *reqs)
	}
}

func TestEnsureReleaseSkipsWhenPresent(t *testing.T) {
	srv, reqs := isoServer(t, buildISO(t, []byte("k"), []byte("i")))
	store := newMemStore()
	store.saved[key(biz.ReleaseNoble, biz.ArtifactKernel)] = []byte("k")
	store.saved[key(biz.ReleaseNoble, biz.ArtifactInitrd)] = []byte("i")

	f := newFetcher(store, srv.URL)
	if err := f.EnsureRelease(context.Background(), biz.ReleaseNoble); err != nil {
		t.Fatalf("EnsureRelease: %v", err)
	}
	if *reqs != 0 {
		t.Errorf("expected no ISO fetch when artifacts present, got %d requests", *reqs)
	}
}

func TestEnsureReleaseFetchesOnlyMissing(t *testing.T) {
	kernel := bytes.Repeat([]byte("K"), 5000)
	initrd := bytes.Repeat([]byte("I"), 5000)
	srv, _ := isoServer(t, buildISO(t, kernel, initrd))
	store := newMemStore()
	store.saved[key(biz.ReleaseNoble, biz.ArtifactKernel)] = []byte("already")

	f := newFetcher(store, srv.URL)
	if err := f.EnsureRelease(context.Background(), biz.ReleaseNoble); err != nil {
		t.Fatalf("EnsureRelease: %v", err)
	}
	if got := string(store.saved[key(biz.ReleaseNoble, biz.ArtifactKernel)]); got != "already" {
		t.Errorf("existing kernel overwritten: %q", got)
	}
	if !bytes.Equal(store.saved[key(biz.ReleaseNoble, biz.ArtifactInitrd)], initrd) {
		t.Error("initrd not fetched")
	}
}

func TestEnsureReleaseUnsupported(t *testing.T) {
	f := newFetcher(newMemStore(), "http://unused")
	if err := f.EnsureRelease(context.Background(), biz.UbuntuRelease("focal")); err == nil {
		t.Fatal("expected error for unsupported release")
	}
}

func TestResolveISOURLDiscovers(t *testing.T) {
	listing := `<a href="ubuntu-24.04.1-live-server-amd64.iso">x</a>
	            <a href="ubuntu-24.04.4-live-server-amd64.iso">y</a>
	            <a href="ubuntu-24.04.4-live-server-arm64.iso">skip</a>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, listing)
	}))
	defer srv.Close()

	f := New(newMemStore(), Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Point discovery at our fake listing by overriding the client transport.
	f.client = srv.Client()
	f.cfg.ISOURLs = nil
	got, err := f.resolveViaListing(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.HasSuffix(got, "ubuntu-24.04.4-live-server-amd64.iso") {
		t.Errorf("picked wrong iso: %s", got)
	}
}

func TestEndToEndReaderAtEOF(t *testing.T) {
	iso := buildISO(t, []byte("kk"), []byte("ii"))
	srv, _ := isoServer(t, iso)
	ra, err := newHTTPReaderAt(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	if ra.Size() != int64(len(iso)) {
		t.Errorf("size = %d, want %d", ra.Size(), len(iso))
	}
	buf := make([]byte, 10)
	if _, err := ra.ReadAt(buf, ra.Size()); !errors.Is(err, io.EOF) {
		t.Errorf("read past end: err = %v, want EOF", err)
	}
}
