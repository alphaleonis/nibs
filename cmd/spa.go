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
			// The SPA entry (index.html) is never cached: it references the build's
			// content-hashed assets by name, so serving a stale copy would load the
			// old app. `no-store` guarantees a rebuilt UI is always picked up — which
			// matters for `nibs serve`/`task demo`, where the embedded assets carry
			// zero modtimes (no Last-Modified/ETag) and the browser would otherwise
			// heuristically cache them.
			w.Header().Set("Cache-Control", "no-store")
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
			// SPA fallback: serve index.html for client-side routing (same no-cache
			// reasoning as "/").
			w.Header().Set("Cache-Control", "no-store")
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}

		// An existing file. Content-hashed build assets under /assets/ are immutable
		// (their name changes when their bytes do), so they cache forever; everything
		// else stays uncached so a rebuild is always reflected.
		if strings.HasPrefix(urlPath, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-store")
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
