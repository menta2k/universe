// Package data provides storage access: TimescaleDB (pgx), Valkey, and the
// on-disk artifact store. All repositories live here behind biz interfaces.
package data

import (
	"context"
	"embed"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // migrate pgx driver
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/valkey-io/valkey-go"

	"github.com/menta2k/universe/backend/internal/conf"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Data bundles the shared storage clients.
type Data struct {
	Pool   *pgxpool.Pool
	Valkey valkey.Client
}

// New connects to TimescaleDB and Valkey and verifies both are reachable.
func New(ctx context.Context, c *conf.Config) (*Data, func(), error) {
	pool, err := pgxpool.New(ctx, c.Database.DSN)
	if err != nil {
		return nil, nil, fmt.Errorf("create pgx pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("ping database: %w", err)
	}

	vk, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{c.Valkey.Addr},
		Password:    c.Valkey.Password,
	})
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("connect valkey: %w", err)
	}
	if err := vk.Do(pingCtx, vk.B().Ping().Build()).Error(); err != nil {
		pool.Close()
		vk.Close()
		return nil, nil, fmt.Errorf("ping valkey: %w", err)
	}

	cleanup := func() {
		pool.Close()
		vk.Close()
	}
	return &Data{Pool: pool, Valkey: vk}, cleanup, nil
}

// Migrate applies all pending migrations against the configured database.
func Migrate(dsn string) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, migrateDSN(dsn))
	if err != nil {
		return fmt.Errorf("init migrator: %w", err)
	}
	defer m.Close()
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// migrateDSN rewrites a postgres:// DSN for golang-migrate's pgx/v5 driver.
func migrateDSN(dsn string) string {
	return "pgx5://" + trimScheme(dsn)
}

func trimScheme(dsn string) string {
	for _, p := range []string{"postgres://", "postgresql://", "pgx5://"} {
		if len(dsn) > len(p) && dsn[:len(p)] == p {
			return dsn[len(p):]
		}
	}
	return dsn
}
