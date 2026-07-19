package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	pintftp "github.com/pin/tftp/v3"

	"github.com/menta2k/universe/backend/internal/netboot"
	nbtftp "github.com/menta2k/universe/backend/internal/netboot/tftp"
)

// recordedTransfer captures one TransferLogger invocation.
type recordedTransfer struct {
	ClientIP string
	Filename string
	Bytes    int64
	Success  bool
	ErrMsg   string
}

// recordingLogger is a thread-safe fake TransferLogger.
type recordingLogger struct {
	mu      sync.Mutex
	records []recordedTransfer
}

func (r *recordingLogger) LogTransfer(_ context.Context, clientIP, filename string, byteCount int64, success bool, errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, recordedTransfer{
		ClientIP: clientIP,
		Filename: filename,
		Bytes:    byteCount,
		Success:  success,
		ErrMsg:   errMsg,
	})
}

func (r *recordingLogger) snapshot() []recordedTransfer {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedTransfer(nil), r.records...)
}

// waitForRecord polls until the logger has a record for filename or times out.
func (r *recordingLogger) waitForRecord(t *testing.T, filename string) recordedTransfer {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, rec := range r.snapshot() {
			if rec.Filename == filename {
				return rec
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no transfer record for %q within timeout; records: %+v", filename, r.snapshot())
	return recordedTransfer{}
}

// emptySource is a FileSource with no files (embedded binaries only).
type emptySource struct{}

func (emptySource) Open(_ context.Context, filename string) (io.ReadCloser, int64, error) {
	return nil, 0, fmt.Errorf("no artifact %q", filename)
}

// startTFTPServer binds an ephemeral UDP port and serves on it.
func startTFTPServer(t *testing.T, logger nbtftp.TransferLogger) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind ephemeral UDP port: %v", err)
	}
	srv := nbtftp.NewServer(emptySource{}, logger,
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	done := make(chan error, 1)
	go func() { done <- srv.Serve(conn) }()
	t.Cleanup(func() {
		srv.Shutdown()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("tftp server did not shut down within timeout")
		}
	})
	return conn.LocalAddr().String()
}

func TestTFTPServesEmbeddedIPXEBinary(t *testing.T) {
	logger := &recordingLogger{}
	addr := startTFTPServer(t, logger)

	client, err := pintftp.NewClient(addr)
	if err != nil {
		t.Fatalf("tftp client: %v", err)
	}
	client.SetTimeout(2 * time.Second)
	client.RequestTSize(true)

	wt, err := client.Receive(netboot.IPXEBinBIOS, "octet")
	if err != nil {
		t.Fatalf("receive %q: %v", netboot.IPXEBinBIOS, err)
	}
	var buf bytes.Buffer
	n, err := wt.WriteTo(&buf)
	if err != nil {
		t.Fatalf("read transfer: %v", err)
	}

	want := netboot.IPXEBinaries()[netboot.IPXEBinBIOS]
	if n != int64(len(want)) {
		t.Errorf("received %d bytes, want %d", n, len(want))
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Error("received content differs from embedded binary")
	}

	rec := logger.waitForRecord(t, netboot.IPXEBinBIOS)
	if !rec.Success {
		t.Errorf("transfer logged as failure: %q", rec.ErrMsg)
	}
	if rec.Bytes != int64(len(want)) {
		t.Errorf("logged %d bytes, want %d", rec.Bytes, len(want))
	}
	if rec.ClientIP != "127.0.0.1" {
		t.Errorf("logged client IP %q, want 127.0.0.1", rec.ClientIP)
	}
}

func TestTFTPMissingFileRejected(t *testing.T) {
	logger := &recordingLogger{}
	addr := startTFTPServer(t, logger)

	client, err := pintftp.NewClient(addr)
	if err != nil {
		t.Fatalf("tftp client: %v", err)
	}
	client.SetTimeout(2 * time.Second)

	wt, err := client.Receive("does-not-exist.img", "octet")
	if err == nil {
		var buf bytes.Buffer
		if _, werr := wt.WriteTo(&buf); werr == nil {
			t.Fatal("received missing file successfully, want error")
		}
	}

	rec := logger.waitForRecord(t, "does-not-exist.img")
	if rec.Success {
		t.Error("missing file logged as success")
	}
	if rec.ErrMsg == "" {
		t.Error("missing file logged without error message")
	}
}
