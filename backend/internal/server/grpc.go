package server

import (
	"log/slog"

	"github.com/go-kratos/kratos/v2/middleware/recovery"
	kgrpc "github.com/go-kratos/kratos/v2/transport/grpc"

	"github.com/menta2k/universe/backend/internal/biz"
	"github.com/menta2k/universe/backend/internal/conf"
)

// GRPCRegisterFn attaches a service to the gRPC server.
type GRPCRegisterFn func(*kgrpc.Server)

// NewGRPCServer builds the gRPC server with the same auth/audit middleware
// chain as HTTP (session token via cookie is HTTP-only; gRPC callers are
// expected to be internal tooling on a trusted socket).
func NewGRPCServer(
	c *conf.Config,
	log *slog.Logger,
	operators *biz.OperatorUsecase,
	events *biz.EventRecorder,
	registrars ...GRPCRegisterFn,
) *kgrpc.Server {
	srv := kgrpc.NewServer(
		kgrpc.Address(c.Server.GRPCAddr),
		kgrpc.Middleware(
			recovery.Recovery(),
			AuthMiddleware(operators, "nb_session"),
			AuditMiddleware(events),
		),
	)
	for _, register := range registrars {
		register(srv)
	}
	log.Info("grpc server configured", "addr", c.Server.GRPCAddr)
	return srv
}
