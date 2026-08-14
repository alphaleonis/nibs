package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/spf13/cobra"
	"github.com/vektah/gqlparser/v2/gqlerror"
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
	// Host and port come from --host/--port (or config); no positional args.
	Args: codedNoArgs(nil),
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

// startServer starts the HTTP server. It blocks until ctx is canceled, then
// gracefully shuts down, draining in-flight requests. The opener function is
// called with the URL after the listener is bound, allowing tests to inject a fake.
func startServer(ctx context.Context, app *App, host string, port int, open bool, opener func(string) error) error {
	// Start filesystem watching so external edits (CLI, text editor, another
	// process) are reflected in the web UI without requiring a server restart.
	if err := app.Core.StartWatching(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to start filesystem watcher: %v\n", err)
	}

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

	// Shut down gracefully when ctx is canceled.
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
	mux.Handle("/graphql", newGraphQLHandler(app, wsPingPongInterval))
	if staticFS != nil {
		mux.Handle("/", spaHandler(staticFS))
	}
	// Compute the CSP once at construction time: buildCSP reads the embedded
	// index.html to pin the FOUC-guard inline script's hash (see
	// securityHeadersMiddleware). CORS is left outermost so its headers (and
	// the 204 preflight short-circuit) wrap the security-header layer, which in
	// turn applies to every proxied response.
	csp := buildCSP(staticFS)
	return corsMiddleware(securityHeadersMiddleware(mux, csp))
}

// inlineScriptRe matches every <script ...>...</script> block in an HTML
// document. Submatch 1 is the opening tag's raw attribute string (empty for a
// bare <script>), submatch 2 is the exact body between the opening tag's ">"
// and the "</script>" — including any leading/trailing whitespace and newlines,
// which are part of the bytes a CSP hash is computed over. The (?s) flag lets
// "." span newlines; ".*?" is non-greedy so adjacent scripts don't merge.
var inlineScriptRe = regexp.MustCompile(`(?s)<script([^>]*)>(.*?)</script>`)

// scriptSrcAttrRe matches a real `src` attribute in a <script> tag's attribute
// string: an `src` token on a start/whitespace boundary followed by `=`. Using
// an attribute-name boundary rather than a bare substring avoids misclassifying
// an inline script whose tag carries an unrelated src-containing attribute
// (e.g. data-source, id="x-src") as external — which would drop its hash and let
// the strict script-src silently block it.
var scriptSrcAttrRe = regexp.MustCompile(`(?i)(?:^|\s)src\s*=`)

// extractInlineScriptHashes reads index.html from fsys and returns a CSP
// source hash ("sha256-<standard-base64>") for every *inline* <script> — one
// whose opening tag carries no src attribute. The hash is computed over the
// raw body bytes exactly as served, so it can never drift from the shipped
// markup. Returns nil (no error) when fsys is nil or the file cannot be read,
// so the CSP degrades gracefully to omitting the hashes.
func extractInlineScriptHashes(fsys fs.FS) []string {
	if fsys == nil {
		return nil
	}
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		// A non-nil FS whose index.html is unreadable is an unexpected build/embed
		// regression, not the expected nil-FS (API-only) path — warn so the
		// silently stricter CSP (FOUC hash dropped) is diagnosable, matching the
		// file's degraded-but-non-fatal Warning convention (StartWatching, openBrowser).
		fmt.Fprintf(os.Stderr, "Warning: failed to read index.html for CSP script hash: %v\n", err)
		return nil
	}
	var hashes []string
	for _, m := range inlineScriptRe.FindAllStringSubmatch(string(data), -1) {
		attrs, body := m[1], m[2]
		if scriptSrcAttrRe.MatchString(attrs) {
			continue // external script — its URL is covered by script-src 'self', not a hash
		}
		sum := sha256.Sum256([]byte(body))
		hashes = append(hashes, "sha256-"+base64.StdEncoding.EncodeToString(sum[:]))
	}
	return hashes
}

// buildCSP assembles the Content-Security-Policy served by
// securityHeadersMiddleware. script-src is 'self' plus a quoted sha256 hash for
// each inline script found in fsys's index.html; when there are no hashes (nil
// FS, e.g. API-only test muxes) script-src is just 'self'.
//
// Two directives are deliberately weaker or narrower than script-src, and the
// choices are load-bearing:
//   - style-src keeps 'unsafe-inline' because Svelte/bits-ui components emit
//     per-render dynamic inline style= attributes (interpolated colors, computed
//     widths, popover coordinates) that cannot be sha256-pinned like the single
//     static FOUC script. Inline styles are an accepted lower risk than inline
//     scripts — do NOT drop 'unsafe-inline' here without re-verifying dynamic
//     component styling.
//   - img-src is 'self' data: https:: external images linked from user-authored
//     nib markdown (rendered by web/src/lib/markdown.ts) are allowed over https so
//     markdown image links render. This accepts that rendered markdown may issue
//     outbound https image requests (e.g. tracking pixels); plaintext http image
//     origins stay blocked. Narrow to 'self' data: only if a no-outbound-requests
//     posture is later preferred over external-image support.
//
// Unlike the script hash, these origin allowlists do not self-maintain across
// frontend rebuilds — a newly introduced external asset origin must be added
// here consciously.
func buildCSP(fsys fs.FS) string {
	scriptSrc := "'self'"
	for _, h := range extractInlineScriptHashes(fsys) {
		scriptSrc += " '" + h + "'"
	}
	return strings.Join([]string{
		"default-src 'self'",
		"script-src " + scriptSrc,
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: https:",
		"font-src 'self' data:",
		"connect-src 'self'",
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

// securityHeadersMiddleware sets baseline HTTP security headers on every
// response before delegating to next, so they are present even if next writes
// the status line itself:
//
//	Content-Security-Policy: <csp>   — restricts resource origins (see below)
//	X-Content-Type-Options: nosniff  — disables MIME sniffing
//	X-Frame-Options: DENY            — blocks framing (clickjacking defense)
//	Referrer-Policy: no-referrer     — never leak the URL as a referrer
//
// Why the CSP's script-src carries a *computed* hash: web/index.html ships one
// inline <script>, the FOUC guard, which runs before any module
// loads and therefore has no src for a URL allowlist to cover. A strict CSP
// would block it, so buildCSP allowlists its exact sha256. That hash is derived
// at mux-construction time from the *embedded, built* index.html, so it
// self-maintains: rebuilding the frontend regenerates the served bytes and the
// hash together, with nothing to hardcode or keep in sync.
func securityHeadersMiddleware(next http.Handler, csp string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
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

// wsPingPongInterval is how often the WebSocket transport pings a live-updates
// client and, transitively, how fast a vanished one is reaped: gqlgen arms a
// read deadline of twice this value, so a client that stops answering is closed
// within 2x–3x the interval instead of lingering until the OS TCP timeout. It
// mirrors the client's own 10s keep-alive (web/src/lib/graphql.ts), giving both
// ends the same detection bound.
const wsPingPongInterval = 10 * time.Second

// newGraphQLHandler creates a gqlgen HTTP handler with GET, POST, and WebSocket transports.
// Every operation it executes gets its own graph.RequestCache in context via
// requestCacheAroundOperations — resolver helpers (cachedMentions /
// cachedMentionedBy / cachedSearchAllIDs) use it to memoize reader lookups that
// one operation would otherwise repeat once per parent. The in-process CLI
// executor (cmd/graphql.go) registers no middleware; it attaches an equivalent
// cache itself in newQueryContext, because one invocation issuing one operation
// still fans a relationship field out across every parent it selects.
func newGraphQLHandler(app *App, wsPingPong time.Duration) http.Handler {
	es := graph.NewExecutableSchema(graph.Config{
		Resolvers: app.newResolver(),
	})

	srv := handler.New(es)
	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})
	// PingPongInterval, not KeepAlivePingInterval (nibs-y59n): the latter
	// applies only to the legacy graphql-ws subprotocol, which no nibs client
	// speaks — the web client is graphql-ws v6, i.e. graphql-transport-ws, so
	// with it the server sent nothing and a vanished client was never reaped
	// (measured: zero server-initiated frames in 60s). PingPongInterval is the
	// graphql-transport-ws equivalent, and the default websocket adapter
	// enforces the missed-pong read deadline that actually closes dead
	// connections. The legacy setting is dropped rather than kept alongside:
	// a setting that no real connection reads is a trap for the next reader.
	srv.AddTransport(transport.Websocket{
		PingPongInterval: wsPingPong,
	})
	srv.SetErrorPresenter(etagErrorPresenter)
	srv.AroundOperations(requestCacheAroundOperations)

	return srv
}

// etagErrorPresenter is the gqlgen error presenter that attaches a stable,
// machine-readable extensions.code to the failures the web client must route
// structurally rather than by prose:
//
//   - "ETAG_MISMATCH" on ONLY the typed *nibcore.ETagMismatchError — the
//     reconcilable optimistic-concurrency conflict the web client routes into
//     its inline "Load theirs / Overwrite" resolver. errors.As walks the wrap
//     chain, so a wrapped ETagMismatchError is still recognized.
//   - "NOT_FOUND" on any error carrying nib.ErrNotFound (via errors.Is, so a
//     wrapped "target nib not found: <id>: nib not found" still matches). A
//     delete of the edited nib surfaces here — GetForUpdate returns ErrNotFound
//     BEFORE any if-match check, so it is not an etag conflict — and the web
//     client routes it to its gone/deleted notice instead of a raw toast. The tag
//     keys only on the error type, not on WHICH nib is gone: a concurrently-
//     deleted BLOCKING target (a different nib than the one being edited) also
//     carries ErrNotFound, because updateTargetClone wraps it with %w
//     ("target nib not found: <id>: %w"), so it would be tagged NOT_FOUND too. (A
//     missing PARENT does NOT: validateAndSetParent formats its id with %s and
//     does not carry ErrNotFound, so it never tags.) That breadth is inert on
//     today's web save path because its edit-save input sends no parent/blocking
//     fields (only title/status/type/priority/estimate/body/tags), so the only
//     ErrNotFound it can raise is the edited nib's own deletion, and an archived
//     nib stays in the store so its Update lands as a normal write. If blocking
//     fields are later added to that input, a deleted BLOCKING target would also
//     mint NOT_FOUND
//     and misroute the still-alive edited nib to gone/deleted.
//     NOT_FOUND also arrives on the READ path, which no other code does: a
//     filter field naming one nib refuses an id no nib answers to
//     (graph.FilterTargetNotFoundError), and the web filter box sends
//     user-typed — so routinely half-typed — ids. The list keys on the code to
//     explain that inline instead of rendering it as a failure.
//     An UNRESOLVABLE-ID filter refusal is the ONLY read-path source of this
//     code, and web/src/lib/components/TreeTable.svelte routes ANY read-path
//     NOT_FOUND to that calm inline empty state on the strength of it. Not every
//     filter refusal qualifies: an id-valued field given the EMPTY STRING is
//     refused too (graph.FilterTargetEmptyError) and deliberately carries no
//     code, below. What holds the invariant up is that every read resolver
//     returns (nil, nil) on a miss (queryResolver.Nib, nibResolver.Parent) and
//     only mutations wrap nib.ErrNotFound (snapshotResult). A new read resolver
//     that carried it — nib(id:) erroring instead of resolving to null, say —
//     would be presented as an empty result rather than as the failure it is.
//   - "FILTER_CONTRADICTION" on the typed *graph.FilterTargetContradictionError —
//     an id-valued filter field combined with its presence twin set to false
//     (parentId + hasParent:false, blockedById + hasBlockedBy:false). It is a
//     read-path refusal like the two above and needs a code of its own because
//     both alternatives misinform. NOT_FOUND would route it to the "no such nib"
//     empty state, whose wording blames an id when both halves of the pair may
//     name real nibs — the type's own doc comment refuses that collapse for the
//     same reason on the CLI side. Leaving it uncoded lands it in the web list's
//     destructive error box, which is where the web client's query box can reach
//     it in two clicks: it registers `parent:<id>` and `no:parent` as independent
//     tokens, and "Children of this" on the row context menu ANDs a parentId onto
//     whatever is already set, so a `no:parent` view reaches the pair without
//     anyone typing it. Its own code lets TreeTable.svelte name the two tokens in
//     the box's own vocabulary and, for the hierarchy pair, keep the "Clear
//     hierarchy filters" button the empty state offered before the refusal
//     existed.
//
// Every other error (the enum-validation errors, ETagRequiredError,
// OnDiskUnparseableError, a filter target that vanished mid-query, a filter
// field given an empty id, generic failures) is left EXACTLY as the default
// presenter formats it: no code is added, so callers can't mistake a
// non-reconcilable failure for a retryable conflict or a real deletion.
//
// The empty-id refusal is the case where that default is a decision rather than
// an omission. It is a malformed query — an empty id is what a client sends when
// a variable did not interpolate — so it must surface as a failure. Tagging it
// NOT_FOUND would route it to the inline "nothing matched" state and hide a
// client bug behind exactly the confident empty answer the refusal exists to
// replace; the CLI keys on the Go error chain instead and gives it exit 2 (see
// filterTargetErrCode).
//
// The web classifiers key on these codes first; the "etag mismatch" substring
// match is kept only as a fallback (see web/src/lib/nibForm.svelte.ts,
// isEtagConflict). The message text is preserved verbatim here so that fallback
// stays intact.
func etagErrorPresenter(ctx context.Context, err error) *gqlerror.Error {
	gqlErr := graphql.DefaultErrorPresenter(ctx, err)

	var etagErr *nibcore.ETagMismatchError
	var contradiction *graph.FilterTargetContradictionError
	switch {
	case errors.As(err, &etagErr):
		setErrorCode(gqlErr, wireCodeETagMismatch)
	case errors.As(err, &contradiction):
		setErrorCode(gqlErr, wireCodeFilterContradiction)
	case errors.Is(err, nib.ErrNotFound):
		setErrorCode(gqlErr, wireCodeNotFound)
	}

	return gqlErr
}

// The extensions.code values the presenter above can mint. They are a vocabulary
// of their own, deliberately NOT internal/output's Err* block: those are CLI
// codes the process maps to an exit status (an etag conflict is CONFLICT there,
// exit 4), while these are the wire codes a GraphQL client routes on. NOT_FOUND
// is spelled the same in both by coincidence of meaning, not because one set
// derives from the other, and merging them would claim the CLI can report
// ETAG_MISMATCH and the wire can report FILE_ERROR.
//
// Each of these is a shipped contract: it must be NAMED in
// internal/graph/schema.graphqls wherever the refusal that carries it is
// described, because those descriptions are what a client is given — the SDL
// itself, the doc comments codegen copies into model/models_gen.go and
// web/src/lib/gql/graphql.ts, and `nibs catalog schema`, which an agent reads.
// A code the presenter mints but the SDL never names is one no client can
// discover.
//
// Only the weaker half of that is mechanized: adding a code here that the SDL
// never spells AT ALL fails TestEveryMintableWireErrorCodeIsNamedInTheSchema.
// That a code is named at every site whose refusal really carries it — and at
// no site whose refusal does not — remains a review obligation; no test can
// see it.
const (
	wireCodeETagMismatch        = "ETAG_MISMATCH"
	wireCodeFilterContradiction = "FILTER_CONTRADICTION"
	wireCodeNotFound            = "NOT_FOUND"
)

// setErrorCode attaches a stable extensions.code to a presented GraphQL error,
// allocating the extensions map on first use.
func setErrorCode(gqlErr *gqlerror.Error, code string) {
	if gqlErr.Extensions == nil {
		gqlErr.Extensions = map[string]any{}
	}
	gqlErr.Extensions["code"] = code
}

// requestCacheAroundOperations installs a fresh graph.RequestCache on each
// GraphQL OPERATION's context. Downstream resolvers read it via
// graph.RequestCacheFrom to coalesce duplicate mention and search lookups
// within the operation.
//
// A new cache per operation is essential — sharing one across operations serves
// stale data after a mutation, since every entry is derived from live store
// state. That is also why this is an operation middleware and not an
// http.Handler wrapper: gqlgen's WebSocket transport stores the upgrade
// request's context once and derives every later operation from it, so an
// http.Handler wrapper would install exactly one cache for the whole connection
// and hold a search answer computed at connect time for as long as the socket
// lives. AroundOperations runs once per POST, once per GET and once per
// WebSocket message alike, so all three transports get the same scope.
func requestCacheAroundOperations(ctx context.Context, next graphql.OperationHandler) graphql.ResponseHandler {
	return next(graph.WithRequestCache(ctx, graph.NewRequestCache()))
}
