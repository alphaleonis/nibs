package graph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

func ptr(s string) *string { return &s }

// TestAddAreaDeclaresARootArea is the tracer bullet for the one area verb whose
// write path had no wire surface: lock, re-read, plan, write, and answer with the
// whole vocabulary as it now stands.
//
// The new node lands in NAME order rather than at the end of the file, because
// that is the order Config.areas is specified in; where the declaration sits in
// areas.yml carries no meaning.
func TestAddAreaDeclaresARootArea(t *testing.T) {
	resolver, core := setupTestResolverWithAreas(t)

	payload, err := resolver.Mutation().AddArea(context.Background(), model.AddAreaInput{
		Path:        "platform",
		Description: ptr("Build, release and tooling"),
		Color:       ptr("teal"),
	})
	if err != nil {
		t.Fatalf("AddArea: %v", err)
	}

	got := pathsOf(payload.Config.Areas)
	want := []string{"auth", "platform", "web", "web/dashboard", "web/ui"}
	if !slices.Equal(got, want) {
		t.Errorf("areas = %v, want %v", got, want)
	}
	if len(payload.Notes) != 0 {
		t.Errorf("notes = %v, want none for an ordinary edit", payload.Notes)
	}

	stored := storedAreasFile(t, core.Root())
	for _, want := range []string{"name: platform", "Build, release and tooling", "teal"} {
		if !strings.Contains(stored, want) {
			t.Errorf("areas.yml does not carry %q:\n%s", want, stored)
		}
	}
}

// TestAddAreaNestsUnderADeclaredParent: the argument is the FULL path of the new
// node, so the leaf lands under the node the rest of it names rather than beside
// it at the root.
func TestAddAreaNestsUnderADeclaredParent(t *testing.T) {
	resolver, _ := setupTestResolverWithAreas(t)

	payload, err := resolver.Mutation().AddArea(context.Background(),
		model.AddAreaInput{Path: "web/settings", Description: ptr("Preferences")})
	if err != nil {
		t.Fatalf("AddArea: %v", err)
	}

	got := pathsOf(payload.Config.Areas)
	if !slices.Contains(got, "web/settings") {
		t.Errorf("web/settings is not declared: %v", got)
	}
	if slices.Contains(got, "settings") {
		t.Errorf("the child was declared at the root as well: %v", got)
	}
}

// TestAddAreaBootstrapsAStoreDeclaringNoAreas is the case this mutation exists
// for as much as any: updateArea and removeArea refuse a store with no
// vocabulary, because there is no node for them to name. Add is the verb that
// gets a project out of that state, so it must not refuse it — and a store whose
// areas.yml is missing entirely is the ordinary first call, not a broken store.
func TestAddAreaBootstrapsAStoreDeclaringNoAreas(t *testing.T) {
	resolver, core := setupTestResolver(t)
	resolver.AreaWriter = core

	payload, err := resolver.Mutation().AddArea(context.Background(),
		model.AddAreaInput{Path: "platform", Description: ptr("Build and release")})
	if err != nil {
		t.Fatalf("AddArea over a store declaring no areas: %v", err)
	}

	if got := pathsOf(payload.Config.Areas); !slices.Equal(got, []string{"platform"}) {
		t.Errorf("areas = %v, want the vocabulary this call bootstrapped", got)
	}
	if stored := storedAreasFile(t, core.Root()); !strings.Contains(stored, "name: platform") {
		t.Errorf("areas.yml does not declare the bootstrapped area:\n%s", stored)
	}
}

// TestAddAreaRefusesAPathAlreadyDeclared keeps the mutation from reporting a
// declaration it did not make, and from writing the duplicate that would make one
// path mean two nodes.
func TestAddAreaRefusesAPathAlreadyDeclared(t *testing.T) {
	resolver, core := setupTestResolverWithAreas(t)
	before := storedAreasFile(t, core.Root())

	_, err := resolver.Mutation().AddArea(context.Background(), model.AddAreaInput{Path: "web/dashboard"})
	if err == nil {
		t.Fatal("declaring an area the store already declares was accepted")
	}
	if !strings.Contains(err.Error(), "already declares") {
		t.Errorf("error = %q, want the already-declared refusal", err.Error())
	}
	if after := storedAreasFile(t, core.Root()); after != before {
		t.Errorf("the refused add rewrote the vocabulary:\n%s", after)
	}
}

// TestAddAreaRefusesAnUndeclaredParent is the decision not to auto-create
// intermediates, at the second surface that could have diverged on it: one typo
// would otherwise mint two permanent areas, and the vocabulary is what authorizes
// an `area:` value at all.
//
// The refusal has to name the parent and prescribe the remedy in THIS surface's
// terms — a caller reaching the API over HTTP has no `nibs area add` to run — and
// the remedy has to work.
func TestAddAreaRefusesAnUndeclaredParent(t *testing.T) {
	resolver, core := setupTestResolverWithAreas(t)
	before := storedAreasFile(t, core.Root())

	_, err := resolver.Mutation().AddArea(context.Background(), model.AddAreaInput{Path: "wbe/dashboard"})
	if err == nil {
		t.Fatal("declaring an area under an undeclared parent was accepted")
	}
	for _, want := range []string{"wbe", "addArea"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), "nibs area add") {
		t.Errorf("the refusal prescribes a CLI command to an API caller: %q", err.Error())
	}
	if after := storedAreasFile(t, core.Root()); after != before {
		t.Errorf("the refused add minted something:\n%s", after)
	}

	// The remedy the refusal prescribes is what makes the original call work.
	if _, err := resolver.Mutation().AddArea(context.Background(), model.AddAreaInput{Path: "wbe"}); err != nil {
		t.Fatalf("the prescribed remedy failed: %v", err)
	}
	if _, err := resolver.Mutation().AddArea(context.Background(), model.AddAreaInput{Path: "wbe/dashboard"}); err != nil {
		t.Fatalf("the call after the remedy failed: %v", err)
	}
}

// TestAddAreaAnswersArgumentsWithoutTheStore is an ORDER guard: waiting for the
// store's write lock has no deadline but the request's own end, so a refusal the
// arguments alone answer, asked afterwards, leaves a typo parked behind whatever
// other writer holds the store.
//
// The stub is what detects it — the store is never asked at all — and the class
// matters as much as the message: these are rejected arguments, so none of them
// may arrive carrying the IO type a client routes a rerun on.
func TestAddAreaAnswersArgumentsWithoutTheStore(t *testing.T) {
	tests := []struct {
		name  string
		input model.AddAreaInput
		want  string
	}{
		{
			name:  "no path at all",
			input: model.AddAreaInput{Path: ""},
			want:  "none was given",
		},
		{
			name:  "a path with an empty segment",
			input: model.AddAreaInput{Path: "web//panel"},
			want:  "no name in it",
		},
		{
			name:  "a name with trailing whitespace",
			input: model.AddAreaInput{Path: "platform "},
			want:  "whitespace",
		},
		{
			name:  "a name no store could read back",
			input: model.AddAreaInput{Path: strings.Repeat("x", 201)},
			want:  "bounded at 200",
		},
		{
			name:  "a color the vocabulary cannot hold",
			input: model.AddAreaInput{Path: "platform", Color: ptr("#xyz")},
			want:  "hex code",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, _ := setupTestResolverWithAreas(t)
			stub := &stubAreaWriter{}
			resolver.AreaWriter = stub

			_, err := resolver.Mutation().AddArea(context.Background(), tt.input)
			if err == nil {
				t.Fatal("the mutation was accepted")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.want)
			}
			if stub.calls != 0 {
				t.Errorf("the store was asked %d times for a question the arguments answer", stub.calls)
			}
			var ioErr *nibcore.AreaEditIOError
			if errors.As(err, &ioErr) {
				t.Errorf("error = %v, want a validation-class refusal", err)
			}
		})
	}
}

// TestAddAreaWordsAFailedWrite covers the one IO phase this verb words itself.
// The shared wording serves the phases every verb reaches; add also reaches the
// areas.yml write, and its sentence differs from rename's and retire's precisely
// because nothing was rewritten first — there are no members to report as
// stranded and no disposition to drop on the rerun.
//
// The TYPE has to survive the wording: cmd/set.go's mutationErrCode classifies
// `nibs query`'s exit on it, and AreaEditIOError implements no Unwrap, so a
// surface that replaced it would hand back a bare validation fallback.
func TestAddAreaWordsAFailedWrite(t *testing.T) {
	resolver, _ := setupTestResolverWithAreas(t)
	resolver.AreaWriter = &stubAreaWriter{err: &nibcore.AreaEditIOError{
		Phase: nibcore.AreaEditPhaseWrite,
		Path:  "platform",
		Cause: errors.New("disk on fire"),
	}}

	_, err := resolver.Mutation().AddArea(context.Background(), model.AddAreaInput{Path: "platform"})
	if err == nil {
		t.Fatal("a failed areas.yml write was reported as a success")
	}
	for _, want := range []string{"platform", "disk on fire", "nothing else was written", "rerun"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err.Error(), want)
		}
	}
	var ioErr *nibcore.AreaEditIOError
	if !errors.As(err, &ioErr) {
		t.Errorf("error = %v, want the IO type a client routes a rerun on", err)
	}
}

// TestAddAreaAdoptsANibStrandedOnThatPath is what "this verb rewrites no nib"
// means in practice. A nib can already CARRY a path the vocabulary stopped
// declaring — a retire leaves one that way — and every write to it is refused
// until the path is declared again. Declaring it IS the repair, on the strength
// of the value the nib is already holding, so no cascade is owed here.
func TestAddAreaAdoptsANibStrandedOnThatPath(t *testing.T) {
	resolver, core := setupTestResolverWithAreas(t)

	// Written as bytes rather than created through the store: Core.Create refuses
	// an undeclared area, which is the very state this test needs to start from.
	stranded := &nib.Nib{
		ID: "str1", Version: nib.CurrentVersion, Title: "Left behind by a retire",
		Status: "todo", Type: "task", Priority: "normal", Area: "platform",
	}
	rendered, err := stranded.Render()
	if err != nil {
		t.Fatalf("rendering the stranded nib: %v", err)
	}
	if err := os.WriteFile(filepath.Join(core.Root(), "data", "str1.md"), rendered, 0o644); err != nil {
		t.Fatalf("landing the stranded nib: %v", err)
	}
	if err := core.Load(); err != nil {
		t.Fatalf("reloading the store: %v", err)
	}

	// The premise: the nib is stranded, so a write to it is refused. Without this
	// the repair below proves nothing.
	if _, err := resolver.Mutation().UpdateNib(context.Background(), "str1", areaInput("platform")); err == nil {
		t.Fatal("a nib carrying an undeclared area must be write-refused")
	}

	if _, err := resolver.Mutation().AddArea(context.Background(), model.AddAreaInput{Path: "platform"}); err != nil {
		t.Fatalf("AddArea: %v", err)
	}

	if got := storedAreaOfNib(t, core, "str1"); got != "platform" {
		t.Errorf("area = %q, want the value the nib already carried — nothing should have rewritten it", got)
	}
	if _, err := resolver.Mutation().UpdateNib(context.Background(), "str1", areaInput("platform")); err != nil {
		t.Errorf("the nib is still write-refused after its area was declared: %v", err)
	}
}
