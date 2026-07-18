package data

import (
	"context"
	"fmt"
)

// DhcpGate reads the DHCP enabled flag (biz.DhcpGate implementation).
type DhcpGate struct {
	data *Data
}

func NewDhcpGate(d *Data) *DhcpGate { return &DhcpGate{data: d} }

func (g *DhcpGate) Enabled(ctx context.Context) (bool, error) {
	var enabled bool
	if err := g.data.Pool.QueryRow(ctx,
		`SELECT enabled FROM dhcp_config`).Scan(&enabled); err != nil {
		return false, fmt.Errorf("read dhcp enabled flag: %w", err)
	}
	return enabled, nil
}
