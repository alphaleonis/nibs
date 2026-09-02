package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/validator/rules"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// The endpoint `nibs serve` exposes is reachable by any page the user happens to
// have open (a plain GET triggers no preflight), and Nib is recursive on six
// fields, so a fixed-size document can ask for a tree whose size multiplies at
// every level. These tests pin the answer: bounds on how deep a document may
// nest those fields and on how many of them it may select altogether, a
// measurement of those bounds that is itself cheap, and a refusal — not merely
// absent CORS headers — for a request that names a disallowed origin.

// graphQLHTTPResponse is the wire shape of a GraphQL response over HTTP.
type graphQLHTTPResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// relationFixtureApp builds a DELIBERATELY tiny store — one parent with two
// children — so an alternating children/parent query that escapes the bound
// still resolves a handful of objects. The unbounded shape is N^(depth/2) with
// N=2 here, so even a twelve-level alternation is 2^6 = 64 objects: the guard
// can be probed without the multi-gigabyte blow-up the real defect produces.
func relationFixtureApp(t *testing.T) *App {
	t.Helper()
	app := setupServeTestApp(t)
	if err := app.Core.Create(&nib.Nib{ID: "root", Slug: "root", Title: "Root", Status: "todo"}); err != nil {
		t.Fatalf("create root: %v", err)
	}
	for i := range 2 {
		id := fmt.Sprintf("kid%d", i)
		if err := app.Core.Create(&nib.Nib{ID: id, Slug: id, Title: "Kid", Status: "todo", Parent: "root"}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	return app
}

// postGraphQL sends one query to the handler and decodes the response.
func postGraphQL(t *testing.T, serverURL, query string) (int, graphQLHTTPResponse) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": query})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	resp, err := http.Post(serverURL+"/graphql", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var decoded graphQLHTTPResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.StatusCode, decoded
}

// alternating builds a query whose path traverses `fields` recursive Nib fields,
// alternating children and parent below the entry field. Every child's parent is
// the nib the level started from, so each children level multiplies the resolved
// node count by the fan-out: the resolved size is C^(children levels) while the
// document grows linearly.
func alternating(fields int) string {
	// The innermost selection must be a scalar, so build outward from one.
	inner := "id"
	// Field 1 is the entry `nib`; fields 2..n alternate children, parent, ...
	for i := fields; i >= 2; i-- {
		name := "parent"
		if i%2 == 0 {
			name = "children"
		}
		inner = name + " { " + inner + " }"
	}
	return `{ nib(id: "root") { ` + inner + ` } }`
}

// The attack shape specifically: alternating children/parent, which is small in
// query text and multiplicative in resolved objects.
//
// The fixture has two children, so an unbounded run of the deepest case below
// (twelve fields, six children levels) resolves 2^6 = 64 objects — the shape is
// exercised at a size that cannot blow up the machine while the bound is off.
func TestGraphQLRefusesAlternatingRelationshipNesting(t *testing.T) {
	app := relationFixtureApp(t)
	server := httptest.NewServer(newServeMux(app, nil))
	defer server.Close()

	tests := []struct {
		name  string
		query string
	}{
		// Four recursive fields is the SMALLEST shape that closes the cycle:
		// children -> parent -> children resolves C^2 nibs. It is the first
		// value above the bound, so this pins where the bound sits.
		{"first shape that closes a cycle", alternating(4)},
		{"twelve fields deep", alternating(12)},
		{
			// Hiding the nesting behind a fragment must not evade the bound.
			"nesting behind a fragment",
			`{ nib(id: "root") { ...Deep } }
			 fragment Deep on Nib { children { parent { children { id } } } }`,
		},
		{
			// The mentions pair cycles the same way children/parent does, and
			// so does blocking/blockedBy — the bound counts the type, not a
			// list of field names.
			"mentions cycle",
			`{ nib(id: "root") { mentions { mentionedBy { mentions { id } } } } }`,
		},
		{
			"blocking cycle",
			`{ nib(id: "root") { blocking { blockedBy { blocking { id } } } } }`,
		},
		{
			// The bound is on the operation, not on the query root: a mutation's
			// payload selection re-enters the same recursion.
			"inside a mutation payload",
			`mutation { updateNib(id: "kid0", input: { title: "renamed" }) ` +
				`{ children { parent { children { id } } } } }`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, resp := postGraphQL(t, server.URL, tt.query)
			if len(resp.Errors) == 0 {
				t.Fatalf("query was executed, want a refusal; data = %s", resp.Data)
			}
			if !strings.Contains(resp.Errors[0].Message, "nests recursive") {
				t.Fatalf("refused with %q, want the recursion-bound message", resp.Errors[0].Message)
			}
		})
	}
}

// The other side of the boundary: one relationship hop past the deepest shipped
// view still resolves, so the bound has the headroom its constant claims.
func TestGraphQLServesQueriesAtTheRecursionBound(t *testing.T) {
	app := relationFixtureApp(t)
	server := httptest.NewServer(newServeMux(app, nil))
	defer server.Close()

	tests := []struct {
		name  string
		query string
	}{
		// Exactly at the bound: three recursive fields, resolving 2C nibs.
		{"children then parent", alternating(3)},
		{"two levels of descent", `{ nib(id: "root") { children { children { id } } } }`},
		{"list root with a relationship", `{ nibs { children { parent { id } } } }`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, resp := postGraphQL(t, server.URL, tt.query)
			if len(resp.Errors) > 0 {
				t.Fatalf("query at the bound was refused: %v", resp.Errors)
			}
		})
	}
}

// aliasedBranches repeats one relationship branch under `count` aliases. Every
// branch sits within the depth bound, and aliases are distinct response keys so
// the executor resolves each one separately: the document's cost grows with its
// size while its shape never trips the depth bound.
func aliasedBranches(count int) string {
	var b strings.Builder
	b.WriteString("{ nibs {")
	for i := range count {
		fmt.Fprintf(&b, " a%d: children { id }", i)
	}
	b.WriteString(" } }")
	return b.String()
}

// The depth bound is a maximum over the document's paths, never a sum, so it
// alone leaves the total unbounded — these pin the second bound that closes it.
func TestGraphQLBoundsTotalRecursiveSelections(t *testing.T) {
	app := relationFixtureApp(t)
	server := httptest.NewServer(newServeMux(app, nil))
	defer server.Close()

	// `nibs` is itself one recursive-type selection, so `count` aliases below it
	// total count+1.
	t.Run("above the total", func(t *testing.T) {
		_, resp := postGraphQL(t, server.URL, aliasedBranches(maxRecursiveSelectionTotal))
		if len(resp.Errors) == 0 {
			t.Fatalf("query was executed, want a refusal; data = %s", resp.Data)
		}
		if !strings.Contains(resp.Errors[0].Message, "selects at least") {
			t.Fatalf("refused with %q, want the total-bound message", resp.Errors[0].Message)
		}
	})

	t.Run("at the total", func(t *testing.T) {
		_, resp := postGraphQL(t, server.URL, aliasedBranches(maxRecursiveSelectionTotal-1))
		if len(resp.Errors) > 0 {
			t.Fatalf("a query at the total bound was refused: %v", resp.Errors)
		}
	})

	// A document that multiplies its own selections must be refused rather than
	// overflowing the count: 40 fragments each spreading the previous one twice,
	// over a base that selects one relationship field, select 2^40 in total.
	t.Run("multiplied behind fragments", func(t *testing.T) {
		_, resp := postGraphQL(t, server.URL, doublingFragments(64, "children { id }"))
		if len(resp.Errors) == 0 {
			t.Fatalf("query was executed, want a refusal; data = %s", resp.Data)
		}
		if !strings.Contains(resp.Errors[0].Message, "selects at least") {
			t.Fatalf("refused with %q, want the total-bound message", resp.Errors[0].Message)
		}
	})
}

// The refusal happens BEFORE any resolver runs, which is what makes it a bound
// on resolution cost rather than a filter on the response: a mutation whose
// payload selection is too deep must leave the store untouched.
func TestRecursionBoundRefusesBeforeTheMutationRuns(t *testing.T) {
	app := relationFixtureApp(t)
	server := httptest.NewServer(newServeMux(app, nil))
	defer server.Close()

	before, err := app.Core.Get("kid0")
	if err != nil {
		t.Fatalf("get kid0: %v", err)
	}

	_, resp := postGraphQL(t, server.URL,
		`mutation { updateNib(id: "kid0", input: { title: "renamed" }) `+
			`{ children { parent { children { id } } } } }`)
	if len(resp.Errors) == 0 {
		t.Fatalf("mutation was executed, want a refusal; data = %s", resp.Data)
	}

	after, err := app.Core.Get("kid0")
	if err != nil {
		t.Fatalf("get kid0 after: %v", err)
	}
	if after.Title != before.Title {
		t.Fatalf("title = %q, want %q — the mutation ran despite the refusal", after.Title, before.Title)
	}
}

// webQueryDocuments returns every GraphQL document the shipped web client sends,
// read from its own source so the bound is checked against the real query text
// rather than a copy that can drift.
func webQueryDocuments(t *testing.T) map[string]string {
	t.Helper()
	path := filepath.Join("..", "web", "src", "lib", "queries.ts")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	re := regexp.MustCompile("(?s)graphql\\(`(.*?)`\\)")
	matches := re.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatalf("no graphql(`...`) documents found in %s", path)
	}
	nameRe := regexp.MustCompile(`(?m)^\s*(query|mutation|subscription)\s+(\w+)`)
	docs := make(map[string]string, len(matches))
	for _, m := range matches {
		name := nameRe.FindStringSubmatch(m[1])
		if name == nil {
			t.Fatalf("document has no operation name:\n%s", m[1])
		}
		docs[name[2]] = m[1]
	}
	return docs
}

// The constant's basis, checked against the client's own source: no document the
// web UI sends may reach the limit, and the deepest must stay strictly below it
// so a view gaining one relationship hop is not an outage.
func TestShippedWebQueriesFitTheRecursionBound(t *testing.T) {
	schema := graph.NewExecutableSchema(graph.Config{Resolvers: &graph.Resolver{}}).Schema()
	recursive := recursiveTypeNames(schema)
	if !recursive["Nib"] {
		t.Fatalf("Nib is not among the recursive types %v; the bound would count nothing", recursive)
	}

	deepest, deepestName := 0, ""
	heaviest, heaviestName := 0, ""
	for name, doc := range webQueryDocuments(t) {
		parsed, errs := gqlparser.LoadQueryWithRules(schema, doc, rules.NewDefaultRules())
		if errs != nil {
			t.Fatalf("%s does not load against the schema: %v", name, errs)
		}
		for _, op := range parsed.Operations {
			cost := measureRecursion(op.SelectionSet, recursive)
			if cost.depth > maxRecursiveSelectionDepth {
				t.Errorf("%s nests %d recursive fields, above the served limit of %d",
					name, cost.depth, maxRecursiveSelectionDepth)
			}
			if cost.depth > deepest {
				deepest, deepestName = cost.depth, name
			}
			if cost.total > heaviest {
				heaviest, heaviestName = cost.total, name
			}
		}
	}
	if deepest >= maxRecursiveSelectionDepth {
		t.Errorf("deepest shipped document (%s) is %d, leaving no headroom under the limit of %d",
			deepestName, deepest, maxRecursiveSelectionDepth)
	}
	// Both constants claim headroom over the shipped client, so both claims are
	// held to account here: the total bound must be at least twice what any
	// document asks for, which is the "room to grow" its comment states.
	if heaviest*2 > maxRecursiveSelectionTotal {
		t.Errorf("heaviest shipped document (%s) selects %d recursive fields, more than half the limit of %d",
			heaviestName, heaviest, maxRecursiveSelectionTotal)
	}
	t.Logf("deepest shipped web document: %s at %d recursive fields (limit %d); "+
		"heaviest: %s selecting %d in total (limit %d)",
		deepestName, deepest, maxRecursiveSelectionDepth,
		heaviestName, heaviest, maxRecursiveSelectionTotal)
}

// doublingFragments builds a document whose n fragments each spread the previous
// one twice, over a base selection of `leaf`. It is legal and cycle-free, it
// stays around 40 bytes per fragment, and gqlparser validates it in microseconds
// — so a measurement that expands a spread at every occurrence rather than once
// per fragment turns a document a client can send in a URL into 2^n work inside
// the guard itself.
func doublingFragments(n int, leaf string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "{ nib(id: \"root\") { ...f%d } }\n", n)
	fmt.Fprintf(&b, "fragment f0 on Nib { %s }\n", leaf)
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "fragment f%d on Nib { ...f%d ...f%d }\n", i, i-1, i-1)
	}
	return b.String()
}

// The guard must not be a denial of service of its own: measuring a document
// costs one visit per fragment, not one per spread.
func TestRecursionMeasurementExpandsEachFragmentOnce(t *testing.T) {
	schema := graph.NewExecutableSchema(graph.Config{Resolvers: &graph.Resolver{}}).Schema()
	recursive := recursiveTypeNames(schema)

	const fragments = 40
	doc := doublingFragments(fragments, "id")
	if len(doc) > 4096 {
		t.Fatalf("the probe document is %d bytes; it is meant to be one a client could send in a URL", len(doc))
	}
	parsed, errs := gqlparser.LoadQueryWithRules(schema, doc, rules.NewDefaultRules())
	if errs != nil {
		t.Fatalf("the probe document must be legal GraphQL: %v", errs)
	}

	// Unmemoized this is 2^40 visits and never returns, so the observable is
	// that the measurement finishes at all. Memoized it is 40 visits and takes
	// microseconds, which is why a ten-second deadline cannot flake. The
	// measurement runs on its own goroutine so a regression fails here instead
	// of hanging the package's whole run.
	measured := make(chan recursionCost, 1)
	go func() {
		measured <- measureRecursion(parsed.Operations[0].SelectionSet, recursive)
	}()
	select {
	case cost := <-measured:
		// The document asks for nothing: `nib` is the only recursive field it
		// selects, at either bound. An unmemoized guard therefore pays the 2^n
		// and then serves the query — the cost is the guard's own, not the
		// resolvers'.
		if cost.depth != 1 || cost.total != 1 {
			t.Fatalf("cost = %+v, want depth 1 and total 1", cost)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("measuring a %d-fragment document did not finish in 10s; each spread is being re-expanded", fragments)
	}
}

// The set of counted types is derived from the schema so that a later recursive
// type is covered without a second edit, and recursion does not have to be
// declared by one type to exist: a union or an interface can close the cycle
// between types that each look acyclic on their own. Today's schema has neither,
// so this holds that promise to account on a schema that does.
func TestRecursiveTypeNamesSeesCyclesThroughAbstractTypes(t *testing.T) {
	schema := gqlparser.MustLoadSchema(&ast.Source{Name: "abstract.graphql", Input: `
		type Query { node: Node, thing: Thing }
		interface Node { id: ID! }
		# Linked has no field of its own type: the cycle runs through Node.
		type Linked implements Node { id: ID!, link: Node }
		union Thing = Boxed
		# A union declares no fields at all, so Boxed's cycle is only visible
		# through the union's members.
		type Boxed { thing: Thing }
	`})

	recursive := recursiveTypeNames(schema)
	for _, name := range []string{"Node", "Linked", "Thing", "Boxed"} {
		if !recursive[name] {
			t.Errorf("%s is not counted as recursive; a document can re-enter it without limit", name)
		}
	}
	// The root is reachable from nothing, so counting it would bound every
	// document by its own entry field.
	if recursive["Query"] {
		t.Error("Query is counted as recursive")
	}
}

// The bound is a policy of the SERVED endpoint, and the in-process executor
// behind `nibs query` deliberately does not carry it: that path runs in the
// user's own process against the store they pointed it at, with no untrusted
// page able to drive it, and it is the documented precision path for traversing
// relationships in one hop.
//
// Twelve alternating fields over a two-child fixture resolves 2^6 = 64 objects,
// so the unbounded path is exercised at a size that cannot blow up the machine.
func TestCLIExecutorIsNotBoundByTheServedRecursionLimit(t *testing.T) {
	app := relationFixtureApp(t)

	data, err := executeQuery(app, alternating(12), nil, "")
	if err != nil {
		t.Fatalf("the in-process executor refused a deep query: %v", err)
	}
	if !strings.Contains(string(data), `"kid0"`) {
		t.Fatalf("deep query returned no data: %s", data)
	}
}

// The deepest read the shipped web client performs — NibDetail selects all six
// recursive relationship fields at one level each — must still execute.
func TestGraphQLServesTheDeepestShippedWebQuery(t *testing.T) {
	app := relationFixtureApp(t)
	server := httptest.NewServer(newServeMux(app, nil))
	defer server.Close()

	doc, ok := webQueryDocuments(t)["NibDetail"]
	if !ok {
		t.Fatal("NibDetail is no longer among the web client's documents")
	}
	// The document declares $id, so send it as a named operation with variables.
	body, err := json.Marshal(map[string]any{
		"query":     doc,
		"variables": map[string]any{"id": "root"},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	resp, err := http.Post(server.URL+"/graphql", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var decoded graphQLHTTPResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Errors) > 0 {
		t.Fatalf("NibDetail was refused: %v", decoded.Errors)
	}
	if !strings.Contains(string(decoded.Data), `"root"`) {
		t.Fatalf("NibDetail returned no nib: %s", decoded.Data)
	}
}

// A request naming a disallowed origin must be refused, not merely served
// without CORS headers: the browser discards the response, but the server has
// already paid for it. A request with NO Origin — the CLI, curl, a same-origin
// browser fetch — must still succeed.
func TestCORSRefusesDisallowedOriginBeforeServing(t *testing.T) {
	app := relationFixtureApp(t)
	mux := newServeMux(app, nil)

	const query = `{ nib(id: "root") { id } }`

	tests := []struct {
		name       string
		origin     string
		host       string
		wantServed bool
	}{
		{"no origin header", "", "127.0.0.1:3000", true},
		{"allowed localhost origin", "http://localhost:3000", "127.0.0.1:3000", true},
		{"allowed loopback origin", "http://127.0.0.1:5173", "127.0.0.1:3000", true},
		// Serving on a LAN address is supported (`--host`), and a browser sends
		// Origin on same-origin POSTs, so the request's own authority must be
		// accepted or the refusal breaks the very UI it protects.
		{"same-origin on a LAN address", "http://192.168.1.5:3000", "192.168.1.5:3000", true},
		{"cross-origin attacker page", "https://evil.example", "127.0.0.1:3000", false},
		{"non-loopback http origin", "http://192.168.1.9:8080", "127.0.0.1:3000", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"query": query})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "http://"+tt.host+"/graphql", bytes.NewReader(body))
			req.Host = tt.host
			req.Header.Set("Content-Type", "application/json")
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if tt.wantServed {
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
				}
				if !strings.Contains(rec.Body.String(), `"root"`) {
					t.Fatalf("request was not served: %s", rec.Body.String())
				}
				return
			}
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 — a disallowed origin must not be executed (body: %s)",
					rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), `"root"`) {
				t.Fatalf("disallowed origin was served the query result: %s", rec.Body.String())
			}
		})
	}
}

// Preflight still short-circuits with 204 for an origin the server serves.
func TestCORSPreflightStillAnswers204(t *testing.T) {
	app := relationFixtureApp(t)
	mux := newServeMux(app, nil)

	req := httptest.NewRequest(http.MethodOptions, "http://127.0.0.1:3000/graphql", nil)
	req.Host = "127.0.0.1:3000"
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want the echoed origin", got)
	}
}

// An oversized request body must be refused rather than buffered in full.
func TestGraphQLRequestBodyIsCapped(t *testing.T) {
	app := relationFixtureApp(t)
	server := httptest.NewServer(newServeMux(app, nil))
	defer server.Close()

	// A syntactically valid document whose padding pushes it past the cap.
	padding := strings.Repeat("x", maxRequestBodyBytes+1024)
	query := `{ nib(id: "` + padding + `") { id } }`
	status, resp := postGraphQL(t, server.URL, query)

	if status == http.StatusOK && len(resp.Errors) == 0 {
		t.Fatalf("an oversized body was accepted (status %d, data %s)", status, resp.Data)
	}
}

// Trap check for the http.Server timeouts: this server hands long-lived
// WebSocket subscriptions to gqlgen, and a write deadline on a hijacked
// connection would kill live updates silently. Go clears the connection's
// deadlines inside Hijack (net/http server.go, hijackLocked), so the timeouts
// bound ordinary requests only — this test drives a real socket past a
// deliberately tiny WriteTimeout to hold that claim to account.
func TestWebSocketSurvivesHTTPServerWriteTimeout(t *testing.T) {
	app := relationFixtureApp(t)

	const tinyTimeout = 200 * time.Millisecond
	server := httptest.NewUnstartedServer(newGraphQLHandler(app, wsTestInterval))
	server.Config.ReadHeaderTimeout = tinyTimeout
	server.Config.ReadTimeout = tinyTimeout
	server.Config.WriteTimeout = tinyTimeout
	server.Start()
	defer server.Close()

	conn := dialTransportWS(t, server.URL)

	// Several multiples of the write deadline. A connection killed by it would
	// fail well inside this window.
	window := 10 * tinyTimeout
	if err := conn.SetReadDeadline(time.Now().Add(window)); err != nil {
		t.Fatal(err)
	}
	pings := 0
	for {
		var msg map[string]any
		err := conn.ReadJSON(&msg)
		if err == nil {
			if msg["type"] == "ping" {
				pings++
				if werr := conn.WriteJSON(map[string]any{"type": "pong"}); werr != nil {
					t.Fatalf("write pong after %v: %v", window, werr)
				}
			}
			continue
		}
		if isOwnTimeout(err) {
			break // survived the window
		}
		t.Fatalf("connection died under a %v WriteTimeout: %v", tinyTimeout, err)
	}
	if pings < 2 {
		t.Fatalf("only %d ping(s); the window did not outlast the write deadline", pings)
	}
}

// areaDepthApp builds a store whose declared areas nest `depth` levels in a
// single chain, so a query against it exercises a vocabulary deeper than the
// document bound would ever allow a recursive type to be walked.
func areaDepthApp(t *testing.T, depth int) *App {
	t.Helper()
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatalf("creating the test store: %v", err)
	}

	// Built from the leaf up, since a node owns its children.
	node := config.AreaConfig{Name: fmt.Sprintf("level%d", depth-1)}
	for i := depth - 2; i >= 0; i-- {
		node = config.AreaConfig{Name: fmt.Sprintf("level%d", i), Children: []config.AreaConfig{node}}
	}
	cfg := config.Default()
	cfg.Areas = []config.AreaConfig{node}
	if err := cfg.ValidateAreas(); err != nil {
		t.Fatalf("the chain fixture is not a valid vocabulary: %v", err)
	}

	testCore := nibcore.New(nibsDir, cfg)
	if err := testCore.Load(); err != nil {
		t.Fatalf("loading the test store: %v", err)
	}
	t.Cleanup(func() { _ = testCore.Close() })
	return &App{Core: testCore}
}

// `Area` is deliberately NOT self-recursive, and this is the guard that keeps it
// that way. Area nesting in a store's config is unbounded, while this endpoint
// refuses a document that nests a recursive type past
// maxRecursiveSelectionDepth — so an `Area` carrying `children` would enroll
// itself in that bound and leave a vocabulary nested deeper than the bound with
// no queryable shape at all. The flat, depth-carrying list exists so the
// document's depth stays fixed however deep the vocabulary goes.
func TestAreaVocabularyIsQueryableBelowTheRecursionBound(t *testing.T) {
	schema := graph.NewExecutableSchema(graph.Config{Resolvers: &graph.Resolver{}}).Schema()
	if recursiveTypeNames(schema)["Area"] {
		t.Fatal("Area is counted as a recursive type; a vocabulary nested past the depth bound would be unqueryable")
	}

	depth := maxRecursiveSelectionDepth + 3
	server := httptest.NewServer(newServeMux(areaDepthApp(t, depth), nil))
	defer server.Close()

	status, resp := postGraphQL(t, server.URL, `{ config { areas { path depth } } }`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if len(resp.Errors) > 0 {
		t.Fatalf("query refused: %s", resp.Errors[0].Message)
	}

	var decoded struct {
		Config struct {
			Areas []struct {
				Path  string `json:"path"`
				Depth int    `json:"depth"`
			} `json:"areas"`
		} `json:"config"`
	}
	if err := json.Unmarshal(resp.Data, &decoded); err != nil {
		t.Fatalf("decoding data: %v", err)
	}
	areas := decoded.Config.Areas
	if len(areas) != depth {
		t.Fatalf("got %d areas, want the whole %d-deep chain", len(areas), depth)
	}
	deepest := areas[len(areas)-1]
	if deepest.Depth != depth-1 {
		t.Errorf("deepest area depth = %d, want %d", deepest.Depth, depth-1)
	}
}
