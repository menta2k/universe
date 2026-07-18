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
	"universe/backend/internal/server"
	"universe/backend/internal/service"
)

// app owns the wired services and their lifecycles.
type app struct {
	cfg     *conf.Config
	log     *slog.Logger
	httpSrv starterStopper
	grpcSrv starterStopper
}

// starterStopper matches kratos transport servers.
type starterStopper interface {
	Start(context.Context) error
	Stop(context.Context) error
}

const sessionTTL = 12 * time.Hour

// newApp wires config -> data -> biz -> service -> servers.
func newApp(ctx context.Context, cfg *conf.Config) (*app, func(), error) {
	log := server.NewLogger(slog.LevelInfo)
	metrics := server.NewMetrics()

	d, cleanup, err := data.New(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}

	events := biz.NewEventRecorder(data.NewEventRepo(d), data.NewEventPublisher(d), log)
	operators := biz.NewOperatorUsecase(
		data.NewOperatorRepo(d), data.NewSessionStore(d, sessionTTL), log)

	if err := operators.EnsureBootstrap(ctx,
		cfg.BootstrapOperator.Username, cfg.BootstrapOperator.Password); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("bootstrap operator: %w", err)
	}

	authSvc := service.NewAuthService(operators)

	httpSrv := server.NewHTTPServer(cfg, log, metrics, operators, events,
		func(s *khttp.Server) { v1.RegisterAuthServiceHTTPServer(s, authSvc) },
	)
	grpcSrv := server.NewGRPCServer(cfg, log, operators, events,
		func(s *kgrpc.Server) { v1.RegisterAuthServiceServer(s, authSvc) },
	)

	return &app{cfg: cfg, log: log, httpSrv: httpSrv, grpcSrv: grpcSrv}, cleanup, nil
}

// start launches every enabled service under the errgroup with graceful stop.
func (a *app) start(ctx context.Context, g *errgroup.Group) {
	runServer(ctx, g, a.log, "api-http", a.httpSrv)
	runServer(ctx, g, a.log, "grpc", a.grpcSrv)
}

func runServer(ctx context.Context, g *errgroup.Group, log *slog.Logger, name string, s starterStopper) {
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
