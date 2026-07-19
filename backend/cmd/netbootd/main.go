// netbootd is the Netboot Manager daemon: admin API (HTTP/gRPC) plus the
// machine-facing DHCP, TFTP, and boot-HTTP services, all run as toggleable
// goroutines under one errgroup (pattern from tinkerbell/smee).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/menta2k/universe/backend/internal/conf"
	"github.com/menta2k/universe/backend/internal/data"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("netbootd exited with error", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("netbootd", flag.ExitOnError)
	confPath := fs.String("conf", "configs/netbootd.example.yaml", "path to config file")
	if err := fs.Parse(migrateSubcommand(args)); err != nil {
		return err
	}

	cfg, err := conf.Load(*confPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if isMigrateRun(args) {
		if err := data.Migrate(cfg.Database.DSN); err != nil {
			return err
		}
		slog.Info("migrations applied")
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	app, cleanup, err := newApp(ctx, cfg)
	if err != nil {
		return fmt.Errorf("build application: %w", err)
	}
	defer cleanup()

	g, ctx := errgroup.WithContext(ctx)
	app.start(ctx, g)
	return g.Wait()
}

// migrateSubcommand strips the leading "migrate" verb so flag parsing works
// for both `netbootd -conf x` and `netbootd migrate -conf x`.
func migrateSubcommand(args []string) []string {
	if isMigrateRun(args) {
		return args[1:]
	}
	return args
}

func isMigrateRun(args []string) bool {
	return len(args) > 0 && args[0] == "migrate"
}
