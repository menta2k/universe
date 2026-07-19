package dhcp

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/menta2k/universe/backend/internal/biz"
)

// Controller owns the lifecycle of the authoritative DHCP server. The server
// runs only while enabled (FR-016); Reload rebuilds it from new config, and
// enable/disable start/stop it. It implements biz.DhcpReloadNotifier.
type Controller struct {
	cfg       Config
	vk        valkey.Client
	decider   BootDecider
	events    EventSink
	conflicts *ConflictWatcher
	leaseTTL  time.Duration
	log       *slog.Logger

	mu      sync.Mutex
	current *Server
	running bool
}

func NewController(cfg Config, vk valkey.Client, decider BootDecider, events EventSink, conflicts *ConflictWatcher, log *slog.Logger) *Controller {
	return &Controller{cfg: cfg, vk: vk, decider: decider, events: events, conflicts: conflicts, log: log}
}

// Reload (re)starts the server with the given config when enabled, or stops it
// when disabled. Safe to call repeatedly.
func (c *Controller) Reload(cfg *biz.DhcpConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stopLocked()
	if cfg == nil || !cfg.Enabled {
		c.log.Info("dhcp server not running (disabled)")
		return
	}

	subnets, reservations := buildPoolInputs(cfg)
	ttl := time.Duration(cfg.LeaseTTLSeconds) * time.Second
	pool := NewLeasePool(c.vk, subnets, reservations, ttl)
	srv := NewServer(c.cfg, pool, c.decider, c.events, c.conflicts, c.log)
	c.current = srv
	c.running = true
	go func() {
		if err := srv.ListenAndServe(c.cfg.dhcpAddr()); err != nil {
			c.log.Error("dhcp server stopped", "err", err)
		}
	}()
	c.log.Info("dhcp server (re)started", "subnets", len(subnets))
}

// Stop shuts the server down on process exit.
func (c *Controller) Stop(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopLocked()
	return nil
}

func (c *Controller) stopLocked() {
	if c.current != nil {
		c.current.Shutdown()
		c.current = nil
	}
	c.running = false
}

// buildPoolInputs converts biz config into pool inputs, skipping malformed
// entries (validation already happened at the usecase; this is defensive).
func buildPoolInputs(cfg *biz.DhcpConfig) ([]Subnet, []Reservation) {
	var subnets []Subnet
	for _, s := range cfg.Subnets {
		_, network, err := net.ParseCIDR(s.Network)
		if err != nil {
			continue
		}
		subnets = append(subnets, Subnet{
			Network:    network,
			RangeStart: net.ParseIP(s.RangeStart),
			RangeEnd:   net.ParseIP(s.RangeEnd),
			Gateway:    net.ParseIP(s.Gateway),
			DNS:        parseIPs(s.DNS),
		})
	}
	return subnets, nil
}

func parseIPs(ss []string) []net.IP {
	var out []net.IP
	for _, s := range ss {
		if ip := net.ParseIP(s); ip != nil {
			out = append(out, ip)
		}
	}
	return out
}
