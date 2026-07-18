// Package testenv starts throwaway TimescaleDB and Valkey containers for
// integration tests. Tests are skipped when Docker is unavailable.
package testenv

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"universe/backend/internal/conf"
	"universe/backend/internal/data"
)

// Env holds connection details for the ephemeral containers.
type Env struct {
	DSN        string
	ValkeyAddr string
	Data       *data.Data
}

// Start launches TimescaleDB + Valkey, applies migrations, and returns
// connected clients. Cleanup is registered on t automatically.
func Start(t *testing.T) *Env {
	t.Helper()
	ctx := context.Background()

	if _, err := testcontainers.NewDockerProvider(); err != nil {
		t.Skipf("docker not available, skipping integration test: %v", err)
	}

	ts, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "timescale/timescaledb:2.17.2-pg16",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "netboot",
				"POSTGRES_USER":     "netboot",
				"POSTGRES_PASSWORD": "netboot-test",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(2 * time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("start timescaledb: %v", err)
	}
	t.Cleanup(func() { _ = ts.Terminate(context.Background()) })

	vk, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "valkey/valkey:8.0",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForListeningPort("6379/tcp").WithStartupTimeout(time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("start valkey: %v", err)
	}
	t.Cleanup(func() { _ = vk.Terminate(context.Background()) })

	dsn := containerDSN(t, ctx, ts, "5432/tcp",
		"postgres://netboot:netboot-test@%s:%s/netboot?sslmode=disable")
	vkHost, vkPort := hostPort(t, ctx, vk, "6379/tcp")

	if err := data.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := &conf.Config{}
	cfg.Database.DSN = dsn
	cfg.Valkey.Addr = fmt.Sprintf("%s:%s", vkHost, vkPort)
	d, cleanup, err := data.New(ctx, cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(cleanup)

	return &Env{DSN: dsn, ValkeyAddr: cfg.Valkey.Addr, Data: d}
}

func containerDSN(t *testing.T, ctx context.Context, c testcontainers.Container, port, format string) string {
	t.Helper()
	host, p := hostPort(t, ctx, c, port)
	return fmt.Sprintf(format, host, p)
}

func hostPort(t *testing.T, ctx context.Context, c testcontainers.Container, port string) (string, string) {
	t.Helper()
	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	mapped, err := c.MappedPort(ctx, port)
	if err != nil {
		t.Fatalf("container port: %v", err)
	}
	return host, mapped.Port()
}
