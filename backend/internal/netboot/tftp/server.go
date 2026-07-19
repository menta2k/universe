// Package tftp provides the read-only TFTP server for the netboot boot path
// (contracts/boot-protocols.md section 2). It serves the embedded iPXE
// binaries and, as a fallback, files from a pluggable FileSource (the
// artifact store). Writes are rejected and every RRQ is logged.
package tftp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"regexp"

	pintftp "github.com/pin/tftp/v3"

	"github.com/menta2k/universe/backend/internal/netboot"
)

// filenamePattern is the allowlist for requested filenames: a single path
// component, no separators, no traversal.
var filenamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// errTraversal marks filenames rejected by the allowlist.
var errTraversal = errors.New("filename not allowed")

// FileSource opens a file for reading by name. Implemented by the artifact
// store; size may be -1 when unknown.
type FileSource interface {
	Open(ctx context.Context, filename string) (io.ReadCloser, int64, error)
}

// TransferLogger records the outcome of every TFTP read request.
type TransferLogger interface {
	LogTransfer(ctx context.Context, clientIP string, filename string, bytes int64, success bool, errMsg string)
}

// Server is a read-only TFTP server wrapping github.com/pin/tftp/v3.
type Server struct {
	source   FileSource
	logger   TransferLogger
	log      *slog.Logger
	embedded map[string][]byte
	srv      *pintftp.Server
}

// NewServer builds a Server serving the embedded iPXE binaries first and
// falling back to source. Write requests (WRQ) are rejected because no write
// handler is registered.
func NewServer(source FileSource, logger TransferLogger, log *slog.Logger) *Server {
	s := &Server{
		source:   source,
		logger:   logger,
		log:      log,
		embedded: netboot.IPXEBinaries(),
	}
	s.srv = pintftp.NewServer(s.handleRead, nil)
	return s
}

// ListenAndServe binds addr (e.g. ":69") and serves until Shutdown.
func (s *Server) ListenAndServe(addr string) error {
	if err := s.srv.ListenAndServe(addr); err != nil {
		return fmt.Errorf("tftp server on %s: %w", addr, err)
	}
	return nil
}

// Serve serves on an already opened UDP connection until Shutdown. Useful for
// binding an ephemeral port before starting the server.
func (s *Server) Serve(conn net.PacketConn) error {
	if err := s.srv.Serve(conn); err != nil {
		return fmt.Errorf("tftp server: %w", err)
	}
	return nil
}

// Shutdown stops the server and unblocks ListenAndServe/Serve.
func (s *Server) Shutdown() {
	s.srv.Shutdown()
}

// handleRead is the RRQ handler passed to pin/tftp. It validates the
// filename, opens the content, streams it, and logs the outcome.
func (s *Server) handleRead(filename string, rf io.ReaderFrom) error {
	ctx := context.Background()
	clientIP := remoteIP(rf)

	n, err := s.serveFile(ctx, filename, rf)
	if err != nil {
		s.log.Warn("tftp read denied",
			"file", filename, "client", clientIP, "err", err)
		s.logger.LogTransfer(ctx, clientIP, filename, n, false, err.Error())
		return err
	}

	s.log.Info("tftp read served",
		"file", filename, "client", clientIP, "bytes", n)
	s.logger.LogTransfer(ctx, clientIP, filename, n, true, "")
	return nil
}

// serveFile resolves and streams filename, returning the bytes transferred.
func (s *Server) serveFile(ctx context.Context, filename string, rf io.ReaderFrom) (int64, error) {
	if !filenamePattern.MatchString(filename) || filename == "." || filename == ".." {
		return 0, fmt.Errorf("%w: %q", errTraversal, filename)
	}

	reader, size, err := s.open(ctx, filename)
	if err != nil {
		return 0, err
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			s.log.Warn("tftp close file failed", "file", filename, "err", closeErr)
		}
	}()

	if size >= 0 {
		if ot, ok := rf.(pintftp.OutgoingTransfer); ok {
			ot.SetSize(size)
		}
	}

	n, err := rf.ReadFrom(reader)
	if err != nil {
		return n, fmt.Errorf("transfer %q failed after %d bytes: %w", filename, n, err)
	}
	return n, nil
}

// open resolves filename: embedded iPXE binaries first, then the FileSource.
func (s *Server) open(ctx context.Context, filename string) (io.ReadCloser, int64, error) {
	if content, ok := s.embedded[filename]; ok {
		return io.NopCloser(bytes.NewReader(content)), int64(len(content)), nil
	}
	reader, size, err := s.source.Open(ctx, filename)
	if err != nil {
		return nil, 0, fmt.Errorf("file not found %q: %w", filename, err)
	}
	return reader, size, nil
}

// remoteIP extracts the client IP from the raw transfer when available.
func remoteIP(rf io.ReaderFrom) string {
	ot, ok := rf.(pintftp.OutgoingTransfer)
	if !ok {
		return ""
	}
	addr := ot.RemoteAddr()
	return addr.IP.String()
}
