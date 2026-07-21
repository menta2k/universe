package bootsrv

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/menta2k/universe/backend/internal/biz"
	"github.com/menta2k/universe/backend/internal/netboot/autoinstall"
)

// BootUsecase resolves booting machines and finalizes installs (biz layer).
type BootUsecase interface {
	BootInfo(ctx context.Context, mac string) (*biz.BootDecision, error)
	ReportInstall(ctx context.Context, sessionID, status, logTail string) error
}

// ArtifactSource opens kernel/initrd/iso content for streaming.
type ArtifactSource interface {
	GetByReleaseKind(ctx context.Context, release biz.UbuntuRelease, kind biz.ArtifactKind) (*biz.Artifact, error)
	GetByFilename(ctx context.Context, filename string) (*biz.Artifact, error)
	Open(ctx context.Context, a *biz.Artifact) (io.ReadCloser, error)
}

// CredentialMinter creates a one-time password hash per session (FR-018).
type CredentialMinter interface {
	MintOneTime() (hash string, err error)
}

// Server implements the machine-facing boot HTTP endpoints (contracts §3).
type Server struct {
	externalURL string
	boot        BootUsecase
	artifacts   ArtifactSource
	tokens      *TokenStore
	creds       CredentialMinter
	events      *biz.EventRecorder
	log         *slog.Logger
	httpSrv     *http.Server
	opts        BootOptions
}

// BootOptions selects how the installer's root filesystem is delivered.
// NFSRoot (low memory) takes precedence over ServeISO (url=, RAM-heavy); with
// neither, only registered machines with pre-uploaded artifacts boot.
type BootOptions struct {
	// ServeISO adds root=/dev/ram0 ... url=<iso> so casper downloads the whole
	// ISO into RAM.
	ServeISO bool
	// NFSRoot adds netboot=nfs boot=casper nfsroot=<ip>:/<release> so casper
	// mounts the squashfs live over NFS (low memory).
	NFSRoot bool
	// NFSServerIP is the address casper mounts the NFS root from (the
	// provisioning interface IP).
	NFSServerIP string
}

func NewServer(
	externalURL string, boot BootUsecase, artifacts ArtifactSource,
	tokens *TokenStore, creds CredentialMinter, events *biz.EventRecorder, log *slog.Logger,
	opts BootOptions,
) *Server {
	return &Server{
		externalURL: strings.TrimRight(externalURL, "/"),
		boot:        boot, artifacts: artifacts, tokens: tokens,
		creds: creds, events: events, log: log, opts: opts,
	}
}

// Handler builds the boot route mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boot/ipxe/{mac}", s.handleIPXE)
	mux.HandleFunc("GET /boot/iso/{file}", s.handleISO)
	mux.HandleFunc("GET /boot/file/{release}/{kind}", s.handleFile)
	mux.HandleFunc("GET /boot/seed/{token}/user-data", s.handleUserData)
	mux.HandleFunc("GET /boot/seed/{token}/meta-data", s.handleMetaData)
	mux.HandleFunc("GET /boot/seed/{token}/vendor-data", s.handleVendorData)
	mux.HandleFunc("POST /boot/report/{token}", s.handleReport)
	return mux
}

func (s *Server) ListenAndServe(addr string) error {
	s.httpSrv = &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.log.Info("boot http server listening", "addr", addr)
	if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("boot http server: %w", err)
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Shutdown(ctx)
}

// handleIPXE serves the per-machine iPXE script (contracts §3).
func (s *Server) handleIPXE(w http.ResponseWriter, r *http.Request) {
	mac := r.PathValue("mac")
	dec, err := s.boot.BootInfo(r.Context(), mac)
	if err != nil {
		// No active session: tell iPXE to fall through to local boot.
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "#!ipxe\nexit\n")
		return
	}

	hash, err := s.creds.MintOneTime()
	if err != nil {
		s.fail(w, r, "ipxe", dec.Session.ID, mac, http.StatusInternalServerError, err)
		return
	}
	token, err := s.tokens.Issue(r.Context(), SeedPayload{
		SessionID: dec.Session.ID, MachineID: dec.Machine.ID, PasswordHash: hash,
	})
	if err != nil {
		s.fail(w, r, "ipxe", dec.Session.ID, mac, http.StatusInternalServerError, err)
		return
	}

	in := autoinstall.Input{
		Machine: dec.Machine, Profile: dec.Profile, Session: dec.Session,
		BootURL: s.externalURL, SeedToken: token, OneTimePasswordHash: hash,
	}
	cmdline, err := autoinstall.Cmdline(in)
	if err != nil {
		s.fail(w, r, "ipxe", dec.Session.ID, mac, http.StatusInternalServerError, err)
		return
	}
	script := s.ipxeScript(dec, cmdline)

	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.WriteString(w, script)
	s.record(r.Context(), biz.PhaseIPXEScript, biz.OutcomeOK, dec.Session.ID, mac, nil)
}

func (s *Server) ipxeScript(dec *biz.BootDecision, cmdline string) string {
	base := s.externalURL
	rel := string(dec.Profile.UbuntuRelease)
	switch {
	case s.opts.NFSRoot:
		// Low-memory path: casper mounts the squashfs live over NFS (paged) via
		// netboot=nfs, instead of buffering the whole ISO in RAM. The release
		// ISO is loop-mounted under the NFS export at /<release>.
		//
		// Networking is left under cloud-init/subiquity control (its default is
		// DHCP on the boot NIC) so the installer gets a resolver and apt can
		// reach the mirror. An earlier network-config=disabled here (to keep
		// cloud-init off the NFS NIC) left /etc/resolv.conf empty, so every
		// extra-package install failed with apt exit status 100; seeding
		// resolv.conf from the autoinstall did not survive into curtin's target,
		// whereas the cloud-init-managed DHCP path installs cleanly end to end.
		cmdline = fmt.Sprintf(
			"netboot=nfs boot=casper nfsroot=%s:/%s ip=dhcp %s",
			s.opts.NFSServerIP, rel, cmdline)
	case s.opts.ServeISO:
		// When serving the ISO, prepend the parameters casper needs to download
		// and loop-mount the live filesystem over HTTP (per Ubuntu's netboot
		// docs): root=/dev/ram0 ramdisk_size=... engages the ramdisk boot,
		// ip=dhcp brings networking up, and url=<...>.iso must end in .iso so
		// casper recognises it as a fetchable image. Loads the whole ISO into
		// RAM, so the target needs ~12 GB+.
		cmdline = fmt.Sprintf(
			"root=/dev/ram0 ramdisk_size=1500000 ip=dhcp url=%s/boot/iso/%s.iso %s",
			base, rel, cmdline)
	}
	var b strings.Builder
	b.WriteString("#!ipxe\n")
	fmt.Fprintf(&b, "kernel %s/boot/file/%s/kernel initrd=initrd %s\n", base, rel, cmdline)
	fmt.Fprintf(&b, "initrd %s/boot/file/%s/initrd\n", base, rel)
	b.WriteString("boot\n")
	return b.String()
}

// handleISO serves the release install ISO with HTTP range support so casper
// can fetch its root filesystem. The daemon must have the ISO stored (uploaded
// or auto-fetched); missing ISO is a 404.
func (s *Server) handleISO(w http.ResponseWriter, r *http.Request) {
	// The URL ends in "<release>.iso" so casper recognises it; that is exactly
	// the stored artifact filename, so look it up directly.
	art, err := s.artifacts.GetByFilename(r.Context(), r.PathValue("file"))
	if err != nil {
		http.Error(w, "iso not found", http.StatusNotFound)
		return
	}
	rc, err := s.artifacts.Open(r.Context(), art)
	if err != nil {
		http.Error(w, "iso unavailable", http.StatusInternalServerError)
		return
	}
	defer func() { _ = rc.Close() }()
	rs, ok := rc.(io.ReadSeeker)
	if !ok {
		http.Error(w, "iso not seekable", http.StatusInternalServerError)
		return
	}
	// ServeContent handles Range/If-Range/Content-Length so casper can fetch
	// the filesystem in chunks.
	http.ServeContent(w, r, art.Filename, art.UpdatedAt, rs)
	s.record(r.Context(), biz.PhaseFileServed, biz.OutcomeOK, "", "",
		map[string]any{"file": art.Filename})
}

// handleFile streams a kernel or initrd with a Content-Length (iPXE speed).
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	release := biz.UbuntuRelease(r.PathValue("release"))
	kindParam := r.PathValue("kind")
	var kind biz.ArtifactKind
	switch kindParam {
	case "kernel":
		kind = biz.ArtifactKernel
	case "initrd":
		kind = biz.ArtifactInitrd
	default:
		http.Error(w, "unknown artifact kind", http.StatusNotFound)
		return
	}

	art, err := s.artifacts.GetByReleaseKind(r.Context(), release, kind)
	if err != nil {
		s.record(r.Context(), biz.PhaseFileServed, biz.OutcomeError, "", "",
			map[string]any{"release": string(release), "kind": kindParam, "error": "not found"})
		http.Error(w, "artifact not found", http.StatusNotFound)
		return
	}
	rc, err := s.artifacts.Open(r.Context(), art)
	if err != nil {
		http.Error(w, "artifact unavailable", http.StatusInternalServerError)
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", art.SizeBytes))
	n, _ := io.Copy(w, rc)
	s.record(r.Context(), biz.PhaseFileServed, biz.OutcomeOK, "", "",
		map[string]any{"file": art.Filename, "bytes": n})
}

// handleUserData renders and serves the per-session autoinstall document.
func (s *Server) handleUserData(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	payload, err := s.tokens.Resolve(r.Context(), token)
	if err != nil {
		s.record(r.Context(), biz.PhaseSeedServed, biz.OutcomeDenied, "", "",
			map[string]any{"reason": "invalid or expired token"})
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	dec, err := s.bootInfoBySession(r.Context(), payload)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	userData, _, err := autoinstall.Render(autoinstall.Input{
		Machine: dec.Machine, Profile: dec.Profile, Session: dec.Session,
		BootURL: s.externalURL, SeedToken: token, OneTimePasswordHash: payload.PasswordHash,
	})
	if err != nil {
		// Never serve a partial document (FR-008); fail the boot.
		s.fail(w, r, "seed", dec.Session.ID, dec.Machine.MAC, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "text/cloud-config")
	_, _ = io.WriteString(w, userData)
	s.record(r.Context(), biz.PhaseSeedServed, biz.OutcomeOK, dec.Session.ID, dec.Machine.MAC, nil)
}

func (s *Server) handleMetaData(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	payload, err := s.tokens.Resolve(r.Context(), token)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	dec, err := s.bootInfoBySession(r.Context(), payload)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	_, metaData, err := autoinstall.Render(autoinstall.Input{
		Machine: dec.Machine, Profile: dec.Profile, Session: dec.Session,
		BootURL: s.externalURL, SeedToken: token, OneTimePasswordHash: payload.PasswordHash,
	})
	if err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.WriteString(w, metaData)
}

// handleVendorData returns an empty document subiquity still expects.
func (s *Server) handleVendorData(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
}

// handleReport finalizes a session from the installer callback (idempotent).
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	payload, err := s.tokens.Resolve(r.Context(), token)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	status := r.FormValue("status")
	if status == "" {
		status = "ok"
	}
	logTail := r.FormValue("log_tail")
	if err := s.boot.ReportInstall(r.Context(), payload.SessionID, status, logTail); err != nil {
		http.Error(w, "report failed", http.StatusInternalServerError)
		return
	}
	// Terminal state reached: burn the token (FR-018).
	_ = s.tokens.Invalidate(r.Context(), token)
	w.WriteHeader(http.StatusNoContent)
}

// bootInfoBySession re-resolves the decision via the machine's MAC. The token
// payload carries machine/session ids; we look up by MAC to reuse BootInfo.
func (s *Server) bootInfoBySession(ctx context.Context, p *SeedPayload) (*biz.BootDecision, error) {
	// BootInfo is keyed by MAC; the seed is bound to an active session, so a
	// direct session lookup is provided by the usecase via MachineID's MAC.
	return s.boot.(sessionResolver).BootInfoBySession(ctx, p.SessionID)
}

// sessionResolver is an optional extension implemented by the usecase to
// resolve a decision directly from a session id (used by the seed endpoints).
type sessionResolver interface {
	BootInfoBySession(ctx context.Context, sessionID string) (*biz.BootDecision, error)
}

func (s *Server) record(ctx context.Context, phase biz.Phase, outcome biz.Outcome, sessionID, mac string, detail map[string]any) {
	if s.events == nil {
		return
	}
	s.events.Record(ctx, biz.Event{
		SessionID: sessionID, MachineMAC: mac, Phase: phase, Outcome: outcome, Detail: detail,
	})
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, endpoint, sessionID, mac string, code int, err error) {
	s.log.Error("boot endpoint failed", "endpoint", endpoint, "session", sessionID, "err", err)
	s.record(r.Context(), biz.PhaseSessionFailed, biz.OutcomeError, sessionID, mac,
		map[string]any{"endpoint": endpoint, "error": err.Error()})
	http.Error(w, "internal error", code)
}
