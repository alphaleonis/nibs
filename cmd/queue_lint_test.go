package cmd

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// queueLintFiles is one milestone queue whose order and dependencies agree: qb
// blocks qa and sits ahead of it. Moving qa in front of qb is therefore the
// smallest write that CREATES an inversion. qc carries no edge, so a test can
// add one without meeting the cycle guard on the qa/qb pair.
//
// queueLintServer builds the same three nibs, so the two entry points below are
// compared on identical ground.
var queueLintFiles = map[string]string{
	"qm1--waypoint.md": "---\nversion: 2\ntitle: Waypoint\nstatus: todo\ntype: milestone\n---\n",
	"qb--blocker.md":   "---\nversion: 2\ntitle: Blocker\nstatus: todo\ntype: task\nmilestone: qm1\nmilestone_order: a0\n---\n",
	"qa--blocked.md":   "---\nversion: 2\ntitle: Blocked\nstatus: todo\ntype: task\nblocked_by:\n    - qb\nmilestone: qm1\nmilestone_order: b0\n---\n",
	"qc--bystander.md": "---\nversion: 2\ntitle: Bystander\nstatus: todo\ntype: task\nmilestone: qm1\nmilestone_order: c0\n---\n",
}

// queueLintServer serves the same fixture over HTTP.
func queueLintServer(t *testing.T) *httptest.Server {
	t.Helper()
	app := setupServeTestApp(t)
	for _, b := range []*nib.Nib{
		{ID: "qm1", Slug: "waypoint", Title: "Waypoint", Type: "milestone", Status: "todo"},
		{ID: "qb", Slug: "blocker", Title: "Blocker", Type: "task", Status: "todo", Milestone: "qm1", MilestoneOrder: "a0"},
		{ID: "qa", Slug: "blocked", Title: "Blocked", Type: "task", Status: "todo", Milestone: "qm1", MilestoneOrder: "b0", BlockedBy: []string{"qb"}},
		{ID: "qc", Slug: "bystander", Title: "Bystander", Type: "task", Status: "todo", Milestone: "qm1", MilestoneOrder: "c0"},
	} {
		if err := app.Core.Create(b); err != nil {
			t.Fatalf("create %s: %v", b.ID, err)
		}
	}
	server := httptest.NewServer(newServeMux(app, nil))
	t.Cleanup(server.Close)
	return server
}

// inversionTriples reads the reported pairs off a response's extensions.
func inversionTriples(t *testing.T, resp graphQLHTTPResponse) []map[string]string {
	t.Helper()
	raw, ok := resp.Extensions["queueInversions"]
	if !ok {
		return nil
	}
	var out []map[string]string
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode queueInversions: %v", err)
	}
	return out
}

// TestServeReportsQueueInversions closes the gap this change is about: a queue
// move through the HTTP server used to persist an inversion with nothing in the
// response naming it, because the lint lived in cmd/ and only the CLI ran it.
func TestServeReportsQueueInversions(t *testing.T) {
	t.Run("a queue move that crosses a blocker reports the pair", func(t *testing.T) {
		server := queueLintServer(t)

		_, resp := postGraphQL(t, server.URL,
			`mutation { reorderNib(id: "qa", beforeId: "qb", scope: MILESTONE) { id } }`)
		if len(resp.Errors) > 0 {
			t.Fatalf("mutation refused: %v", resp.Errors)
		}

		got := inversionTriples(t, resp)
		want := []map[string]string{{"milestone": "qm1", "ahead": "qa", "blocker": "qb"}}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("extensions.queueInversions = %v, want %v", got, want)
		}
	})

	t.Run("a move that creates none carries no extension", func(t *testing.T) {
		// The key is absent rather than an empty list: a client reading it as
		// "there is something to say" must not be handed an empty something.
		server := queueLintServer(t)

		_, resp := postGraphQL(t, server.URL,
			`mutation { reorderNib(id: "qb", first: true, scope: MILESTONE) { id } }`)
		if len(resp.Errors) > 0 {
			t.Fatalf("mutation refused: %v", resp.Errors)
		}
		if _, ok := resp.Extensions["queueInversions"]; ok {
			t.Errorf("extensions carry queueInversions for a move that created none: %v", resp.Extensions)
		}
	})

	t.Run("a plain query carries no extension", func(t *testing.T) {
		server := queueLintServer(t)

		_, resp := postGraphQL(t, server.URL, `{ nib(id: "qa") { id } }`)
		if _, ok := resp.Extensions["queueInversions"]; ok {
			t.Errorf("a read reported inversions: %v", resp.Extensions)
		}
	})

	t.Run("two queue-shaping fields in one document both report", func(t *testing.T) {
		// The reason the extension is registered once per OPERATION rather than
		// from the resolver: graphql.RegisterExtension panics on a second
		// registration of the same key, so this document would have taken the
		// server down.
		server := queueLintServer(t)

		_, resp := postGraphQL(t, server.URL,
			`mutation {
			   a: reorderNib(id: "qa", beforeId: "qb", scope: MILESTONE) { id }
			   b: updateNib(id: "qc", input: { addBlocking: ["qa"] }) { id }
			 }`)
		if len(resp.Errors) > 0 {
			t.Fatalf("mutation refused: %v", resp.Errors)
		}
		if got := inversionTriples(t, resp); len(got) != 2 {
			t.Errorf("reported %d pairs, want 2 (one per field): %v", len(got), got)
		}
	})
}

// TestQueryCommandWarnsOnCreatedInversion covers the third entry point the nib
// names: the in-process executor. `nibs query` prints the GraphQL data and
// nothing else on stdout, in both modes, so the warning goes to stderr — where
// a piped `--json` consumer is unaffected and a human still sees it.
func TestQueryCommandWarnsOnCreatedInversion(t *testing.T) {
	nibsPath := writeStoreFiles(t, queueLintFiles)
	t.Cleanup(resetQueueCLIFlags)
	resetQueueCLIFlags()
	var stderr strings.Builder
	rootCmd.SetErr(&stderr)
	t.Cleanup(func() { rootCmd.SetErr(nil) })

	if _, err := runRootWith(t, "--nibs-path", nibsPath, "query",
		`mutation { reorderNib(id: "qa", beforeId: "qb", scope: MILESTONE) { id } }`); err != nil {
		t.Fatalf("query mutation: %v", err)
	}

	if !strings.Contains(stderr.String(), "qa is ahead of qb, which still blocks it") {
		t.Errorf("stderr = %q, want the inversion warning", stderr.String())
	}
}

// TestQueueInversionEntryPointsAgree is the acceptance this nib names: one
// definition, two entry points. The same move on the same fixture is driven
// through `nibs mv --queue` and through the GraphQL server, and the pair the
// server reports must be the pair the CLI's sentence names, in the same roles.
func TestQueueInversionEntryPointsAgree(t *testing.T) {
	server := queueLintServer(t)
	_, resp := postGraphQL(t, server.URL,
		`mutation { reorderNib(id: "qa", beforeId: "qb", scope: MILESTONE) { id } }`)
	if len(resp.Errors) > 0 {
		t.Fatalf("mutation refused: %v", resp.Errors)
	}
	served := inversionTriples(t, resp)
	if len(served) != 1 {
		t.Fatalf("server reported %d pairs, want 1: %v", len(served), served)
	}

	nibsPath := writeStoreFiles(t, queueLintFiles)
	t.Cleanup(resetQueueCLIFlags)
	resetQueueCLIFlags()
	var stderr strings.Builder
	rootCmd.SetErr(&stderr)
	t.Cleanup(func() { rootCmd.SetErr(nil) })
	if _, err := runRootWith(t, "--nibs-path", nibsPath, "mv", "qa", "--queue", "--before", "qb"); err != nil {
		t.Fatalf("mv --queue --before: %v", err)
	}

	// Built from what the SERVER answered, so the CLI is held to the other
	// entry point's ids and roles rather than to a literal repeated here.
	want := fmt.Sprintf("milestone %s: %s is ahead of %s, which still blocks it",
		served[0]["milestone"], served[0]["ahead"], served[0]["blocker"])
	if !strings.Contains(stderr.String(), want) {
		t.Errorf("CLI warning = %q, want it to name %q", stderr.String(), want)
	}
}
