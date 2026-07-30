package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestIsStaticAssetPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"JS file", "/app.js", true},
		{"CSS file", "/style.css", true},
		{"Assets dir", "/assets/font.woff2", true},
		{"No extension", "/some/route", false},
		{"HTML file", "/page.html", false}, // document current behavior
		{"Dot in route", "/api/v1.0/data", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isStaticAssetPath(tt.path); got != tt.want {
				t.Errorf("isStaticAssetPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func testMapFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!doctype html><html><body><div id="app"></div></body></html>`),
		},
		"assets/main.js": &fstest.MapFile{
			Data: []byte(`console.log("nibs");`),
		},
		"assets/style.css": &fstest.MapFile{
			Data: []byte(`body { margin: 0; }`),
		},
	}
}

func TestSPAHandler(t *testing.T) {
	t.Run("unknown path returns index.html", func(t *testing.T) {
		h := spaHandler(testMapFS())

		req := httptest.NewRequest(http.MethodGet, "/some/unknown/path", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `<div id="app">`) {
			t.Errorf("expected index.html content, got %q", body)
		}
	})

	t.Run("root path serves index.html", func(t *testing.T) {
		h := spaHandler(testMapFS())

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		ct := rec.Header().Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Errorf("expected Content-Type containing text/html, got %q", ct)
		}

		body := rec.Body.String()
		if !strings.Contains(body, `<div id="app">`) {
			t.Errorf("expected index.html content, got %q", body)
		}
	})

	t.Run("missing asset paths return 404, not index.html", func(t *testing.T) {
		h := spaHandler(testMapFS())

		tests := []struct {
			name string
			path string
		}{
			{name: "missing JS", path: "/assets/missing.js"},
			{name: "missing CSS", path: "/assets/missing.css"},
			{name: "missing nested asset", path: "/assets/fonts/missing.woff2"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, tt.path, nil)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)

				if rec.Code != http.StatusNotFound {
					t.Errorf("expected status 404, got %d", rec.Code)
				}
			})
		}
	})

	t.Run("static files served with correct Content-Type", func(t *testing.T) {
		h := spaHandler(testMapFS())

		tests := []struct {
			name        string
			path        string
			wantType    string
			wantContent string
		}{
			{
				name:        "JavaScript",
				path:        "/assets/main.js",
				wantType:    "javascript", // matches both text/javascript and application/javascript
				wantContent: `console.log("nibs");`,
			},
			{
				name:        "CSS",
				path:        "/assets/style.css",
				wantType:    "text/css",
				wantContent: `body { margin: 0; }`,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, tt.path, nil)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)

				if rec.Code != http.StatusOK {
					t.Fatalf("expected status 200, got %d", rec.Code)
				}

				ct := rec.Header().Get("Content-Type")
				if !strings.Contains(ct, tt.wantType) {
					t.Errorf("expected Content-Type containing %q, got %q", tt.wantType, ct)
				}

				body := rec.Body.String()
				if !strings.Contains(body, tt.wantContent) {
					t.Errorf("expected body containing %q, got %q", tt.wantContent, body)
				}
			})
		}
	})

	t.Run("cache-control: index/fallback no-store, hashed assets immutable", func(t *testing.T) {
		h := spaHandler(testMapFS())
		tests := []struct {
			name string
			path string
			want string
		}{
			// The SPA entry must never be cached, or a rebuilt UI is masked by a
			// stale index.html pointing at old asset hashes (the nibs serve /
			// task demo staleness class).
			{name: "root index", path: "/", want: "no-store"},
			{name: "SPA fallback", path: "/some/client/route", want: "no-store"},
			// Content-hashed build assets are immutable — safe to cache forever.
			{name: "hashed JS asset", path: "/assets/main.js", want: "public, max-age=31536000, immutable"},
			{name: "hashed CSS asset", path: "/assets/style.css", want: "public, max-age=31536000, immutable"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, tt.path, nil)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if got := rec.Header().Get("Cache-Control"); got != tt.want {
					t.Errorf("Cache-Control for %s = %q, want %q", tt.path, got, tt.want)
				}
			})
		}
	})
}

func TestServeMuxWithSPA(t *testing.T) {
	t.Run("API routes take precedence over SPA", func(t *testing.T) {
		app := setupServeTestApp(t)
		h := newServeMux(app, testMapFS())

		t.Run("/health returns JSON, not index.html", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", rec.Code)
			}

			var body struct {
				Status string `json:"status"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode JSON: %v", err)
			}
			if body.Status != "ok" {
				t.Errorf("expected status 'ok', got %q", body.Status)
			}
		})

		t.Run("/graphql POST still works", func(t *testing.T) {
			query := `{"query":"{ nibs { id } }"}`
			req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(query))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", rec.Code)
			}

			ct := rec.Header().Get("Content-Type")
			if !strings.Contains(ct, "application/json") {
				t.Errorf("expected JSON content type, got %q", ct)
			}
		})
	})

	t.Run("nil staticFS disables SPA serving", func(t *testing.T) {
		app := setupServeTestApp(t)
		h := newServeMux(app, nil)

		t.Run("non-API paths return 404", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/some/path", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("expected status 404, got %d", rec.Code)
			}
		})

		t.Run("root path returns 404", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("expected status 404, got %d", rec.Code)
			}
		})

		t.Run("API routes still work", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", rec.Code)
			}

			var body struct {
				Status string `json:"status"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode JSON: %v", err)
			}
			if body.Status != "ok" {
				t.Errorf("expected status 'ok', got %q", body.Status)
			}
		})
	})
}
