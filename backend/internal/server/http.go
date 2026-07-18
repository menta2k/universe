package server

import (
	"log/slog"

	"github.com/go-kratos/kratos/v2/middleware/recovery"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"universe/backend/internal/biz"
	"universe/backend/internal/conf"
)

// loginAttemptsPerMinute bounds authentication attempts per client.
const loginAttemptsPerMinute = 10

// RegisterFn attaches a service to the HTTP server.
type RegisterFn func(*khttp.Server)

// NewHTTPServer builds the admin API server (:8080) with auth, audit,
// envelope encoding, /healthz and /metrics.
func NewHTTPServer(
	c *conf.Config,
	log *slog.Logger,
	metrics *Metrics,
	operators *biz.OperatorUsecase,
	events *biz.EventRecorder,
	registrars ...RegisterFn,
) *khttp.Server {
	srv := khttp.NewServer(
		khttp.Address(c.Server.HTTPAddr),
		khttp.Filter(SecurityHeaders),
		khttp.Middleware(
			recovery.Recovery(),
			RateLimitLogin(loginAttemptsPerMinute),
			AuthMiddleware(operators, "nb_session"),
			AuditMiddleware(events),
		),
		khttp.ResponseEncoder(ResponseEncoder),
		khttp.ErrorEncoder(ErrorEncoder),
	)
	srv.HandlePrefix("/metrics", promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{}))
	srv.HandleFunc("/healthz", healthz)
	for _, register := range registrars {
		register(srv)
	}
	log.Info("http server configured", "addr", c.Server.HTTPAddr)
	return srv
}
