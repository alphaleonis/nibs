package graph

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/executor"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// renameAreaOverTheWire runs one renameArea document through the executable
// schema and returns the decoded payload, as an API client receives it. A
// resolver-level assertion cannot stand in for this: it reads a Go struct, where
// a field that never marshals looks the same as one that does.
func renameAreaOverTheWire(t *testing.T, resolver *Resolver) (notes []string, prefixPresent bool) {
	t.Helper()

	es := NewExecutableSchema(Config{Resolvers: resolver})
	exec := executor.New(es)

	ctx := graphql.StartOperationTrace(context.Background())
	params := &graphql.RawParams{
		Query:     `mutation { renameArea(input: {path: "web", newName: "platform"}) { notes config { prefix } } }`,
		Variables: map[string]any{},
	}
	opCtx, errs := exec.CreateOperationContext(ctx, params)
	if len(errs) > 0 {
		t.Fatalf("CreateOperationContext: %v", errs)
	}
	ctx = graphql.WithOperationContext(ctx, opCtx)
	handler, ctx := exec.DispatchOperation(ctx, opCtx)
	resp := handler(ctx)
	if len(resp.Errors) > 0 {
		t.Fatalf("renameArea returned errors: %v", resp.Errors)
	}

	var decoded struct {
		RenameArea struct {
			Notes  []string `json:"notes"`
			Config *struct {
				Prefix string `json:"prefix"`
			} `json:"config"`
		} `json:"renameArea"`
	}
	if err := json.Unmarshal(resp.Data, &decoded); err != nil {
		t.Fatalf("unmarshal %s: %v", resp.Data, err)
	}
	return decoded.RenameArea.Notes, decoded.RenameArea.Config != nil
}

// TestAreaMutationDeliversTheNoteToAWireCaller is what the payload type exists
// for: the note an edit owes when it replaced a symlinked areas.yml reaches the
// caller, rather than only the server's own warning sink, which under
// `nibs serve` no API consumer reads.
func TestAreaMutationDeliversTheNoteToAWireCaller(t *testing.T) {
	resolver, core := setupTestResolverWithAreas(t)
	stub := &stubAreaWriter{res: nibcore.AreaEditResult{
		Areas:           core.Areas(),
		StaleLinkTarget: "/srv/vocab/areas.yml",
	}}
	resolver.AreaWriter = stub

	notes, configPresent := renameAreaOverTheWire(t, resolver)
	if len(notes) != 1 {
		t.Fatalf("notes = %v, want the stale-link note", notes)
	}
	for _, want := range []string{"symlink", "regular file"} {
		if !strings.Contains(notes[0], want) {
			t.Errorf("note = %q, want substring %q", notes[0], want)
		}
	}
	// The warning sink still names the target; this channel must not. It answers
	// an unauthenticated HTTP client, and the served scrub rewrites rendered
	// ERRORS rather than the data of a successful answer.
	if strings.Contains(notes[0], "/srv/vocab") {
		t.Errorf("note = %q, want no filesystem path on the wire", notes[0])
	}
	if !configPresent {
		t.Error("config was not delivered alongside the note")
	}

	// An ordinary edit delivers an EMPTY list rather than null, which is what the
	// non-null `[String!]!` promises a client that iterates it without a guard.
	stub.res.StaleLinkTarget = ""
	notes, configPresent = renameAreaOverTheWire(t, resolver)
	if notes == nil || len(notes) != 0 {
		t.Errorf("notes = %v, want an empty list", notes)
	}
	if !configPresent {
		t.Error("config was not delivered")
	}
}
