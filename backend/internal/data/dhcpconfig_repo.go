package data

import (
	"context"
	"fmt"

	"universe/backend/internal/biz"
)

// DhcpConfigRepo is the pgx implementation of biz.DhcpConfigRepo.
type DhcpConfigRepo struct {
	data *Data
}

func NewDhcpConfigRepo(d *Data) *DhcpConfigRepo { return &DhcpConfigRepo{data: d} }

func (r *DhcpConfigRepo) Get(ctx context.Context) (*biz.DhcpConfig, error) {
	cfg := &biz.DhcpConfig{}
	if err := r.data.Pool.QueryRow(ctx,
		`SELECT enabled, version, lease_ttl_seconds FROM dhcp_config`).
		Scan(&cfg.Enabled, &cfg.Version, &cfg.LeaseTTLSeconds); err != nil {
		return nil, fmt.Errorf("read dhcp config: %w", err)
	}
	subnets, err := r.subnets(ctx)
	if err != nil {
		return nil, err
	}
	cfg.Subnets = subnets
	return cfg, nil
}

func (r *DhcpConfigRepo) subnets(ctx context.Context) ([]biz.DhcpSubnet, error) {
	rows, err := r.data.Pool.Query(ctx,
		`SELECT id, network::text, range_start::text, range_end::text,
		        coalesce(gateway::text,''), coalesce(array_to_string(dns,','),'')
		 FROM dhcp_subnets ORDER BY network`)
	if err != nil {
		return nil, fmt.Errorf("list subnets: %w", err)
	}
	defer rows.Close()
	var out []biz.DhcpSubnet
	for rows.Next() {
		var s biz.DhcpSubnet
		var dns string
		if err := rows.Scan(&s.ID, &s.Network, &s.RangeStart, &s.RangeEnd, &s.Gateway, &dns); err != nil {
			return nil, fmt.Errorf("scan subnet: %w", err)
		}
		if dns != "" {
			s.DNS = splitComma(dns)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Replace applies the new subnet set and TTL in one transaction, bumping version.
func (r *DhcpConfigRepo) Replace(ctx context.Context, leaseTTL int, subnets []biz.DhcpSubnet) (*biz.DhcpConfig, error) {
	tx, err := r.data.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`UPDATE dhcp_config SET lease_ttl_seconds = $1, version = version + 1, updated_at = now()`,
		leaseTTL); err != nil {
		return nil, fmt.Errorf("update dhcp config: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM dhcp_subnets`); err != nil {
		return nil, fmt.Errorf("clear subnets: %w", err)
	}
	for _, s := range subnets {
		if _, err := tx.Exec(ctx,
			`INSERT INTO dhcp_subnets (network, range_start, range_end, gateway, dns)
			 VALUES ($1::cidr, $2::inet, $3::inet, NULLIF($4,'')::inet, $5::inet[])`,
			s.Network, s.RangeStart, s.RangeEnd, s.Gateway, pgInetArray(s.DNS)); err != nil {
			return nil, fmt.Errorf("insert subnet %s: %w", s.Network, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit dhcp config: %w", err)
	}
	return r.Get(ctx)
}

func (r *DhcpConfigRepo) SetEnabled(ctx context.Context, enabled bool) (*biz.DhcpConfig, error) {
	if _, err := r.data.Pool.Exec(ctx,
		`UPDATE dhcp_config SET enabled = $1, updated_at = now()`, enabled); err != nil {
		return nil, fmt.Errorf("set dhcp enabled: %w", err)
	}
	return r.Get(ctx)
}

// ListForeignServers aggregates dhcp_offers_seen (FR-016).
func (r *DhcpConfigRepo) ListForeignServers(ctx context.Context, page, pageSize int) ([]biz.ForeignServer, int64, error) {
	var total int64
	if err := r.data.Pool.QueryRow(ctx,
		`SELECT count(DISTINCT server_id) FROM dhcp_offers_seen`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count foreign servers: %w", err)
	}
	p, size := normalizePage(page, pageSize)
	rows, err := r.data.Pool.Query(ctx,
		`SELECT server_id::text, extract(epoch FROM max(time))::bigint, count(*)
		 FROM dhcp_offers_seen GROUP BY server_id ORDER BY max(time) DESC LIMIT $1 OFFSET $2`,
		size, (p-1)*size)
	if err != nil {
		return nil, 0, fmt.Errorf("list foreign servers: %w", err)
	}
	defer rows.Close()
	var out []biz.ForeignServer
	for rows.Next() {
		var f biz.ForeignServer
		if err := rows.Scan(&f.ServerID, &f.LastSeen, &f.OffersSeen); err != nil {
			return nil, 0, fmt.Errorf("scan foreign server: %w", err)
		}
		out = append(out, f)
	}
	return out, total, rows.Err()
}

func splitComma(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// pgInetArray builds a Postgres inet[] literal from string IPs.
func pgInetArray(ips []string) string {
	if len(ips) == 0 {
		return "{}"
	}
	lit := "{"
	for i, ip := range ips {
		if i > 0 {
			lit += ","
		}
		lit += ip
	}
	return lit + "}"
}
