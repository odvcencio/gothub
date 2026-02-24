package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist
var distFS embed.FS

// Handler returns an http.Handler that serves the embedded frontend.
// API and protocol routes are handled by other handlers; this serves
// the SPA shell for all other paths.
func Handler() http.Handler {
	sub, _ := fs.Sub(distFS, "dist")
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve static assets directly
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Try to open the file — if it exists, serve it
		if f, err := sub.Open(path); err == nil {
			f.Close()

			// Set proper MIME for WASM
			if strings.HasSuffix(path, ".wasm") {
				w.Header().Set("Content-Type", "application/wasm")
			}

			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for all non-file routes
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
