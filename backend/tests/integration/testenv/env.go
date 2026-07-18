// Package testenv starts throwaway TimescaleDB and Valkey containers once per
// test binary and hands each test an isolated database. Container startup is
// the expensive part (tens of seconds), so it is shared; per-test isolation
// comes from a freshly created + migrated database and a flushed Valkey.
//
// Tests are skipped when Docker is unavailable.
package testenv

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"universe/backend/internal/conf"
	"universe/backend/internal/data"
)

// Env holds connection details for one test's isolated database.
type Env struct {
	DSN        string
	ValkeyAddr string
	Data       *data.Data
}

type shared struct {
	pgHost, pgPort string
	vkHost, vkPort string
	adminDSN       string
	err            error
	unavailable    bool
}

var (
	once    sync.Once
	sharedS shared
	dbSeq   atomicSeq
)

// Start returns an isolated environment. The first call boots the containers;
// subsequent calls reuse them with a fresh per-test database.
func Start(t *testing.T) *Env {
	t.Helper()
	once.Do(bootShared)
	if sharedS.unavailable {
		t.Skipf("docker not available, skipping integration test: %v", sharedS.err)
	}
	if sharedS.err != nil {
		t.Fatalf("shared test env failed: %v", sharedS.err)
	}

	ctx := context.Background()
	dbName := fmt.Sprintf("nbtest_%d", dbSeq.next())

	admin, cleanupAdmin, err := data.New(ctx, dsnConfig(sharedS.adminDSN))
	if err != nil {
		t.Fatalf("connect admin db: %v", err)
	}
	if _, err := admin.Pool.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		cleanupAdmin()
		t.Fatalf("create test db: %v", err)
	}
	cleanupAdmin()

	dsn := fmt.Sprintf("postgres://netboot:netboot-test@%s:%s/%s?sslmode=disable",
		sharedS.pgHost, sharedS.pgPort, dbName)
	if err := data.Migrate(dsn); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}

	cfg := dsnConfig(dsn)
	cfg.Valkey.Addr = fmt.Sprintf("%s:%s", sharedS.vkHost, sharedS.vkPort)
	d, cleanup, err := data.New(ctx, cfg)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	// Each test gets a clean Valkey keyspace.
	if err := d.Valkey.Do(ctx, d.Valkey.B().Flushdb().Build()).Error(); err != nil {
		t.Fatalf("flush valkey: %v", err)
	}
	t.Cleanup(cleanup)

	return &Env{DSN: dsn, ValkeyAddr: cfg.Valkey.Addr, Data: d}
}

func bootShared() {
	ctx := context.Background()
	if _, err := testcontainers.NewDockerProvider(); err != nil {
		sharedS.unavailable = true
		sharedS.err = err
		return
	}

	ts, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "timescale/timescaledb:2.17.2-pg16",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "postgres",
				"POSTGRES_USER":     "netboot",
				"POSTGRES_PASSWORD": "netboot-test",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(3 * time.Minute),
		},
	})
	if err != nil {
		sharedS.err = fmt.Errorf("start timescaledb: %w", err)
		return
	}

	vk, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "valkey/valkey:8.0",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForListeningPort("6379/tcp").WithStartupTimeout(time.Minute),
		},
	})
	if err != nil {
		sharedS.err = fmt.Errorf("start valkey: %w", err)
		return
	}

	sharedS.pgHost, sharedS.pgPort = hostPort(ctx, ts, "5432/tcp")
	sharedS.vkHost, sharedS.vkPort = hostPort(ctx, vk, "6379/tcp")
	sharedS.adminDSN = fmt.Sprintf("postgres://netboot:netboot-test@%s:%s/postgres?sslmode=disable",
		sharedS.pgHost, sharedS.pgPort)
}

func dsnConfig(dsn string) *conf.Config {
	cfg := &conf.Config{}
	cfg.Database.DSN = dsn
	cfg.Valkey.Addr = fmt.Sprintf("%s:%s", sharedS.vkHost, sharedS.vkPort)
	return cfg
}

func hostPort(ctx context.Context, c testcontainers.Container, port string) (string, string) {
	host, err := c.Host(ctx)
	if err != nil {
		panic(err)
	}
	mapped, err := c.MappedPort(ctx, port)
	if err != nil {
		panic(err)
	}
	return host, mapped.Port()
}

// atomicSeq is a tiny monotonic counter without importing sync/atomic ceremony.
type atomicSeq struct {
	mu sync.Mutex
	n  int
}

func (s *atomicSeq) next() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return s.n
}

// ensure strings import is used (host formatting helper kept for clarity).
var _ = strings.TrimSpace
