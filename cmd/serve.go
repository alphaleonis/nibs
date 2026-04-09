package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/spf13/cobra"
)

var (
	serveHost   string
	servePort   int
	serveOpen   bool
	serveNoOpen bool
)

var serveCmd = &cobra.Command{
	Use:     "web",
	Aliases: []string{"serve"},
	Short:   "Start the web UI server",
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		defer func() { _ = app.Core.Close() }()

		flagPortSet := cmd.Flags().Changed("port")
		flagOpenSet := cmd.Flags().Changed("open") || cmd.Flags().Changed("no-open")
		flagOpenValue := serveOpen && !serveNoOpen

		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		opts := resolveServeOptions(app.Config(), servePort, flagPortSet, flagOpenSet, flagOpenValue)
		host := serveHost
		if host == "" {
			host = "127.0.0.1"
		}
		return startServer(ctx, app, host, opts.port, opts.open, openBrowser)
	},
}

func init() {
	serveCmd.Flags().StringVar(&serveHost, "host", "", "Host address to bind to (default: 127.0.0.1)")
	serveCmd.Flags().IntVar(&servePort, "port", 0, "Port to listen on (default: from config or 3000)")
	serveCmd.Flags().BoolVar(&serveOpen, "open", false, "Open browser on startup")
	serveCmd.Flags().BoolVar(&serveNoOpen, "no-open", false, "Do not open browser on startup")
	serveCmd.MarkFlagsMutuallyExclusive("open", "no-open")
	rootCmd.AddCommand(serveCmd)
}

// serveOptions holds the resolved serve command options.
type serveOptions struct {
	port int
	open bool
}

// resolveServeOptions resolves serve options from flags and config.
// flagPort is the --port flag value (0 means not set).
// flagPortSet is true if --port was explicitly provided.
// flagOpenSet is true if --open or --no-open was explicitly provided.
// flagOpenValue is the resolved open value from flags (only used when flagOpenSet is true).
func resolveServeOptions(cfg *config.Config, flagPort int, flagPortSet bool, flagOpenSet bool, flagOpenValue bool) serveOptions {
	port := cfg.ServerPort()
	if flagPortSet {
		port = flagPort
	}

	open := cfg.ServerOpenBrowser()
	if flagOpenSet {
		open = flagOpenValue
	}

	return serveOptions{port: port, open: open}
}

// shutdownTimeout is how long to wait for in-flight requests to complete.
const shutdownTimeout = 5 * time.Second

// startServer starts the HTTP server. It blocks until ctx is cancelled, then
// gracefully shuts down, draining in-flight requests. The opener function is
// called with the URL after the listener is bound, allowing tests to inject a fake.
func startServer(ctx context.Context, app *App, host string, port int, open bool, opener func(string) error) error {
	mux := newServeMux(app, WebDistFS)
	addr := fmt.Sprintf("%s:%d", host, port)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	boundAddr := ln.Addr().String()
	fmt.Printf("Listening on http://%s\n", boundAddr)

	if open && opener != nil {
		openURL := fmt.Sprintf("http://%s", boundAddr)
		if err := opener(openURL); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to open browser: %v\n", err)
		}
	}

	// Shut down gracefully when ctx is cancelled.
	errCh := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		errCh <- srv.Shutdown(shutdownCtx)
	}()

	if err := srv.Serve(ln); err != http.ErrServerClosed {
		return err
	}

	return <-errCh
}

// openBrowser opens the given URL in the default browser.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("cmd", "/c", "start", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// newServeMux builds the HTTP handler for the serve command.
// When staticFS is non-nil, unmatched routes serve the SPA frontend.
// When staticFS is nil, only API routes are registered.
func newServeMux(app *App, staticFS fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.Handle("/graphql", newGraphQLHandler(app))
	if staticFS != nil {
		mux.Handle("/", spaHandler(staticFS))
	}
	return corsMiddleware(mux)
}

// isAllowedOrigin returns true if the origin is a localhost address.
func isAllowedOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return u.Scheme == "http" && (host == "localhost" || host == "127.0.0.1")
}

// corsMiddleware adds CORS headers to all responses and handles preflight requests.
// Only localhost origins are allowed; the matching origin is echoed back.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vary", "Origin")
		origin := r.Header.Get("Origin")
		if isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// newGraphQLHandler creates a gqlgen HTTP handler with GET, POST, and WebSocket transports.
func newGraphQLHandler(app *App) http.Handler {
	es := graph.NewExecutableSchema(graph.Config{
		Resolvers: app.newResolver(),
	})

	srv := handler.New(es)
	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})
	srv.AddTransport(transport.Websocket{
		KeepAlivePingInterval: 10 * time.Second,
	})

	return srv
}
