package integration

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	khttp "github.com/go-kratos/kratos/v2/transport/http"

	v1 "universe/backend/api/netboot/v1"
	"universe/backend/internal/biz"
	"universe/backend/internal/conf"
	"universe/backend/internal/data"
	"universe/backend/internal/server"
	"universe/backend/internal/service"
	"universe/backend/tests/integration/testenv"
)

// startFullServer wires the real Kratos HTTP server against real storage and
// runs it on an ephemeral port, returning its base URL. This exercises the
// server wiring, auth middleware, audit, encoders, hardening, and the machine/
// profile/dhcp/session service RPCs end-to-end over HTTP.
func startFullServer(t *testing.T, env *testenv.Env) string {
	t.Helper()
	log := testLog()
	addr := freePort(t)
	cfg := &conf.Config{}
	cfg.Server.HTTPAddr = addr
	cfg.Server.ExternalBootURL = "http://127.0.0.1:8082"

	events := biz.NewEventRecorder(data.NewEventRepo(env.Data), data.NewEventPublisher(env.Data), log)
	operators := biz.NewOperatorUsecase(
		data.NewOperatorRepo(env.Data), data.NewSessionStore(env.Data, time.Hour), log)
	if err := operators.EnsureBootstrap(context.Background(), "admin", "change-me-please"); err != nil {
		t.Fatal(err)
	}
	machineRepo := data.NewMachineRepo(env.Data)
	sessionRepo := data.NewSessionRepo(env.Data)
	profileRepo := data.NewProfileRepo(env.Data)
	machines := biz.NewMachineUsecase(machineRepo, sessionRepo, profileRepo,
		data.NewDhcpGate(env.Data), events, log)
	profiles := biz.NewProfileUsecase(profileRepo, autoinstallValidator{}, log)
	dhcpRepo := data.NewDhcpConfigRepo(env.Data)
	dhcpUC := biz.NewDhcpConfigUsecase(dhcpRepo, data.NewLeaseRepo(env.Data), dhcpRepo, nil, log)
	sessionsUC := biz.NewSessionQueryUsecase(data.NewSessionQueryRepo(env.Data))
	artifactsUC := biz.NewArtifactUsecase(mustArtifactStore(t, env), data.NewTransferRepo(env.Data), log)

	authSvc := service.NewAuthService(operators)
	machineSvc := service.NewMachineService(machines)
	profileSvc := service.NewProfileService(profiles, machines)
	dhcpSvc := service.NewDhcpService(dhcpUC)
	sessionSvc := service.NewSessionService(sessionsUC)
	artifactSvc := service.NewArtifactService(artifactsUC, 1<<30)

	srv := server.NewHTTPServer(cfg, log, server.NewMetrics(), operators, events,
		func(s *khttp.Server) { v1.RegisterAuthServiceHTTPServer(s, authSvc) },
		func(s *khttp.Server) { v1.RegisterMachineServiceHTTPServer(s, machineSvc) },
		func(s *khttp.Server) { v1.RegisterProfileServiceHTTPServer(s, profileSvc) },
		func(s *khttp.Server) { v1.RegisterDhcpServiceHTTPServer(s, dhcpSvc) },
		func(s *khttp.Server) { v1.RegisterSessionServiceHTTPServer(s, sessionSvc) },
		func(s *khttp.Server) { v1.RegisterArtifactServiceHTTPServer(s, artifactSvc) },
		func(s *khttp.Server) { artifactSvc.RegisterMultipart(s) },
	)
	go func() { _ = srv.Start(context.Background()) }()
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })

	// Poll /healthz on the fixed address until the server serves.
	base := "http://" + addr
	for range 200 {
		resp, err := http.Get(base + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				return base
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server did not become ready (base=%q)", base)
	return ""
}

// autoinstallValidator is a no-op validator for the HTTP test (rendering is
// covered elsewhere).
type autoinstallValidator struct{}

func (autoinstallValidator) Validate(*biz.Profile) error { return nil }

func mustArtifactStore(t *testing.T, env *testenv.Env) *data.ArtifactStore {
	t.Helper()
	store, err := data.NewArtifactStore(env.Data, t.TempDir(), 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestFullServerHTTPFlow(t *testing.T) {
	env := testenv.Start(t)
	base := startFullServer(t, env)
	jar := &cookieJar{}

	// Unauthenticated request -> 401 via error encoder.
	code, body, _ := doJSON(t, jar, "GET", base+"/api/v1/machines", "")
	if code != 401 || !strings.Contains(body, "UNAUTHENTICATED") {
		t.Fatalf("unauth machines: code=%d body=%s", code, body)
	}

	// Login sets a session cookie (auth_service.Login, rate limiter allow path).
	code, body, hdr := doJSON(t, jar, "POST", base+"/api/v1/auth/login",
		`{"username":"admin","password":"change-me-please"}`)
	if code != 200 || !strings.Contains(body, `"username":"admin"`) {
		t.Fatalf("login: code=%d body=%s", code, body)
	}
	// Security headers present.
	if hdr.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing security headers")
	}
	jar.setFrom(hdr)

	// Authenticated Me + machine create + list (auth middleware authed path,
	// audit middleware mutation path, response encoder, machine service).
	if code, body, _ := doJSON(t, jar, "GET", base+"/api/v1/auth/me", ""); code != 200 || !strings.Contains(body, "admin") {
		t.Fatalf("me: code=%d body=%s", code, body)
	}
	code, body, _ = doJSON(t, jar, "POST", base+"/api/v1/machines",
		`{"mac":"52:54:00:77:88:99","name":"http-node"}`)
	if code != 200 || !strings.Contains(body, "http-node") {
		t.Fatalf("create machine: code=%d body=%s", code, body)
	}
	if code, body, _ := doJSON(t, jar, "GET", base+"/api/v1/machines", ""); code != 200 || !strings.Contains(body, "http-node") {
		t.Fatalf("list machines: code=%d body=%s", code, body)
	}

	// Validation error surfaces field details (422 path).
	if code, body, _ := doJSON(t, jar, "POST", base+"/api/v1/machines", `{"mac":"bad","name":"X"}`); code != 422 || !strings.Contains(body, "VALIDATION_FAILED") {
		t.Fatalf("invalid create: code=%d body=%s", code, body)
	}

	// Exercise the remaining read services + DHCP config lifecycle.
	for _, path := range []string{
		"/api/v1/profiles", "/api/v1/sessions", "/api/v1/artifacts",
		"/api/v1/artifacts/transfers", "/api/v1/dhcp/config", "/api/v1/dhcp/leases",
		"/api/v1/dhcp/conflicts", "/api/v1/machines/unknown",
	} {
		if code, body, _ := doJSON(t, jar, "GET", base+path, ""); code != 200 {
			t.Errorf("GET %s: code=%d body=%s", path, code, body)
		}
	}
	// DHCP update (valid) + enable + disable.
	if code, body, _ := doJSON(t, jar, "PUT", base+"/api/v1/dhcp/config",
		`{"lease_ttl_seconds":3600,"subnets":[{"network":"192.168.90.0/24","range_start":"192.168.90.100","range_end":"192.168.90.200","gateway":"192.168.90.1"}]}`); code != 200 {
		t.Errorf("dhcp update: code=%d body=%s", code, body)
	}
	if code, _, _ := doJSON(t, jar, "POST", base+"/api/v1/dhcp/enable", "{}"); code != 200 {
		t.Errorf("dhcp enable: code=%d", code)
	}
	if code, _, _ := doJSON(t, jar, "POST", base+"/api/v1/dhcp/disable", "{}"); code != 200 {
		t.Errorf("dhcp disable: code=%d", code)
	}

	// Logout clears the session.
	if code, _, hdr := doJSON(t, jar, "POST", base+"/api/v1/auth/logout", "{}"); code != 200 {
		t.Fatalf("logout: code=%d", code)
	} else {
		jar.setFrom(hdr)
	}
	if code, _, _ := doJSON(t, jar, "GET", base+"/api/v1/auth/me", ""); code != 401 {
		t.Errorf("after logout expected 401, got %d", code)
	}

	// Metrics + health endpoints.
	if code, _, _ := doJSON(t, jar, "GET", base+"/healthz", ""); code != 200 {
		t.Errorf("healthz code=%d", code)
	}
	// Metrics + health checked below (before re-login for the provision flow).
	if code, body, _ := doJSON(t, jar, "GET", base+"/metrics", ""); code != 200 || !strings.Contains(body, "netboot_") {
		t.Errorf("metrics code=%d", code)
	}
}

func TestFullServerLoginRateLimit(t *testing.T) {
	env := testenv.Start(t)
	base := startFullServer(t, env)
	jar := &cookieJar{}
	limited := false
	for range 30 {
		code, _, _ := doJSON(t, jar, "POST", base+"/api/v1/auth/login",
			`{"username":"admin","password":"wrong"}`)
		if code == 429 {
			limited = true
			break
		}
	}
	if !limited {
		t.Error("expected a 429 after exceeding the login rate limit")
	}
}

// --- tiny HTTP helpers -----------------------------------------------------

// freePort reserves an ephemeral port and returns its address, closing the
// listener so the server can bind it (small race window, acceptable in tests).
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

type cookieJar struct{ cookie string }

func (j *cookieJar) setFrom(h http.Header) {
	if sc := h.Get("Set-Cookie"); sc != "" {
		j.cookie = strings.SplitN(sc, ";", 2)[0]
	}
}

func doJSON(t *testing.T, jar *cookieJar, method, url, body string) (int, string, http.Header) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if jar != nil && jar.cookie != "" {
		req.Header.Set("Cookie", jar.cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

var _ = json.Marshal
