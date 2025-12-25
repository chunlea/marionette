package admin

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// staticHandler serves the embedded frontend assets.
// Falls back to index.html for SPA routing.
func staticHandler() http.Handler {
	// Get the dist subdirectory
	subFS, err := fs.Sub(distFS, "dist")
	if err != nil {
		// If dist doesn't exist, return a placeholder
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Marionette Admin</title></head>
<body>
<h1>Marionette Admin</h1>
<p>Frontend not built. Run <code>make web-build</code> to build the frontend.</p>
</body>
</html>`))
		})
	}

	fileServer := http.FileServer(http.FS(subFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Try to serve the file directly
		// Check if file exists by trying to open it
		f, err := subFS.Open(strings.TrimPrefix(path, "/"))
		if err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// File doesn't exist, serve index.html for SPA routing
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
