// Structured logging and Prometheus metrics shared by all services.
package server

import (
	"log/slog"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// NewLogger returns the process-wide JSON logger.
func NewLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

// Metrics holds pre-registered Prometheus collectors (labels initialized up
// front, following the smee pattern, so scrapes see all series immediately).
type Metrics struct {
	Registry *prometheus.Registry

	DHCPPackets   *prometheus.CounterVec // labels: msgtype, outcome
	TFTPTransfers *prometheus.CounterVec // labels: outcome
	BootHTTP      *prometheus.CounterVec // labels: endpoint, outcome
	APIRequests   *prometheus.CounterVec // labels: operation, code
	SessionsTotal *prometheus.CounterVec // labels: result
}

func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	factory := promauto.With(reg)
	m := &Metrics{
		Registry: reg,
		DHCPPackets: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "netboot_dhcp_packets_total", Help: "DHCP packets processed",
		}, []string{"msgtype", "outcome"}),
		TFTPTransfers: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "netboot_tftp_transfers_total", Help: "TFTP transfers served",
		}, []string{"outcome"}),
		BootHTTP: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "netboot_boot_http_requests_total", Help: "Boot HTTP requests",
		}, []string{"endpoint", "outcome"}),
		APIRequests: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "netboot_api_requests_total", Help: "Admin API requests",
		}, []string{"operation", "code"}),
		SessionsTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "netboot_sessions_total", Help: "Provisioning sessions by result",
		}, []string{"result"}),
	}
	for _, outcome := range []string{"ok", "error", "denied"} {
		m.TFTPTransfers.WithLabelValues(outcome)
	}
	for _, result := range []string{"completed", "failed", "stale"} {
		m.SessionsTotal.WithLabelValues(result)
	}
	return m
}
