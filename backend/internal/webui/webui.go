// Package webui embeds the built Vue frontend (frontend/dist, staged into
// this package's dist/ directory by `make webui`) and serves it as an SPA
// from the admin HTTP server, so production deployments ship a single binary.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// reservedPrefixes belong to the API / operations endpoints. Requests below
// them never fall back to index.html — an unmatched path there is a real 404.
var reservedPrefixes = []string{"/api/", "/metrics", "/healthz"}

// Dist returns the embedded frontend build rooted at its dist directory.
func Dist() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}

// Handler serves the embedded frontend. It is safe to mount even when the
// binary was built without `make webui` — it then answers with an explanation
// instead of the UI.
func Handler() (http.Handler, error) {
	dist, err := Dist()
	if err != nil {
		return nil, err
	}
	return NewHandler(dist), nil
}

// NewHandler serves an SPA from the given filesystem: exact files are served
// with long-lived caching for hashed assets, and every other GET path falls
// back to index.html so client-side routing works after a page reload.
func NewHandler(dist fs.FS) http.Handler {
	fileServer := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		for _, prefix := range reservedPrefixes {
			if strings.HasPrefix(r.URL.Path, prefix) {
				http.NotFound(w, r)
				return
			}
		}

		name := strings.TrimPrefix(path(r), "/")
		if name != "" && fileExists(dist, name) {
			if strings.HasPrefix(name, "assets/") {
				// Vite emits content-hashed filenames — cache them forever.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			http.Error(w,
				"web UI is not embedded in this build — run `make webui` before building, "+
					"or use the production Dockerfile",
				http.StatusNotFound)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(index)
		}
	})
}

// path returns the cleaned request path, collapsing any traversal segments.
func path(r *http.Request) string {
	p := strings.ReplaceAll(r.URL.Path, "\\", "/")
	for strings.Contains(p, "..") {
		p = strings.ReplaceAll(p, "..", "")
	}
	return p
}

func fileExists(fsys fs.FS, name string) bool {
	info, err := fs.Stat(fsys, name)
	return err == nil && !info.IsDir()
}
