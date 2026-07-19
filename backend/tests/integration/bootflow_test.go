package integration

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/menta2k/universe/backend/internal/biz"
	"github.com/menta2k/universe/backend/internal/data"
	"github.com/menta2k/universe/backend/internal/netboot/bootsrv"
	"github.com/menta2k/universe/backend/tests/integration/testenv"
)

// seedProfile inserts a profile directly and returns its id.
func seedProfile(t *testing.T, env *testenv.Env) string {
	t.Helper()
	var id string
	err := env.Data.Pool.QueryRow(context.Background(),
		`INSERT INTO profiles (name, ubuntu_release, ssh_authorized_keys)
		 VALUES ('noble-default', 'noble', ARRAY['ssh-ed25519 AAAA test@example'])
		 RETURNING id`).Scan(&id)
	if err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	return id
}

func newBootStack(t *testing.T, env *testenv.Env) (*bootsrv.Server, *biz.MachineUsecase, *biz.SessionUsecase) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	events := biz.NewEventRecorder(data.NewEventRepo(env.Data), data.NewEventPublisher(env.Data), log)
	machineRepo := data.NewMachineRepo(env.Data)
	sessionRepo := data.NewSessionRepo(env.Data)
	profileRepo := data.NewProfileRepo(env.Data)
	machines := biz.NewMachineUsecase(machineRepo, sessionRepo, profileRepo,
		alwaysEnabledGate{}, events, log)
	sessions := biz.NewSessionUsecase(sessionRepo, machineRepo, events, log)
	facade := biz.NewBootFacade(machines, sessions)

	store, err := data.NewArtifactStore(env.Data, t.TempDir(), 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	// Seed kernel + initrd artifacts.
	for _, kind := range []biz.ArtifactKind{biz.ArtifactKernel, biz.ArtifactInitrd} {
		name := "noble-" + string(kind)
		if _, err := store.Save(context.Background(),
			&biz.Artifact{Kind: kind, UbuntuRelease: biz.ReleaseNoble, Filename: name},
			strings.NewReader("dummy-"+string(kind)+"-bytes")); err != nil {
			t.Fatalf("seed artifact: %v", err)
		}
	}

	tokens := bootsrv.NewTokenStore(env.Data.Valkey, 30*time.Minute)
	srv := bootsrv.NewServer("http://192.0.2.1:8082", facade, store, tokens,
		bootsrv.NewOneTimeCredentialMinter(), events, log)
	return srv, machines, sessions
}

type alwaysEnabledGate struct{}

func (alwaysEnabledGate) Enabled(context.Context) (bool, error) { return true, nil }

func TestBootFlowEndToEnd(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	profileID := seedProfile(t, env)
	srv, machines, _ := newBootStack(t, env)

	m, err := machines.Register(ctx, biz.RegisterInput{
		MAC: "52:54:00:ab:cd:ef", Name: "boot-target", ProfileID: profileID})
	if err != nil {
		t.Fatal(err)
	}
	armed, err := machines.Provision(ctx, m.ID)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. iPXE script for the armed machine.
	body, code := get(t, ts.URL+"/boot/ipxe/52:54:00:ab:cd:ef")
	if code != 200 || !strings.Contains(body, "#!ipxe") || !strings.Contains(body, "autoinstall") {
		t.Fatalf("ipxe script wrong (code %d): %s", code, body)
	}
	token := extractSeedToken(t, body)
	seedBase := ts.URL + "/boot/seed/" + token + "/"

	// 2. Kernel file served with Content-Length.
	resp, err := http.Get(ts.URL + "/boot/file/noble/kernel")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || resp.Header.Get("Content-Length") == "" {
		t.Errorf("kernel serve: code=%d content-length=%q", resp.StatusCode, resp.Header.Get("Content-Length"))
	}
	_ = resp.Body.Close()

	// 3. user-data seed with the one-time token.
	udBody, udCode := get(t, seedBase+"user-data")
	if udCode != 200 || !strings.Contains(udBody, "autoinstall") || !strings.Contains(udBody, "allow-pw: false") {
		t.Fatalf("user-data wrong (code %d): %s", udCode, udBody)
	}

	// 4. Invalid token -> 403.
	if _, code := get(t, ts.URL+"/boot/seed/deadbeef/user-data"); code != 403 {
		t.Errorf("expected 403 for bad token, got %d", code)
	}

	// 5. Report success -> session completed, machine installed.
	rep, err := http.PostForm(ts.URL+"/boot/report/"+token, map[string][]string{"status": {"ok"}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.StatusCode != http.StatusNoContent {
		t.Errorf("report status = %d, want 204", rep.StatusCode)
	}
	_ = rep.Body.Close()

	got, err := machines.Get(ctx, armed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != biz.StateInstalled {
		t.Errorf("machine state = %s, want installed", got.State)
	}
}

func get(t *testing.T, url string) (string, int) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b), resp.StatusCode
}

// extractSeedToken pulls the one-time seed token out of the rendered cmdline
// (…s=http://host/boot/seed/<token>/…).
func extractSeedToken(t *testing.T, ipxeScript string) string {
	t.Helper()
	const marker = "/boot/seed/"
	i := strings.Index(ipxeScript, marker)
	if i < 0 {
		t.Fatalf("seed marker not found in: %s", ipxeScript)
	}
	rest := ipxeScript[i+len(marker):]
	token, _, ok := strings.Cut(rest, "/")
	if !ok {
		t.Fatalf("malformed seed url: %s", rest)
	}
	return token
}
