package biz

import (
	"context"
	"fmt"
	"log/slog"
	"net"
)

// DhcpSubnet is a configured address range with options.
type DhcpSubnet struct {
	ID         string
	Network    string // CIDR
	RangeStart string
	RangeEnd   string
	Gateway    string
	DNS        []string
}

// DhcpConfig is the whole address-service configuration (single logical row +
// its subnets). Enabled defaults to false and only flips on explicit action.
type DhcpConfig struct {
	Enabled         bool
	Version         int
	LeaseTTLSeconds int
	Subnets         []DhcpSubnet
}

// DhcpConfigRepo persists DHCP configuration transactionally.
type DhcpConfigRepo interface {
	Get(ctx context.Context) (*DhcpConfig, error)
	// Replace applies subnets + lease TTL in one transaction, bumping version.
	Replace(ctx context.Context, leaseTTL int, subnets []DhcpSubnet) (*DhcpConfig, error)
	SetEnabled(ctx context.Context, enabled bool) (*DhcpConfig, error)
}

// LeaseReader lists active leases (Valkey-backed).
type LeaseReader interface {
	ListLeases(ctx context.Context, page, pageSize int) ([]Lease, int64, error)
}

// ForeignServerReader lists observed competing DHCP servers.
type ForeignServerReader interface {
	ListForeignServers(ctx context.Context, page, pageSize int) ([]ForeignServer, int64, error)
}

// Lease is an active address assignment surfaced to the UI.
type Lease struct {
	IP          string
	MAC         string
	MachineID   string
	MachineName string
	ExpiresAt   int64 // unix seconds
}

// ForeignServer is a competing DHCP server observed on the segment.
type ForeignServer struct {
	ServerID   string
	LastSeen   int64
	OffersSeen int64
}

// DhcpReloadNotifier lets the usecase signal the running server to reload.
type DhcpReloadNotifier interface {
	Reload(cfg *DhcpConfig)
}

// DhcpConfigUsecase validates and applies DHCP configuration with
// last-valid-config semantics (FR-008) and explicit enable/disable (FR-016).
type DhcpConfigUsecase struct {
	repo     DhcpConfigRepo
	leases   LeaseReader
	foreign  ForeignServerReader
	notifier DhcpReloadNotifier
	log      *slog.Logger
}

func NewDhcpConfigUsecase(repo DhcpConfigRepo, leases LeaseReader, foreign ForeignServerReader, notifier DhcpReloadNotifier, log *slog.Logger) *DhcpConfigUsecase {
	return &DhcpConfigUsecase{repo: repo, leases: leases, foreign: foreign, notifier: notifier, log: log}
}

func (u *DhcpConfigUsecase) Get(ctx context.Context) (*DhcpConfig, error) {
	return u.repo.Get(ctx)
}

// Update validates and applies the whole config; on validation failure nothing
// changes and the running server keeps its last valid config.
func (u *DhcpConfigUsecase) Update(ctx context.Context, leaseTTL int, subnets []DhcpSubnet) (*DhcpConfig, error) {
	if err := validateDhcp(leaseTTL, subnets); err != nil {
		return nil, err
	}
	cfg, err := u.repo.Replace(ctx, leaseTTL, subnets)
	if err != nil {
		return nil, fmt.Errorf("apply dhcp config: %w", err)
	}
	if u.notifier != nil && cfg.Enabled {
		u.notifier.Reload(cfg)
	}
	return cfg, nil
}

func (u *DhcpConfigUsecase) Enable(ctx context.Context) (*DhcpConfig, error) {
	cfg, err := u.repo.SetEnabled(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("enable dhcp: %w", err)
	}
	if u.notifier != nil {
		u.notifier.Reload(cfg)
	}
	u.log.Info("dhcp service enabled by operator")
	return cfg, nil
}

func (u *DhcpConfigUsecase) Disable(ctx context.Context) (*DhcpConfig, error) {
	cfg, err := u.repo.SetEnabled(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("disable dhcp: %w", err)
	}
	if u.notifier != nil {
		u.notifier.Reload(cfg)
	}
	u.log.Info("dhcp service disabled by operator")
	return cfg, nil
}

func (u *DhcpConfigUsecase) ListLeases(ctx context.Context, page, pageSize int) ([]Lease, int64, error) {
	return u.leases.ListLeases(ctx, page, pageSize)
}

func (u *DhcpConfigUsecase) ListForeignServers(ctx context.Context, page, pageSize int) ([]ForeignServer, int64, error) {
	return u.foreign.ListForeignServers(ctx, page, pageSize)
}

// validateDhcp enforces range/subnet containment, non-overlap, TTL bounds.
func validateDhcp(leaseTTL int, subnets []DhcpSubnet) error {
	fields := map[string]string{}
	if leaseTTL < 300 || leaseTTL > 86400 {
		fields["lease_ttl_seconds"] = "must be between 300 and 86400"
	}
	type parsed struct {
		net        *net.IPNet
		start, end net.IP
	}
	var nets []parsed
	for i, s := range subnets {
		key := fmt.Sprintf("subnets[%d]", i)
		_, ipNet, err := net.ParseCIDR(s.Network)
		if err != nil {
			fields[key+".network"] = "invalid CIDR"
			continue
		}
		start := net.ParseIP(s.RangeStart)
		end := net.ParseIP(s.RangeEnd)
		if start == nil || end == nil {
			fields[key+".range"] = "invalid range address"
			continue
		}
		if !ipNet.Contains(start) || !ipNet.Contains(end) {
			fields[key+".range"] = "range must lie inside the subnet"
		}
		if bytesCompare(start, end) > 0 {
			fields[key+".range"] = "range_start must be <= range_end"
		}
		if s.Gateway != "" && net.ParseIP(s.Gateway) == nil {
			fields[key+".gateway"] = "invalid gateway address"
		}
		nets = append(nets, parsed{net: ipNet, start: start, end: end})
	}
	// Overlap check across subnets.
	for i := 0; i < len(nets); i++ {
		for j := i + 1; j < len(nets); j++ {
			if nets[i].net.Contains(nets[j].net.IP) || nets[j].net.Contains(nets[i].net.IP) {
				fields[fmt.Sprintf("subnets[%d].network", j)] = "overlaps another subnet"
			}
		}
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func bytesCompare(a, b net.IP) int {
	a4, b4 := a.To4(), b.To4()
	if a4 == nil || b4 == nil {
		return 0
	}
	for i := range 4 {
		if a4[i] != b4[i] {
			if a4[i] < b4[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
