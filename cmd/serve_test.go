package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// TestRequestCacheMiddleware_FreshCachePerRequest pins Behavior 15: each
// HTTP request receives its own graph.RequestCache in context. Two requests
// must see two *distinct* caches. The middleware is transport-agnostic, so
// the test wraps it around a tiny handler that captures the cache pointer
// from r.Context() — no gqlgen plumbing involved.
func TestRequestCacheMiddleware_FreshCachePerRequest(t *testing.T) {
	var caches []*graph.RequestCache
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caches = append(caches, graph.RequestCacheFrom(r.Context()))
		w.WriteHeader(http.StatusOK)
	})
	h := requestCacheMiddleware(inner)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/graphql", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: unexpected status %d", i, rec.Code)
		}
	}

	if len(caches) != 2 {
		t.Fatalf("recorded %d caches, want 2", len(caches))
	}
	for i, c := range caches {
		if c == nil {
			t.Errorf("request %d: cache is nil", i)
		}
	}
	if caches[0] == caches[1] {
		t.Errorf("both requests got the same cache pointer; want per-request isolation")
	}
}

// The intra-request dedup pin (originally named
// TestRequestCacheMiddleware_DedupsWithinOneRequest) lives in the graph
// package — see TestRequestCacheMiddleware_DedupsWithinOneRequest in
// internal/graph/request_cache_test.go. It has to be there because the
// cachedMentions helper the middleware threads through is unexported;
// proving the middleware dedups end-to-end requires direct access to it.
// Here in cmd we only own the `graph.WithRequestCache` wiring, which
// TestRequestCacheMiddleware_FreshCachePerRequest above already pins.

const maxBodySize = 1 << 20 // 1 MB — mirrors gqlgen's default POST limit for test assertions

func setupServeTestApp(t *testing.T) *App {
	t.Helper()
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}

	cfg := config.Default()
	testCore := nibcore.New(nibsDir, cfg)
	if err := testCore.Load(); err != nil {
		t.Fatalf("failed to load core: %v", err)
	}

	t.Cleanup(func() { _ = testCore.Close() })
	return &App{Core: testCore}
}

func TestHealthEndpoint(t *testing.T) {
	app := setupServeTestApp(t)
	handler := newServeMux(app, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", body.Status)
	}
}

func TestGraphQLEndpoint(t *testing.T) {
	app := setupServeTestApp(t)

	// Create a test nib
	b := &nib.Nib{
		ID:     "test-1",
		Slug:   "test-nib",
		Title:  "Test Nib",
		Status: "todo",
	}
	if err := app.Core.Create(b); err != nil {
		t.Fatalf("failed to create test nib: %v", err)
	}

	handler := newServeMux(app, nil)

	t.Run("POST with variables", func(t *testing.T) {
		body := `{"query":"query GetNib($id: ID!) { nib(id: $id) { id title } }","variables":{"id":"test-1"},"operationName":"GetNib"}`
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var resp struct {
			Data struct {
				Nib struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"nib"`
			} `json:"data"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.Data.Nib.ID != "test-1" {
			t.Errorf("expected id 'test-1', got %q", resp.Data.Nib.ID)
		}
		if resp.Data.Nib.Title != "Test Nib" {
			t.Errorf("expected title 'Test Nib', got %q", resp.Data.Nib.Title)
		}
	})

	t.Run("POST with valid query returns data", func(t *testing.T) {
		body := `{"query":"{ nibs { id title } }"}`
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var resp struct {
			Data struct {
				Nibs []struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"nibs"`
			} `json:"data"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(resp.Data.Nibs) != 1 {
			t.Fatalf("expected 1 nib, got %d", len(resp.Data.Nibs))
		}
		if resp.Data.Nibs[0].ID != "test-1" {
			t.Errorf("expected id 'test-1', got %q", resp.Data.Nibs[0].ID)
		}
	})

	t.Run("GET returns 200 with query param", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, `/graphql?query={nibs{id}}`, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
	})

	t.Run("CORS headers on POST response", func(t *testing.T) {
		body := `{"query":"{ nibs { id } }"}`
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:5173")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if v := rec.Header().Get("Access-Control-Allow-Origin"); v != "http://localhost:5173" {
			t.Errorf("expected Access-Control-Allow-Origin 'http://localhost:5173', got %q", v)
		}
	})

	t.Run("CORS rejects non-localhost origin", func(t *testing.T) {
		body := `{"query":"{ nibs { id } }"}`
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://evil.com")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if v := rec.Header().Get("Access-Control-Allow-Origin"); v != "" {
			t.Errorf("expected no Access-Control-Allow-Origin for non-localhost origin, got %q", v)
		}
		if v := rec.Header().Get("Access-Control-Allow-Methods"); v != "" {
			t.Errorf("expected no Access-Control-Allow-Methods for non-localhost origin, got %q", v)
		}
		if v := rec.Header().Get("Vary"); v != "Origin" {
			t.Errorf("expected Vary: Origin even for rejected origins, got %q", v)
		}
	})

	t.Run("CORS allows 127.0.0.1 origin", func(t *testing.T) {
		body := `{"query":"{ nibs { id } }"}`
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://127.0.0.1:3000")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if v := rec.Header().Get("Access-Control-Allow-Origin"); v != "http://127.0.0.1:3000" {
			t.Errorf("expected Access-Control-Allow-Origin 'http://127.0.0.1:3000', got %q", v)
		}
	})

	t.Run("CORS preflight returns 204", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/graphql", nil)
		req.Header.Set("Origin", "http://localhost:5173")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("expected status 204, got %d", rec.Code)
		}
		if v := rec.Header().Get("Access-Control-Allow-Origin"); v != "http://localhost:5173" {
			t.Errorf("expected Access-Control-Allow-Origin 'http://localhost:5173', got %q", v)
		}
		if v := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(v, "POST") {
			t.Errorf("expected Access-Control-Allow-Methods to contain 'POST', got %q", v)
		}
		if v := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(v, "Content-Type") {
			t.Errorf("expected Access-Control-Allow-Headers to contain 'Content-Type', got %q", v)
		}
	})

	t.Run("invalid query returns errors in body with 422", func(t *testing.T) {
		body := `{"query":"{ invalid { field } }"}`
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected status 422, got %d", rec.Code)
		}

		var resp struct {
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(resp.Errors) == 0 {
			t.Fatal("expected errors in response body")
		}
	})

	t.Run("oversized body returns 400", func(t *testing.T) {
		bigBody := strings.NewReader(strings.Repeat("x", maxBodySize+1))
		req := httptest.NewRequest(http.MethodPost, "/graphql", bigBody)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected status 400 for oversized body, got %d", rec.Code)
		}
	})
}

func TestGraphQLWebSocketUpgrade(t *testing.T) {
	app := setupServeTestApp(t)
	handler := newServeMux(app, nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/graphql"

	// Attempt WebSocket upgrade with graphql-transport-ws subprotocol
	dialer := &websocket.Dialer{}
	header := http.Header{}
	header.Set("Sec-WebSocket-Protocol", "graphql-transport-ws")

	conn, resp, err := dialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if resp.Header.Get("Sec-WebSocket-Protocol") != "graphql-transport-ws" {
		t.Errorf("expected subprotocol 'graphql-transport-ws', got %q", resp.Header.Get("Sec-WebSocket-Protocol"))
	}

	// Send connection_init message
	initMsg := map[string]any{"type": "connection_init"}
	if err := conn.WriteJSON(initMsg); err != nil {
		t.Fatalf("failed to send connection_init: %v", err)
	}

	// Expect connection_ack
	var ackMsg map[string]any
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&ackMsg); err != nil {
		t.Fatalf("failed to read connection_ack: %v", err)
	}
	if ackMsg["type"] != "connection_ack" {
		t.Errorf("expected 'connection_ack', got %v", ackMsg["type"])
	}
}

func TestResolveServeOptions(t *testing.T) {
	t.Run("config port used when flag not set", func(t *testing.T) {
		cfg := config.Default()
		port := 4000
		cfg.Nibs.Server.Port = &port

		opts := resolveServeOptions(cfg, 0, false, false, false)
		if opts.port != 4000 {
			t.Errorf("port = %d, want 4000 (from config)", opts.port)
		}
	})

	t.Run("flag overrides config port", func(t *testing.T) {
		cfg := config.Default()
		port := 4000
		cfg.Nibs.Server.Port = &port

		opts := resolveServeOptions(cfg, 8080, true, false, false)
		if opts.port != 8080 {
			t.Errorf("port = %d, want 8080 (from flag)", opts.port)
		}
	})

	t.Run("default port when neither flag nor config set", func(t *testing.T) {
		cfg := config.Default() // no server.port set

		opts := resolveServeOptions(cfg, 0, false, false, false)
		if opts.port != 3000 {
			t.Errorf("port = %d, want 3000 (default)", opts.port)
		}
	})

	t.Run("config open_browser used when no flag set", func(t *testing.T) {
		cfg := config.Default() // open_browser defaults to true

		opts := resolveServeOptions(cfg, 0, false, false, false)
		if !opts.open {
			t.Error("open = false, want true (from config default)")
		}
	})

	t.Run("config open_browser=false respected", func(t *testing.T) {
		cfg := config.Default()
		openBrowser := false
		cfg.Nibs.Server.OpenBrowser = &openBrowser

		opts := resolveServeOptions(cfg, 0, false, false, false)
		if opts.open {
			t.Error("open = true, want false (from config)")
		}
	})

	t.Run("--no-open overrides config open_browser=true", func(t *testing.T) {
		cfg := config.Default() // defaults to true

		// flagOpenSet=true, flagOpenValue=false (--no-open)
		opts := resolveServeOptions(cfg, 0, false, true, false)
		if opts.open {
			t.Error("open = true, want false (--no-open overrides config)")
		}
	})

	t.Run("--open overrides config open_browser=false", func(t *testing.T) {
		cfg := config.Default()
		openBrowser := false
		cfg.Nibs.Server.OpenBrowser = &openBrowser

		// flagOpenSet=true, flagOpenValue=true (--open)
		opts := resolveServeOptions(cfg, 0, false, true, true)
		if !opts.open {
			t.Error("open = false, want true (--open overrides config)")
		}
	})
}

func TestStartServerShutdown(t *testing.T) {
	app := setupServeTestApp(t)

	// Pick a free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- startServer(ctx, app, "127.0.0.1", port, false, nil)
	}()

	// Wait for server to be ready
	ready := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			_ = conn.Close()
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("server did not become ready within 2s")
	}

	// Cancel context to trigger shutdown
	cancel()

	// Server should return without error
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected nil error from shutdown, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within timeout")
	}
}

func TestStartServerRejectsAfterShutdown(t *testing.T) {
	app := setupServeTestApp(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- startServer(ctx, app, "127.0.0.1", port, false, nil)
	}()

	// Wait for server to be ready
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ready := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			_ = conn.Close()
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("server did not become ready within 2s")
	}

	// Cancel context to trigger shutdown
	cancel()

	// Wait for server to finish
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected nil error from shutdown, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within timeout")
	}

	// After shutdown, new connections should be refused
	_, err = net.DialTimeout("tcp", addr, 1*time.Second)
	if err == nil {
		t.Fatal("expected connection refused after shutdown, but connection succeeded")
	}
}

func TestServeOpenBrowser(t *testing.T) {
	app := setupServeTestApp(t)

	// Pick a free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	var openedURL string
	opener := func(url string) error {
		openedURL = url
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- startServer(ctx, app, "127.0.0.1", port, true, opener)
	}()

	// Wait for server to be ready
	ready := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			_ = conn.Close()
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("server did not become ready within 2s")
	}

	expected := fmt.Sprintf("http://127.0.0.1:%d", port)
	if openedURL != expected {
		t.Errorf("expected browser opened with %q, got %q", expected, openedURL)
	}

	// Clean shutdown — no goroutine leak
	cancel()
	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
}

// TestStartServerReflectsExternalFileEdits is a regression test for the bug
// where the web UI showed stale data after an external process (another CLI
// invocation, text editor, etc.) modified a nib file on disk. The running
// server must pick up filesystem changes without requiring a restart.
func TestStartServerReflectsExternalFileEdits(t *testing.T) {
	app := setupServeTestApp(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- startServer(ctx, app, "127.0.0.1", port, false, nil)
	}()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForHTTPReady(t, baseURL+"/health", 2*time.Second)

	// Write a new nib directly to the filesystem, bypassing the Core API.
	// This simulates an external edit — the running server's only way to
	// learn about this is through filesystem watching.
	root := app.Core.Root()
	externalFile := filepath.Join(root, "ext-1--external.md")
	content := "---\ntitle: External Nib\nstatus: todo\ntype: task\n---\n\nBody.\n"
	if err := os.WriteFile(externalFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write external nib file: %v", err)
	}

	// Poll the GraphQL endpoint until the server reports the new nib. The
	// watcher has a debounce delay (100ms) plus fsnotify propagation, so we
	// allow a generous window before declaring failure.
	deadline := time.Now().Add(3 * time.Second)
	queryBody := `{"query":"query { nib(id: \"ext-1\") { id title } }"}`
	found := false
	for time.Now().Before(deadline) {
		resp, err := http.Post(baseURL+"/graphql", "application/json", strings.NewReader(queryBody))
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var body struct {
			Data struct {
				Nib *struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"nib"`
			} `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if body.Data.Nib != nil && body.Data.Nib.ID == "ext-1" {
			found = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !found {
		t.Fatal("server did not pick up externally-created nib file — filesystem watcher is not active")
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
}

// TestSecurityHeaders_Present pins that every response carries the baseline
// security headers. Uses the API-only mux (nil static FS): the headers must be
// set unconditionally, independent of whether the SPA is served.
func TestSecurityHeaders_Present(t *testing.T) {
	app := setupServeTestApp(t)
	handler := newServeMux(app, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}

	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("Content-Security-Policy header is empty")
	}
	for _, sub := range []string{"default-src 'self'", "frame-ancestors 'none'", "form-action 'self'", "img-src 'self' data: https:", "script-src 'self'"} {
		if !strings.Contains(csp, sub) {
			t.Errorf("CSP %q does not contain %q", csp, sub)
		}
	}
}

// TestSecurityHeaders_CSPPinsFoucHash proves the served CSP allowlists the
// sha256 of the actually-embedded FOUC-guard inline script, so the strict
// policy never blocks it. It hashes index.html independently (mirroring, not
// calling, the middleware) and asserts the header pins that exact value.
func TestSecurityHeaders_CSPPinsFoucHash(t *testing.T) {
	// Prefer the production embed (WebDistFS). Under `go test ./cmd/` the embed
	// lives in package main, so WebDistFS is nil here; fall back to the on-disk
	// build produced by `task build` / `web:build` when present. Either source
	// yields the *actual* served index.html bytes newServeMux hashes.
	staticFS := WebDistFS
	if staticFS == nil {
		distDir := filepath.Join("..", "web", "dist")
		if _, err := os.Stat(filepath.Join(distDir, "index.html")); err == nil {
			staticFS = os.DirFS(distDir)
		}
	}

	expected := firstInlineScriptHash(t, staticFS)
	if expected == "" {
		t.Skip("no built index.html with an inline FOUC script available; run `task build` to enable this assertion")
	}

	app := setupServeTestApp(t)
	handler := newServeMux(app, staticFS)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	quoted := "'" + expected + "'"
	if !strings.Contains(csp, quoted) {
		t.Errorf("CSP does not pin FOUC script hash %s\nCSP: %s", quoted, csp)
	}
}

// firstInlineScriptHash independently derives the CSP sha256 source hash of the
// first inline (no-src) <script> in fsys's index.html, mirroring the middleware
// logic without calling it — so the test proves the served header matches the
// real bytes rather than trusting the code under test. Returns "" when fsys is
// nil or has no such script.
func firstInlineScriptHash(t *testing.T, fsys fs.FS) string {
	t.Helper()
	if fsys == nil {
		return ""
	}
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`(?s)<script([^>]*)>(.*?)</script>`)
	for _, m := range re.FindAllStringSubmatch(string(data), -1) {
		if strings.Contains(m[1], "src") {
			continue
		}
		sum := sha256.Sum256([]byte(m[2]))
		return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
	}
	return ""
}

// TestExtractInlineScriptHashes feeds a controlled HTML document with one
// inline and one external (src) script and asserts exactly one hash is returned,
// equal to the sha256 of the inline body, proving src scripts are excluded.
func TestExtractInlineScriptHashes(t *testing.T) {
	const inlineBody = `console.log("hi");`
	html := `<html><head>` +
		`<script>` + inlineBody + `</script>` +
		`<script type="module" src="/assets/app.js"></script>` +
		`</head></html>`
	fsys := fstest.MapFS{"index.html": {Data: []byte(html)}}

	got := extractInlineScriptHashes(fsys)
	if len(got) != 1 {
		t.Fatalf("got %d hashes, want 1: %v", len(got), got)
	}
	sum := sha256.Sum256([]byte(inlineBody))
	want := "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
	if got[0] != want {
		t.Errorf("hash = %q, want %q", got[0], want)
	}
}

// waitForHTTPReady polls the given URL until it returns 200 OK or the deadline passes.
func waitForHTTPReady(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server not ready at %s within %s", url, timeout)
}
