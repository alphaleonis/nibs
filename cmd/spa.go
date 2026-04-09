package cmd

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaHandler serves static files from an fs.FS, falling back to index.html
// for paths that don't match a static file (SPA client-side routing).
//
// Paths that look like static asset requests (have a file extension) return
// 404 if the file doesn't exist, rather than falling back to index.html.
func spaHandler(staticFS fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(staticFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clean and normalize the path (defense-in-depth against directory traversal)
		urlPath := path.Clean(r.URL.Path)
		if urlPath == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Strip leading slash for fs.FS lookups
		fsPath := urlPath[1:]

		_, err := fs.Stat(staticFS, fsPath)
		if err != nil {
			// File not found — check if this looks like a static asset
			if isStaticAssetPath(urlPath) {
				http.NotFound(w, r)
				return
			}
			// SPA fallback: serve index.html for client-side routing
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}

		r.URL.Path = urlPath
		fileServer.ServeHTTP(w, r)
	})
}

// isStaticAssetPath returns true if the path looks like a request for a
// static asset file (has a file extension like .js, .css, .woff2, etc.)
// rather than a client-side route.
func isStaticAssetPath(urlPath string) bool {
	ext := path.Ext(urlPath)
	if ext == "" {
		return false
	}
	// Paths with file extensions under known asset directories are static assets
	if strings.HasPrefix(urlPath, "/assets/") {
		return true
	}
	// Any path with a common static file extension is treated as a static asset.
	// .html is intentionally excluded -- HTML paths are treated as SPA routes.
	switch ext {
	case ".js", ".css", ".map", ".woff", ".woff2", ".ttf", ".eot",
		".svg", ".png", ".jpg", ".jpeg", ".gif", ".ico", ".webp", ".avif":
		return true
	}
	return false
}
