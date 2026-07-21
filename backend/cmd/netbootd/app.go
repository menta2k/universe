package main

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	kgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"golang.org/x/sync/errgroup"

	v1 "github.com/menta2k/universe/backend/api/netboot/v1"
	"github.com/menta2k/universe/backend/internal/biz"
	"github.com/menta2k/universe/backend/internal/conf"
	"github.com/menta2k/universe/backend/internal/data"
	"github.com/menta2k/universe/backend/internal/netboot"
	"github.com/menta2k/universe/backend/internal/netboot/autoinstall"
	"github.com/menta2k/universe/backend/internal/netboot/bootfiles"
	"github.com/menta2k/universe/backend/internal/netboot/bootsrv"
	"github.com/menta2k/universe/backend/internal/netboot/dhcp"
	"github.com/menta2k/universe/backend/internal/netboot/nfs"
	"github.com/menta2k/universe/backend/internal/netboot/tftp"
	"github.com/menta2k/universe/backend/internal/server"
	"github.com/menta2k/universe/backend/internal/service"
)

const sessionTTL = 12 * time.Hour

// app owns the wired services and their lifecycles.
type app struct {
	cfg *conf.Config
	log *slog.Logger

	httpSrv    *khttp.Server
	grpcSrv    *kgrpc.Server
	tftpSrv    *tftp.Server
	bootSrv    *bootsrv.Server
	dhcpCtl    *dhcp.Controller
	sweeper    *biz.SessionSweeper
	streamer   *server.EventStreamer
	dhcpConfig *biz.DhcpConfigUsecase
	bootFiles  *bootfiles.Fetcher

	// NFS-root mode (low-memory netboot): loop-mount each release ISO under
	// nfsDir/<release> and export nfsDir over NFSv3.
	nfsSrv      *nfs.Server
	artifacts   *data.ArtifactStore
	nfsDir      string
	nfsReleases []string
}

func newApp(ctx context.Context, cfg *conf.Config) (*app, func(), error) {
	log := server.NewLogger(slog.LevelInfo)
	metrics := server.NewMetrics()

	d, cleanup, err := data.New(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}

	// biz layer
	events := biz.NewEventRecorder(data.NewEventRepo(d), data.NewEventPublisher(d), log)
	operators := biz.NewOperatorUsecase(
		data.NewOperatorRepo(d), data.NewSessionStore(d, sessionTTL), log)
	machineRepo := data.NewMachineRepo(d)
	sessionRepo := data.NewSessionRepo(d)
	profileRepo := data.NewProfileRepo(d)
	machines := biz.NewMachineUsecase(machineRepo, sessionRepo, profileRepo,
		data.NewDhcpGate(d), events, log)
	sessions := biz.NewSessionUsecase(sessionRepo, machineRepo, events, log)
	profiles := biz.NewProfileUsecase(profileRepo, autoinstall.NewValidator(), log)
	bootFacade := biz.NewBootFacade(machines, sessions)

	artifactStore, err := data.NewArtifactStore(d, cfg.Artifacts.Root, cfg.Artifacts.MaxUploadBytes)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	artifacts := biz.NewArtifactUsecase(artifactStore, data.NewTransferRepo(d), log)
	bootFileFetcher := bootfiles.New(artifactStore, bootFilesConfig(cfg), log)
	sessionQuery := biz.NewSessionQueryUsecase(data.NewSessionQueryRepo(d))
	sweeper := biz.NewSessionSweeper(sessionRepo, machineRepo,
		events, cfg.Netboot.StaleSessionTimeout.Duration(), log)

	// DHCP runtime controller (reacts to enable/disable + config changes; FR-016).
	dhcpConflict := dhcp.NewConflictWatcher(hostIP(cfg.Server.ExternalBootURL),
		data.NewForeignOfferSink(d), log)
	dhcpCtl := dhcp.NewController(dhcp.Config{
		Interface:   cfg.Netboot.DHCPInterface,
		ServerIP:    hostIP(cfg.Server.ExternalBootURL),
		BootHTTPURL: cfg.Server.ExternalBootURL,
		Addr:        cfg.Netboot.DHCPAddr,
	}, d.Valkey, machines, events, dhcpConflict, log)
	dhcpRepo := data.NewDhcpConfigRepo(d)
	dhcpConfig := biz.NewDhcpConfigUsecase(dhcpRepo, data.NewLeaseRepo(d), dhcpRepo, dhcpCtl, log)

	if err := operators.EnsureBootstrap(ctx,
		cfg.BootstrapOperator.Username, cfg.BootstrapOperator.Password); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("bootstrap operator: %w", err)
	}

	// service layer
	authSvc := service.NewAuthService(operators)
	machineSvc := service.NewMachineService(machines)
	profileSvc := service.NewProfileService(profiles, machines)
	artifactSvc := service.NewArtifactService(artifacts, cfg.Artifacts.MaxUploadBytes)
	dhcpSvc := service.NewDhcpService(dhcpConfig)
	sessionSvc := service.NewSessionService(sessionQuery)
	streamer := server.NewEventStreamer(d.Valkey, data.EventsChannel, log)

	httpSrv := server.NewHTTPServer(cfg, log, metrics, operators, events,
		func(s *khttp.Server) { v1.RegisterAuthServiceHTTPServer(s, authSvc) },
		func(s *khttp.Server) { v1.RegisterMachineServiceHTTPServer(s, machineSvc) },
		func(s *khttp.Server) { v1.RegisterProfileServiceHTTPServer(s, profileSvc) },
		func(s *khttp.Server) { v1.RegisterArtifactServiceHTTPServer(s, artifactSvc) },
		func(s *khttp.Server) { artifactSvc.RegisterMultipart(s) },
		func(s *khttp.Server) { v1.RegisterDhcpServiceHTTPServer(s, dhcpSvc) },
		func(s *khttp.Server) { v1.RegisterSessionServiceHTTPServer(s, sessionSvc) },
		func(s *khttp.Server) { s.HandleFunc("/api/v1/events/stream", streamer.ServeHTTP) },
	)
	grpcSrv := server.NewGRPCServer(cfg, log, operators, events,
		func(s *kgrpc.Server) { v1.RegisterAuthServiceServer(s, authSvc) },
		func(s *kgrpc.Server) { v1.RegisterMachineServiceServer(s, machineSvc) },
		func(s *kgrpc.Server) { v1.RegisterProfileServiceServer(s, profileSvc) },
		func(s *kgrpc.Server) { v1.RegisterArtifactServiceServer(s, artifactSvc) },
		func(s *kgrpc.Server) { v1.RegisterDhcpServiceServer(s, dhcpSvc) },
		func(s *kgrpc.Server) { v1.RegisterSessionServiceServer(s, sessionSvc) },
	)

	// machine-facing servers
	tftpSrv := tftp.NewServer(data.NewTFTPFileSource(artifactStore), data.NewTransferLogger(d), log)
	tokens := bootsrv.NewTokenStore(d.Valkey, cfg.Netboot.SeedTokenTTL.Duration())
	bootSrv := bootsrv.NewServer(cfg.Server.ExternalBootURL, bootFacade, artifactStore,
		tokens, bootsrv.NewOneTimeCredentialMinter(), events, log, bootsrv.BootOptions{
			ServeISO:    cfg.BootFiles.ServeISO,
			NFSRoot:     cfg.BootFiles.NFSRoot,
			NFSServerIP: hostIP(cfg.Server.ExternalBootURL),
		})

	var nfsSrv *nfs.Server
	nfsDir := filepath.Join(cfg.Artifacts.Root, "nfs")
	if cfg.BootFiles.NFSRoot {
		nfsSrv = nfs.New(nfsDir, log)
	}

	return &app{
		cfg: cfg, log: log,
		httpSrv: httpSrv, grpcSrv: grpcSrv, tftpSrv: tftpSrv, bootSrv: bootSrv,
		dhcpCtl: dhcpCtl, sweeper: sweeper, streamer: streamer, dhcpConfig: dhcpConfig,
		bootFiles: bootFileFetcher,
		nfsSrv:    nfsSrv, artifacts: artifactStore, nfsDir: nfsDir,
		nfsReleases: configuredReleases(cfg),
	}, cleanup, nil
}

// configuredReleases returns the release codenames to prepare, matching the
// boot-file fetcher's default (noble + jammy) when none are configured.
func configuredReleases(cfg *conf.Config) []string {
	if len(cfg.BootFiles.Releases) > 0 {
		return cfg.BootFiles.Releases
	}
	return []string{"noble", "jammy"}
}

// bootFilesConfig maps the file config onto the fetcher's typed config.
func bootFilesConfig(cfg *conf.Config) bootfiles.Config {
	out := bootfiles.Config{ServeISO: cfg.BootFiles.ServeISO}
	for _, r := range cfg.BootFiles.Releases {
		out.Releases = append(out.Releases, biz.UbuntuRelease(r))
	}
	if len(cfg.BootFiles.ISOURLs) > 0 {
		out.ISOURLs = make(map[biz.UbuntuRelease]string, len(cfg.BootFiles.ISOURLs))
		for k, v := range cfg.BootFiles.ISOURLs {
			out.ISOURLs[biz.UbuntuRelease(k)] = v
		}
	}
	return out
}

// hostIP extracts the host from an external URL like "http://192.168.90.1:8082".
func hostIP(externalURL string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(externalURL, "http://"), "https://")
	if i := strings.IndexAny(s, ":/"); i >= 0 {
		return s[:i]
	}
	return s
}

// start launches every enabled service under the errgroup with graceful stop.
// The authoritative DHCP server is intentionally NOT started here: it is
// enabled only by explicit operator action (FR-016) and managed at runtime
// by the DHCP usecase (US3).
func (a *app) start(ctx context.Context, g *errgroup.Group) {
	startKratos(ctx, g, a.log, "api-http", a.httpSrv)
	startKratos(ctx, g, a.log, "grpc", a.grpcSrv)

	g.Go(func() error {
		addr, err := netboot.BindAddr(a.cfg.Netboot.TFTPInterface, a.cfg.Netboot.TFTPAddr)
		if err != nil {
			return fmt.Errorf("tftp: %w", err)
		}
		if err := a.tftpSrv.ListenAndServe(addr); err != nil {
			return fmt.Errorf("tftp: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		<-ctx.Done()
		a.tftpSrv.Shutdown()
		return nil
	})

	g.Go(func() error {
		addr, err := netboot.BindAddr(a.cfg.Server.BootHTTPInterface, a.cfg.Server.BootHTTPAddr)
		if err != nil {
			return fmt.Errorf("boot-http: %w", err)
		}
		if err := a.bootSrv.ListenAndServe(addr); err != nil {
			return fmt.Errorf("boot-http: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		<-ctx.Done()
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return a.bootSrv.Shutdown(stopCtx)
	})

	// DHCP: reconcile the runtime server with the persisted config at startup
	// (starts only if an operator previously enabled it; FR-016).
	g.Go(func() error {
		if cfg, err := a.dhcpConfig.Get(ctx); err != nil {
			a.log.Error("load dhcp config at startup", "err", err)
		} else {
			a.dhcpCtl.Reload(cfg)
		}
		<-ctx.Done()
		return a.dhcpCtl.Stop(context.Background())
	})

	// Stale-session sweeper (FR-015).
	g.Go(func() error {
		return a.sweeper.Run(ctx)
	})

	// Auto-fetch missing kernel/initrd boot files once at startup (background,
	// non-blocking; cached artifacts persist so subsequent starts are no-ops).
	if a.cfg.BootFiles.AutoFetch {
		g.Go(func() error {
			a.log.Info("boot-file auto-fetch starting")
			a.bootFiles.EnsureConfigured(ctx)
			// NFS-root mode: once each release ISO is fetched, loop-mount it so
			// the NFS export exposes the extracted tree.
			if a.cfg.BootFiles.NFSRoot {
				a.mountNFSReleases(ctx)
			}
			return nil
		})
	}

	// NFS server for low-memory netboot (netboot=nfs). Exports nfsDir; the
	// per-release ISO mounts appear under it as machines are fetched.
	//
	// A bind failure here (e.g. port 2049 already held by the host's kernel
	// NFS/rpcbind) MUST NOT take down the daemon: NFS-root is an optional
	// installer-delivery method, whereas DHCP/TFTP/boot-HTTP are the core
	// provisioning path. So the error is logged and swallowed rather than
	// returned into the errgroup, which would cancel every other server.
	if a.nfsSrv != nil {
		g.Go(func() error {
			if err := a.nfsSrv.ListenAndServe(a.nfsAddr()); err != nil && ctx.Err() == nil {
				a.log.Error("nfs: server stopped; NFS-root netboot unavailable "+
					"(core DHCP/TFTP/boot-HTTP unaffected)", "addr", a.nfsAddr(), "err", err)
			}
			return nil
		})
		g.Go(func() error {
			<-ctx.Done()
			a.unmountNFSReleases()
			return a.nfsSrv.Shutdown(context.Background())
		})
	}
}

// nfsAddr returns the configured NFS listen address, defaulting to :2049.
func (a *app) nfsAddr() string {
	if a.cfg.BootFiles.NFSAddr != "" {
		return a.cfg.BootFiles.NFSAddr
	}
	return ":2049"
}

// mountNFSReleases loop-mounts each configured release's ISO under nfsDir so
// casper can NFS-mount the extracted tree. Missing/unfetched ISOs are skipped.
func (a *app) mountNFSReleases(ctx context.Context) {
	for _, rel := range a.nfsReleases {
		art, err := a.artifacts.GetByFilename(ctx, rel+".iso")
		if err != nil {
			a.log.Warn("nfs: iso not available for release, skipping", "release", rel, "err", err)
			continue
		}
		mountpoint := filepath.Join(a.nfsDir, rel)
		if err := nfs.MountISO(art.Path, mountpoint); err != nil {
			a.log.Error("nfs: mount iso failed", "release", rel, "err", err)
			continue
		}
		a.log.Info("nfs: iso mounted", "release", rel, "mountpoint", mountpoint)
	}
}

// unmountNFSReleases unmounts every release mount created for NFS export.
func (a *app) unmountNFSReleases() {
	for _, rel := range a.nfsReleases {
		if err := nfs.Unmount(filepath.Join(a.nfsDir, rel)); err != nil {
			a.log.Error("nfs: unmount failed", "release", rel, "err", err)
		}
	}
}

type kratosServer interface {
	Start(context.Context) error
	Stop(context.Context) error
}

func startKratos(ctx context.Context, g *errgroup.Group, log *slog.Logger, name string, s kratosServer) {
	g.Go(func() error {
		log.Info("service starting", "service", name)
		if err := s.Start(ctx); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	})
	g.Go(func() error {
		<-ctx.Done()
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		log.Info("service stopping", "service", name)
		return s.Stop(stopCtx)
	})
}
