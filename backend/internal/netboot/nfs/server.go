// Package nfs runs an embedded read-only NFSv3 server that exports the
// extracted Ubuntu live-server ISO tree. Booting via netboot=nfs mounts the
// squashfs live over NFS (paged on demand) instead of copying the whole ISO
// into RAM (url=<iso>), so low-memory targets (4 GB) can install. NFSv3 is used
// because casper's initramfs mounts v3 natively.
package nfs

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	billyosfs "github.com/go-git/go-billy/v5/osfs"
	gonfs "github.com/willscott/go-nfs"
	nfshelper "github.com/willscott/go-nfs/helpers"
)

// Server exports a directory tree over NFSv3.
type Server struct {
	root string
	log  *slog.Logger
	ln   net.Listener
}

// New returns a server that will export root read-only.
func New(root string, log *slog.Logger) *Server {
	return &Server{root: root, log: log}
}

// ListenAndServe binds addr (e.g. ":2049") and serves until the listener is
// closed. The whole subtree under root is exported as the NFS root ("/").
func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("nfs listen %s: %w", addr, err)
	}
	s.ln = ln
	// Silence go-nfs's global logger; we log lifecycle ourselves.
	gonfs.SetLogger(nfsLogger{})
	handler := nfshelper.NewNullAuthHandler(billyosfs.New(s.root))
	cache := nfshelper.NewCachingHandler(handler, 4096)
	s.log.Info("nfs server listening", "addr", addr, "root", s.root)
	if err := gonfs.Serve(ln, cache); err != nil {
		// Serve returns an error when the listener is closed on shutdown; the
		// caller treats a shutdown-triggered error as normal.
		return fmt.Errorf("nfs serve: %w", err)
	}
	return nil
}

// Shutdown stops the server by closing its listener.
func (s *Server) Shutdown(_ context.Context) error {
	if s.ln != nil {
		return s.ln.Close()
	}
	return nil
}

// nfsLogger discards go-nfs's verbose per-request logging.
type nfsLogger struct{}

func (nfsLogger) Panic(_ ...any)            {}
func (nfsLogger) Panicf(_ string, _ ...any) {}
func (nfsLogger) Print(_ ...any)            {}
func (nfsLogger) Printf(_ string, _ ...any) {}
func (nfsLogger) Debug(_ ...any)            {}
func (nfsLogger) Debugf(_ string, _ ...any) {}
func (nfsLogger) Error(_ ...any)            {}
func (nfsLogger) Errorf(_ string, _ ...any) {}
func (nfsLogger) Warn(_ ...any)             {}
func (nfsLogger) Warnf(_ string, _ ...any)  {}
func (nfsLogger) Info(_ ...any)             {}
func (nfsLogger) Infof(_ string, _ ...any)  {}
func (nfsLogger) Trace(_ ...any)            {}
func (nfsLogger) Tracef(_ string, _ ...any) {}
func (nfsLogger) Fatal(_ ...any)            {}
func (nfsLogger) Fatalf(_ string, _ ...any) {}
func (nfsLogger) GetLevel() gonfs.LogLevel  { return gonfs.PanicLevel }
func (nfsLogger) SetLevel(_ gonfs.LogLevel) {}
func (nfsLogger) ParseLevel(_ string) (gonfs.LogLevel, error) {
	return gonfs.PanicLevel, nil
}
