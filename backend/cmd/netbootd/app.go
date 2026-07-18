package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	kgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"golang.org/x/sync/errgroup"

	v1 "universe/backend/api/netboot/v1"
	"universe/backend/internal/biz"
	"universe/backend/internal/conf"
	"universe/backend/internal/data"
	"universe/backend/internal/netboot/bootsrv"
	"universe/backend/internal/netboot/tftp"
	"universe/backend/internal/server"
	"universe/backend/internal/service"
)

const sessionTTL = 12 * time.Hour

// app owns the wired services and their lifecycles.
type app struct {
	cfg *conf.Config
	log *slog.Logger

	httpSrv *khttp.Server
	grpcSrv *kgrpc.Server
	tftpSrv *tftp.Server
	bootSrv *bootsrv.Server
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
	bootFacade := biz.NewBootFacade(machines, sessions)

	artifactStore, err := data.NewArtifactStore(d, cfg.Artifacts.Root, cfg.Artifacts.MaxUploadBytes)
	if err != nil {
		cleanup()
		return nil, nil, err
	}

	if err := operators.EnsureBootstrap(ctx,
		cfg.BootstrapOperator.Username, cfg.BootstrapOperator.Password); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("bootstrap operator: %w", err)
	}

	// service layer
	authSvc := service.NewAuthService(operators)
	machineSvc := service.NewMachineService(machines)

	httpSrv := server.NewHTTPServer(cfg, log, metrics, operators, events,
		func(s *khttp.Server) { v1.RegisterAuthServiceHTTPServer(s, authSvc) },
		func(s *khttp.Server) { v1.RegisterMachineServiceHTTPServer(s, machineSvc) },
	)
	grpcSrv := server.NewGRPCServer(cfg, log, operators, events,
		func(s *kgrpc.Server) { v1.RegisterAuthServiceServer(s, authSvc) },
		func(s *kgrpc.Server) { v1.RegisterMachineServiceServer(s, machineSvc) },
	)

	// machine-facing servers
	tftpSrv := tftp.NewServer(data.NewTFTPFileSource(artifactStore), data.NewTransferLogger(d), log)
	tokens := bootsrv.NewTokenStore(d.Valkey, cfg.Netboot.SeedTokenTTL.Duration())
	bootSrv := bootsrv.NewServer(cfg.Server.ExternalBootURL, bootFacade, artifactStore,
		tokens, bootsrv.NewOneTimeCredentialMinter(), events, log)

	return &app{
		cfg: cfg, log: log,
		httpSrv: httpSrv, grpcSrv: grpcSrv, tftpSrv: tftpSrv, bootSrv: bootSrv,
	}, cleanup, nil
}

// start launches every enabled service under the errgroup with graceful stop.
// The authoritative DHCP server is intentionally NOT started here: it is
// enabled only by explicit operator action (FR-016) and managed at runtime
// by the DHCP usecase (US3).
func (a *app) start(ctx context.Context, g *errgroup.Group) {
	startKratos(ctx, g, a.log, "api-http", a.httpSrv)
	startKratos(ctx, g, a.log, "grpc", a.grpcSrv)

	g.Go(func() error {
		if err := a.tftpSrv.ListenAndServe(a.cfg.Netboot.TFTPAddr); err != nil {
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
		if err := a.bootSrv.ListenAndServe(a.cfg.Server.BootHTTPAddr); err != nil {
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
