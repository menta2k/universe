// Package bootfiles auto-provisions the Ubuntu kernel/initrd boot artifacts a
// release needs, fetching them once from the official live-server ISO when they
// are missing. Only casper/vmlinuz and casper/initrd are read (via HTTP range
// requests), so the multi-GB ISO is never downloaded in full.
package bootfiles

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kdomanski/iso9660"
)

// httpReaderAt reads a remote file over HTTP with Range requests, exposing it
// as an io.ReaderAt so an ISO9660 reader can seek without downloading the whole
// image. A trailing read-ahead buffer collapses the many small sequential
// directory reads the ISO parser makes into few HTTP requests.
type httpReaderAt struct {
	ctx    context.Context
	client *http.Client
	url    string
	size   int64

	// simple single-window read-ahead cache
	buf     []byte
	bufOff  int64
	readAhd int64
}

func newHTTPReaderAt(ctx context.Context, client *http.Client, url string) (*httpReaderAt, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("head %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("head %s: status %d", url, resp.StatusCode)
	}
	if resp.Header.Get("Accept-Ranges") != "bytes" || resp.ContentLength <= 0 {
		return nil, fmt.Errorf("source does not support range requests: %s", url)
	}
	return &httpReaderAt{
		ctx: ctx, client: client, url: url, size: resp.ContentLength,
		readAhd: 1 << 20, // 1 MiB read-ahead
	}, nil
}

func (h *httpReaderAt) Size() int64 { return h.size }

func (h *httpReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= h.size {
		return 0, io.EOF
	}
	if n, ok := h.fromCache(p, off); ok {
		return n, nil
	}
	// Fill the cache starting at off with read-ahead, then serve from it.
	want := int64(len(p))
	end := min(off+max(want, h.readAhd), h.size)
	data, err := h.fetch(off, end-off)
	if err != nil {
		return 0, err
	}
	h.buf, h.bufOff = data, off
	n, _ := h.fromCache(p, off)
	if int64(n) < want && off+int64(n) < h.size {
		return n, io.ErrUnexpectedEOF
	}
	return n, nil
}

func (h *httpReaderAt) fromCache(p []byte, off int64) (int, bool) {
	if h.buf == nil || off < h.bufOff || off >= h.bufOff+int64(len(h.buf)) {
		return 0, false
	}
	n := copy(p, h.buf[off-h.bufOff:])
	return n, true
}

func (h *httpReaderAt) fetch(off, length int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(h.ctx, http.MethodGet, h.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, off+length-1))
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("range get %s: %w", h.url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("range get %s: status %d", h.url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, length))
}


// extractCasperFile opens the ISO at ra and returns a reader for the file at
// casper/<name> (e.g. "vmlinuz" or "initrd").
func extractCasperFile(ra io.ReaderAt, name string) (io.Reader, error) {
	img, err := iso9660.OpenImage(ra)
	if err != nil {
		return nil, fmt.Errorf("open iso: %w", err)
	}
	root, err := img.RootDir()
	if err != nil {
		return nil, fmt.Errorf("iso root: %w", err)
	}
	casper, err := childDir(root, "casper")
	if err != nil {
		return nil, err
	}
	children, err := casper.GetChildren()
	if err != nil {
		return nil, fmt.Errorf("read casper dir: %w", err)
	}
	for _, c := range children {
		if !c.IsDir() && strings.EqualFold(strings.TrimSuffix(c.Name(), "."), name) {
			return c.Reader(), nil
		}
	}
	return nil, fmt.Errorf("casper/%s not found in iso", name)
}

func childDir(dir *iso9660.File, name string) (*iso9660.File, error) {
	children, err := dir.GetChildren()
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}
	for _, c := range children {
		if c.IsDir() && strings.EqualFold(strings.TrimSuffix(c.Name(), "."), name) {
			return c, nil
		}
	}
	return nil, fmt.Errorf("directory %q not found in iso", name)
}
