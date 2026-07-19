package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testDist() fstest.MapFS {
	return fstest.MapFS{
		"index.html":         {Data: []byte("<html>app</html>")},
		"favicon.svg":        {Data: []byte("<svg/>")},
		"assets/app-abc.js":  {Data: []byte("console.log(1)")},
		"assets/app-abc.css": {Data: []byte("body{}")},
	}
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestServesIndexAtRoot(t *testing.T) {
	rec := get(t, NewHandler(testDist()), "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "app") {
		t.Fatalf("body = %q, want index content", rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cc)
	}
}

func TestServesStaticAssetWithImmutableCache(t *testing.T) {
	rec := get(t, NewHandler(testDist()), "/assets/app-abc.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("Cache-Control = %q, want immutable", cc)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("Content-Type = %q, want javascript", ct)
	}
}

func TestFallsBackToIndexForSPARoutes(t *testing.T) {
	for _, path := range []string{"/machines", "/profiles/123", "/deep/nested/route"} {
		rec := get(t, NewHandler(testDist()), path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "app") {
			t.Fatalf("%s: body = %q, want index content", path, rec.Body.String())
		}
	}
}

func TestUnknownAPIAndOpsPathsAre404(t *testing.T) {
	for _, path := range []string{"/api/v1/nope", "/metrics/x", "/healthz/x"} {
		rec := get(t, NewHandler(testDist()), path)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", path, rec.Code)
		}
	}
}

func TestRejectsNonGET(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler(testDist()).ServeHTTP(rec,
		httptest.NewRequest(http.MethodPost, "/machines", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestHeadRequestServed(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler(testDist()).ServeHTTP(rec,
		httptest.NewRequest(http.MethodHead, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestPathTraversalDoesNotEscapeDist(t *testing.T) {
	rec := get(t, NewHandler(testDist()), "/../../etc/passwd")
	if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "root:") {
		t.Fatal("path traversal escaped the embedded filesystem")
	}
}

func TestPlaceholderWhenUINotEmbedded(t *testing.T) {
	rec := get(t, NewHandler(fstest.MapFS{".gitkeep": {Data: nil}}), "/")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not embedded") {
		t.Fatalf("body = %q, want explanation", rec.Body.String())
	}
}

func TestEmbeddedDistAccessible(t *testing.T) {
	// The real embedded FS must at least be openable; content depends on
	// whether `make webui` ran before the build.
	if _, err := Dist(); err != nil {
		t.Fatalf("Dist() error: %v", err)
	}
}
