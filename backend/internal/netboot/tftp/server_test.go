package tftp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/menta2k/universe/backend/internal/netboot"
)

// fakeSource is a FileSource backed by an in-memory map.
type fakeSource struct {
	files map[string][]byte
	calls []string
	mu    sync.Mutex
}

func (f *fakeSource) Open(_ context.Context, filename string) (io.ReadCloser, int64, error) {
	f.mu.Lock()
	f.calls = append(f.calls, filename)
	f.mu.Unlock()
	content, ok := f.files[filename]
	if !ok {
		return nil, 0, errors.New("file not found in source")
	}
	return io.NopCloser(bytes.NewReader(content)), int64(len(content)), nil
}

func (f *fakeSource) opened() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// transferRecord captures one LogTransfer invocation.
type transferRecord struct {
	ClientIP string
	Filename string
	Bytes    int64
	Success  bool
	ErrMsg   string
}

// fakeLogger is a TransferLogger recording every call.
type fakeLogger struct {
	records []transferRecord
	mu      sync.Mutex
}

func (f *fakeLogger) LogTransfer(_ context.Context, clientIP, filename string, byteCount int64, success bool, errMsg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, transferRecord{
		ClientIP: clientIP,
		Filename: filename,
		Bytes:    byteCount,
		Success:  success,
		ErrMsg:   errMsg,
	})
}

func (f *fakeLogger) all() []transferRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]transferRecord(nil), f.records...)
}

// fakeReaderFrom implements io.ReaderFrom plus the parts of
// tftp.OutgoingTransfer the handler relies on (SetSize, RemoteAddr).
type fakeReaderFrom struct {
	buf     bytes.Buffer
	size    int64
	sizeSet bool
	remote  net.UDPAddr
	readErr error
}

func (f *fakeReaderFrom) ReadFrom(r io.Reader) (int64, error) {
	if f.readErr != nil {
		return 0, f.readErr
	}
	return f.buf.ReadFrom(r)
}

func (f *fakeReaderFrom) SetSize(n int64) {
	f.size = n
	f.sizeSet = true
}

func (f *fakeReaderFrom) RemoteAddr() net.UDPAddr {
	return f.remote
}

func newTestServer(source FileSource, logger TransferLogger) *Server {
	return NewServer(source, logger, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func newFakeRF() *fakeReaderFrom {
	return &fakeReaderFrom{remote: net.UDPAddr{IP: net.IPv4(192, 0, 2, 10), Port: 40000}}
}

func TestHandleReadRejectsInvalidFilenames(t *testing.T) {
	tests := []struct {
		name     string
		filename string
	}{
		{"traversal", "../../etc/passwd"},
		{"subdirectory", "a/b"},
		{"backslash", `a\b`},
		{"dot dot", ".."},
		{"empty", ""},
		{"space", "file name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &fakeSource{files: map[string][]byte{}}
			logger := &fakeLogger{}
			srv := newTestServer(source, logger)

			err := srv.handleRead(tt.filename, newFakeRF())
			if err == nil {
				t.Fatalf("handleRead(%q) = nil, want error", tt.filename)
			}
			if got := source.opened(); len(got) != 0 {
				t.Errorf("FileSource consulted for denied filename %q: %v", tt.filename, got)
			}
			records := logger.all()
			if len(records) != 1 {
				t.Fatalf("got %d log records, want 1", len(records))
			}
			rec := records[0]
			if rec.Success {
				t.Errorf("logged success=true for denied filename %q", tt.filename)
			}
			if rec.ErrMsg == "" {
				t.Error("logged empty error message for denied filename")
			}
		})
	}
}

func TestHandleReadServesEmbeddedBinary(t *testing.T) {
	source := &fakeSource{files: map[string][]byte{}}
	logger := &fakeLogger{}
	srv := newTestServer(source, logger)
	rf := newFakeRF()

	if err := srv.handleRead(netboot.IPXEBinBIOS, rf); err != nil {
		t.Fatalf("handleRead(%q) failed: %v", netboot.IPXEBinBIOS, err)
	}

	want := netboot.IPXEBinaries()[netboot.IPXEBinBIOS]
	if rf.buf.Len() != len(want) {
		t.Errorf("served %d bytes, want %d", rf.buf.Len(), len(want))
	}
	if !rf.sizeSet || rf.size != int64(len(want)) {
		t.Errorf("SetSize = (%v, %d), want (true, %d)", rf.sizeSet, rf.size, len(want))
	}
	if got := source.opened(); len(got) != 0 {
		t.Errorf("FileSource consulted for embedded binary: %v", got)
	}

	records := logger.all()
	if len(records) != 1 {
		t.Fatalf("got %d log records, want 1", len(records))
	}
	rec := records[0]
	if !rec.Success {
		t.Errorf("logged success=false: %q", rec.ErrMsg)
	}
	if rec.Bytes != int64(len(want)) {
		t.Errorf("logged %d bytes, want %d", rec.Bytes, len(want))
	}
	if rec.ClientIP != "192.0.2.10" {
		t.Errorf("logged client IP %q, want %q", rec.ClientIP, "192.0.2.10")
	}
	if rec.Filename != netboot.IPXEBinBIOS {
		t.Errorf("logged filename %q, want %q", rec.Filename, netboot.IPXEBinBIOS)
	}
}

func TestHandleReadFallsBackToFileSource(t *testing.T) {
	content := []byte("custom artifact payload")
	source := &fakeSource{files: map[string][]byte{"custom.img": content}}
	logger := &fakeLogger{}
	srv := newTestServer(source, logger)
	rf := newFakeRF()

	if err := srv.handleRead("custom.img", rf); err != nil {
		t.Fatalf("handleRead(custom.img) failed: %v", err)
	}
	if !bytes.Equal(rf.buf.Bytes(), content) {
		t.Errorf("served content mismatch: got %d bytes, want %d", rf.buf.Len(), len(content))
	}
	if !rf.sizeSet || rf.size != int64(len(content)) {
		t.Errorf("SetSize = (%v, %d), want (true, %d)", rf.sizeSet, rf.size, len(content))
	}
	records := logger.all()
	if len(records) != 1 || !records[0].Success {
		t.Fatalf("expected one success record, got %+v", records)
	}
}

func TestHandleReadNotFoundLogged(t *testing.T) {
	source := &fakeSource{files: map[string][]byte{}}
	logger := &fakeLogger{}
	srv := newTestServer(source, logger)

	err := srv.handleRead("missing.img", newFakeRF())
	if err == nil {
		t.Fatal("handleRead(missing.img) = nil, want error")
	}
	records := logger.all()
	if len(records) != 1 {
		t.Fatalf("got %d log records, want 1", len(records))
	}
	rec := records[0]
	if rec.Success {
		t.Error("logged success=true for missing file")
	}
	if rec.ErrMsg == "" {
		t.Error("logged empty error message for missing file")
	}
	if rec.Filename != "missing.img" {
		t.Errorf("logged filename %q, want %q", rec.Filename, "missing.img")
	}
}

func TestHandleReadTransferFailureLogged(t *testing.T) {
	source := &fakeSource{files: map[string][]byte{}}
	logger := &fakeLogger{}
	srv := newTestServer(source, logger)
	rf := newFakeRF()
	rf.readErr = errors.New("client went away")

	err := srv.handleRead(netboot.IPXEBinBIOS, rf)
	if err == nil {
		t.Fatal("handleRead with failing transfer = nil, want error")
	}
	records := logger.all()
	if len(records) != 1 {
		t.Fatalf("got %d log records, want 1", len(records))
	}
	rec := records[0]
	if rec.Success {
		t.Error("logged success=true for failed transfer")
	}
	if !strings.Contains(rec.ErrMsg, "client went away") {
		t.Errorf("error message %q does not mention transfer failure", rec.ErrMsg)
	}
}
