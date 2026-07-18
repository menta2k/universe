package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"universe/backend/internal/biz"
	"universe/backend/internal/netboot/bootsrv"
	"universe/backend/tests/integration/testenv"
)

// newHTTP starts an httptest server for the boot mux and returns its base URL.
func newHTTP(t *testing.T, srv *bootsrv.Server) string {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts.URL
}

// armMachine registers + provisions a machine and returns it plus the one-time
// seed token extracted from its iPXE script.
func armMachine(t *testing.T, ctx context.Context, srv *bootsrv.Server, machines *biz.MachineUsecase, mac, name, profileID, ipxeURL string) (*biz.Machine, string) {
	t.Helper()
	m, err := machines.Register(ctx, biz.RegisterInput{MAC: mac, Name: name, ProfileID: profileID})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	armed, err := machines.Provision(ctx, m.ID)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	body, code := get(t, ipxeURL+"/boot/ipxe/"+mac)
	if code != 200 || !strings.Contains(body, "autoinstall") {
		t.Fatalf("arm ipxe wrong (code %d): %s", code, body)
	}
	return armed, extractSeedToken(t, body)
}

// TestBootIPXEUnarmed covers the fallback script for machines with no active
// session and for entirely unknown MACs (handleIPXE error path).
func TestBootIPXEUnarmed(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	profileID := seedProfile(t, env)
	srv, machines, _ := newBootStack(t, env)
	ts := newHTTP(t, srv)

	// Registered but never provisioned -> no active session.
	if _, err := machines.Register(ctx, biz.RegisterInput{
		MAC: "52:54:00:00:00:11", Name: "unarmed", ProfileID: profileID}); err != nil {
		t.Fatal(err)
	}
	body, code := get(t, ts+"/boot/ipxe/52:54:00:00:00:11")
	if code != 200 || strings.TrimSpace(body) != "#!ipxe\nexit" {
		t.Errorf("unarmed fallback wrong (code %d): %q", code, body)
	}

	// Completely unknown MAC -> same fallback.
	body, code = get(t, ts+"/boot/ipxe/52:54:00:99:99:99")
	if code != 200 || !strings.Contains(body, "#!ipxe") || !strings.Contains(body, "exit") {
		t.Errorf("unknown mac fallback wrong (code %d): %q", code, body)
	}
	if strings.Contains(body, "autoinstall") {
		t.Errorf("fallback must not contain an autoinstall boot: %q", body)
	}
}

// TestBootFileServing covers handleFile: unknown kind -> 404, missing artifact
// -> 404, and a present artifact streamed with a Content-Length.
func TestBootFileServing(t *testing.T) {
	env := testenv.Start(t)
	seedProfile(t, env)
	srv, _, _ := newBootStack(t, env)
	ts := newHTTP(t, srv)

	// Unknown kind.
	if _, code := get(t, ts+"/boot/file/noble/rootfs"); code != http.StatusNotFound {
		t.Errorf("unknown kind: code = %d, want 404", code)
	}

	// Known kind but release with no artifact.
	if _, code := get(t, ts+"/boot/file/jammy/kernel"); code != http.StatusNotFound {
		t.Errorf("missing artifact: code = %d, want 404", code)
	}

	// Present artifact (seeded by newBootStack) streams with Content-Length.
	resp, err := http.Get(ts + "/boot/file/noble/initrd")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("initrd serve: code = %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Length") == "" {
		t.Errorf("missing Content-Length header")
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("content-type = %q, want application/octet-stream", ct)
	}
}

// TestBootMetaAndVendorData covers handleMetaData (valid + invalid token) and
// handleVendorData.
func TestBootMetaAndVendorData(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	profileID := seedProfile(t, env)
	srv, machines, _ := newBootStack(t, env)
	ts := newHTTP(t, srv)

	_, token := armMachine(t, ctx, srv, machines, "52:54:00:00:0a:01", "meta-target", profileID, ts)

	// meta-data with a valid token.
	body, code := get(t, ts+"/boot/seed/"+token+"/meta-data")
	if code != 200 {
		t.Fatalf("meta-data code = %d", code)
	}
	if !strings.Contains(body, "instance-id:") || !strings.Contains(body, "local-hostname:") {
		t.Errorf("meta-data body missing expected keys: %q", body)
	}

	// meta-data with an invalid token -> 403.
	if _, code := get(t, ts+"/boot/seed/deadbeef/meta-data"); code != http.StatusForbidden {
		t.Errorf("invalid meta-data token: code = %d, want 403", code)
	}

	// vendor-data -> 200 empty.
	body, code = get(t, ts+"/boot/seed/"+token+"/vendor-data")
	if code != 200 {
		t.Errorf("vendor-data code = %d, want 200", code)
	}
	if body != "" {
		t.Errorf("vendor-data body = %q, want empty", body)
	}
}

// TestBootReportError covers handleReport error status (machine -> failed),
// invalid token (403), and idempotency of a second report after the terminal
// state burned the token.
func TestBootReportError(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	profileID := seedProfile(t, env)
	srv, machines, _ := newBootStack(t, env)
	ts := newHTTP(t, srv)

	armed, token := armMachine(t, ctx, srv, machines, "52:54:00:00:0b:02", "fail-target", profileID, ts)

	// Invalid token -> 403.
	rep, err := http.PostForm(ts+"/boot/report/deadbeef", map[string][]string{"status": {"error"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = rep.Body.Close()
	if rep.StatusCode != http.StatusForbidden {
		t.Errorf("invalid report token: code = %d, want 403", rep.StatusCode)
	}

	// status=error -> machine transitions to failed.
	rep, err = http.PostForm(ts+"/boot/report/"+token,
		map[string][]string{"status": {"error"}, "log_tail": {"disk failure"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = rep.Body.Close()
	if rep.StatusCode != http.StatusNoContent {
		t.Fatalf("error report: code = %d, want 204", rep.StatusCode)
	}

	got, err := machines.Get(ctx, armed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != biz.StateFailed {
		t.Errorf("machine state = %s, want failed", got.State)
	}

	// A second report after the terminal state: the token was burned, so the
	// endpoint rejects it (403) rather than double-finishing the session.
	rep, err = http.PostForm(ts+"/boot/report/"+token, map[string][]string{"status": {"error"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = rep.Body.Close()
	if rep.StatusCode != http.StatusForbidden {
		t.Errorf("second report: code = %d, want 403 (token burned)", rep.StatusCode)
	}
}

// TestTokenStoreRoundTrip exercises Issue -> Resolve -> Invalidate against a
// real Valkey, plus Resolve of a missing token.
func TestTokenStoreRoundTrip(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	store := bootsrv.NewTokenStore(env.Data.Valkey, 30*time.Minute)

	payload := bootsrv.SeedPayload{SessionID: "sess-1", MachineID: "mach-1", PasswordHash: "$argon2id$abc"}
	token, err := store.Issue(ctx, payload)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if token == "" {
		t.Fatal("issued empty token")
	}

	got, err := store.Resolve(ctx, token)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.SessionID != payload.SessionID || got.MachineID != payload.MachineID || got.PasswordHash != payload.PasswordHash {
		t.Errorf("resolved payload = %+v, want %+v", got, payload)
	}

	if err := store.Invalidate(ctx, token); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	if _, err := store.Resolve(ctx, token); err == nil {
		t.Error("resolve after invalidate should fail")
	}

	// Resolve of a never-issued token.
	if _, err := store.Resolve(ctx, "does-not-exist"); err == nil {
		t.Error("resolve of missing token should fail")
	}
}

// TestCredentialMinter verifies MintOneTime returns distinct argon2id hashes.
func TestCredentialMinter(t *testing.T) {
	minter := bootsrv.NewOneTimeCredentialMinter()
	h1, err := minter.MintOneTime()
	if err != nil {
		t.Fatalf("mint 1: %v", err)
	}
	h2, err := minter.MintOneTime()
	if err != nil {
		t.Fatalf("mint 2: %v", err)
	}
	if !strings.HasPrefix(h1, "$argon2id$") {
		t.Errorf("hash 1 not argon2id: %q", h1)
	}
	if !strings.HasPrefix(h2, "$argon2id$") {
		t.Errorf("hash 2 not argon2id: %q", h2)
	}
	if h1 == h2 {
		t.Error("two mints produced identical hashes (must be per-call random)")
	}
}
