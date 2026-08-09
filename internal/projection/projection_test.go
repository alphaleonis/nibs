package projection

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/nib"
)

// fakeResolver is an in-memory Resolver for unit tests: it looks nibs up in a
// map and returns pre-seeded computed/relation values keyed by nib ID.
type fakeResolver struct {
	nibs        map[string]*nib.Nib
	parentID    map[string]string
	childCount  map[string]int
	progress    map[string]any
	ready       map[string]bool
	blocking    map[string][]string
	mentions    map[string][]string
	mentionedBy map[string][]string
}

func (f *fakeResolver) NibByID(id string) (*nib.Nib, bool) { n, ok := f.nibs[id]; return n, ok }
func (f *fakeResolver) ParentID(id string) string          { return f.parentID[id] }
func (f *fakeResolver) ChildCount(id string) int           { return f.childCount[id] }
func (f *fakeResolver) Progress(id string) any             { return f.progress[id] }
func (f *fakeResolver) Ready(id string) bool               { return f.ready[id] }
func (f *fakeResolver) Blocking(id string) []string        { return f.blocking[id] }
func (f *fakeResolver) Mentions(id string) []string        { return f.mentions[id] }
func (f *fakeResolver) MentionedBy(id string) []string     { return f.mentionedBy[id] }

// fieldsEqual compares two field slices for order-sensitive equality.
func fieldsEqual(a, b []Field) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestViewFields locks the view-tier field sets (§5.2). Order is the canonical
// menu order returned by Selection.Fields.
func TestViewFields(t *testing.T) {
	tests := []struct {
		view string
		want []Field
	}{
		{"id", []Field{FieldID}},
		{"ref", []Field{FieldID, FieldTitle, FieldStatus, FieldType, FieldPriority}},
		{"card", []Field{
			FieldID, FieldTitle, FieldStatus, FieldType, FieldPriority,
			FieldEstimate, FieldTags, FieldParent, FieldOrder, FieldUpdatedAt, FieldBlockedBy,
		}},
		// full carries stored_parent alongside parent: it is the diagnostic view,
		// and parent alone cannot show a link that resolves to nothing.
		{"full", []Field{
			FieldID, FieldSlug, FieldTitle, FieldStatus, FieldType, FieldPriority,
			FieldEstimate, FieldTags, FieldParent, FieldStoredParent, FieldOrder,
			FieldCreatedAt, FieldUpdatedAt, FieldPath, FieldBody, FieldETag,
			FieldBlocking, FieldBlockedBy, FieldMentions, FieldMentionedBy,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.view, func(t *testing.T) {
			sel, err := ViewFields(tt.view)
			if err != nil {
				t.Fatalf("ViewFields(%q) error: %v", tt.view, err)
			}
			if got := sel.Fields(); !fieldsEqual(got, tt.want) {
				t.Errorf("ViewFields(%q) = %v, want %v", tt.view, got, tt.want)
			}
		})
	}
}

// TestViewFields_CardAndFullRelationsAreIdLists asserts that the relation fields
// in the card/full tiers are in id-list (bare) form, not nested.
func TestViewFields_CardAndFullRelationsAreIdLists(t *testing.T) {
	for _, view := range []string{"card", "full"} {
		sel, err := ViewFields(view)
		if err != nil {
			t.Fatalf("ViewFields(%q): %v", view, err)
		}
		if sub := sel.Sub(FieldBlockedBy); sub != nil {
			t.Errorf("ViewFields(%q) blocked-by should be id-list (nil sub), got %v", view, sub)
		}
	}
}

// TestViewFields_ComputedNotInTiers ensures the computed fields are never part
// of a view tier — they are opt-in via -f only.
func TestViewFields_ComputedNotInTiers(t *testing.T) {
	for _, view := range []string{"id", "ref", "card", "full"} {
		sel, err := ViewFields(view)
		if err != nil {
			t.Fatalf("ViewFields(%q): %v", view, err)
		}
		for _, f := range []Field{FieldChildren, FieldProgress, FieldReady} {
			if sel.Has(f) {
				t.Errorf("ViewFields(%q) unexpectedly includes computed field %q", view, f)
			}
		}
	}
}

func TestViewFields_Unknown(t *testing.T) {
	_, err := ViewFields("bogus")
	if err == nil {
		t.Fatal("ViewFields(bogus) should error")
	}
	for _, v := range []string{"id", "ref", "card", "full"} {
		if !strings.Contains(err.Error(), v) {
			t.Errorf("error %q does not name valid view %q", err.Error(), v)
		}
	}
}

func TestParseFields(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    []Field
		subs    map[Field][]Field // expected nested sub-selections
		wantErr bool
		errSub  string
	}{
		{
			name: "bare fields",
			spec: "id,title",
			want: []Field{FieldID, FieldTitle},
		},
		{
			name: "trims whitespace",
			spec: " id , title ",
			want: []Field{FieldID, FieldTitle},
		},
		{
			name: "nested relation sub-selection",
			spec: "id,blocked-by(id,status)",
			want: []Field{FieldID, FieldBlockedBy},
			subs: map[Field][]Field{FieldBlockedBy: {FieldID, FieldStatus}},
		},
		{
			name: "view name expands inline and merges",
			spec: "ref,body",
			want: []Field{FieldID, FieldTitle, FieldStatus, FieldType, FieldPriority, FieldBody},
		},
		{
			name: "duplicate fields merge idempotently",
			spec: "id,id,title",
			want: []Field{FieldID, FieldTitle},
		},
		{
			name:    "empty spec",
			spec:    "",
			wantErr: true,
			errSub:  "empty",
		},
		{
			name:    "unknown token names the menu",
			spec:    "id,bogus",
			wantErr: true,
			errSub:  "blocked-by",
		},
		{
			name:    "deeper-than-one-level nesting rejected",
			spec:    "blocked-by(blocking(id))",
			wantErr: true,
			errSub:  "one level",
		},
		{
			name:    "scalar with sub-selection rejected",
			spec:    "title(id)",
			wantErr: true,
			errSub:  "does not support",
		},
		{
			name:    "relation with empty sub-selection rejected",
			spec:    "blocked-by()",
			wantErr: true,
			errSub:  "empty sub-selection",
		},
		{
			name:    "relation nested inside sub-selection rejected",
			spec:    "blocked-by(blocking)",
			wantErr: true,
			errSub:  "is a relation",
		},
		{
			name:    "unknown sub-field rejected",
			spec:    "blocked-by(bogus)",
			wantErr: true,
			errSub:  "unknown sub-field",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel, err := ParseFields(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseFields(%q) = %v, want error", tt.spec, sel.Fields())
				}
				if tt.errSub != "" && !strings.Contains(err.Error(), tt.errSub) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFields(%q) error: %v", tt.spec, err)
			}
			if got := sel.Fields(); !fieldsEqual(got, tt.want) {
				t.Errorf("ParseFields(%q) = %v, want %v", tt.spec, got, tt.want)
			}
			for f, wantSub := range tt.subs {
				if got := sel.Sub(f); !fieldsEqual(got, wantSub) {
					t.Errorf("Sub(%q) = %v, want %v", f, got, wantSub)
				}
			}
		})
	}
}

// TestParseFields_UnknownTokenNamesWholeMenu asserts the self-documenting
// contract: the unknown-token error lists every field in the menu.
func TestParseFields_UnknownTokenNamesWholeMenu(t *testing.T) {
	_, err := ParseFields("bogus")
	if err == nil {
		t.Fatal("expected error for unknown token")
	}
	for _, f := range FieldMenu() {
		if !strings.Contains(err.Error(), string(f)) {
			t.Errorf("error message does not name menu field %q: %s", f, err.Error())
		}
	}
}

// TestCompile_ViewPlusAdditiveFields covers the view + additive -f model.
func TestCompile_ViewPlusAdditiveFields(t *testing.T) {
	sel, err := Compile("ref", "body")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := []Field{FieldID, FieldTitle, FieldStatus, FieldType, FieldPriority, FieldBody}
	if got := sel.Fields(); !fieldsEqual(got, want) {
		t.Errorf("Compile(ref, body) = %v, want %v", got, want)
	}

	// -f re-listing a field already in the view is idempotent.
	sel2, err := Compile("card", "status,estimate")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	card, _ := ViewFields("card")
	if got := sel2.Fields(); !fieldsEqual(got, card.Fields()) {
		t.Errorf("Compile(card, status,estimate) = %v, want unchanged card %v", got, card.Fields())
	}
}

// TestProject_BodyEtagOptIn locks the firehose invariant: body and etag are
// absent unless explicitly selected (by name or by the full view).
func TestProject_BodyEtagOptIn(t *testing.T) {
	n := &nib.Nib{ID: "a", Title: "T", Status: "todo", Body: "secret prose", Path: "a.md"}

	ref, _ := ViewFields("ref")
	p, err := Project(n, ref, nil)
	if err != nil {
		t.Fatalf("Project(ref): %v", err)
	}
	if _, ok := p.Get("body"); ok {
		t.Error("ref projection must not include body")
	}
	if _, ok := p.Get("etag"); ok {
		t.Error("ref projection must not include etag")
	}

	withBody, _ := Compile("ref", "body")
	p2, err := Project(n, withBody, nil)
	if err != nil {
		t.Fatalf("Project(ref+body): %v", err)
	}
	if v, ok := p2.Get("body"); !ok || v != "secret prose" {
		t.Errorf("-f body must include body, got %v ok=%v", v, ok)
	}

	full, _ := ViewFields("full")
	p3, err := Project(n, full, &fakeResolver{})
	if err != nil {
		t.Fatalf("Project(full): %v", err)
	}
	if _, ok := p3.Get("body"); !ok {
		t.Error("full projection must include body")
	}
	if _, ok := p3.Get("etag"); !ok {
		t.Error("full projection must include etag")
	}
}

// TestProject_NestedRelationExpansion covers one-level nested relation
// projection through the fake accessor: id + status of each blocker.
func TestProject_NestedRelationExpansion(t *testing.T) {
	n := &nib.Nib{ID: "a", BlockedBy: []string{"b1", "b2"}}
	r := &fakeResolver{nibs: map[string]*nib.Nib{
		"b1": {ID: "b1", Status: "todo"},
		"b2": {ID: "b2", Status: "completed"},
	}}
	sel, err := ParseFields("id,blocked-by(id,status)")
	if err != nil {
		t.Fatalf("ParseFields: %v", err)
	}
	p, err := Project(n, sel, r)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	got, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"id":"a","blocked_by":[{"id":"b1","status":"todo"},{"id":"b2","status":"completed"}]}`
	if string(got) != want {
		t.Errorf("nested projection JSON =\n  %s\nwant\n  %s", got, want)
	}
}

// TestProject_NestedRelationSkipsDanglingIDs asserts an id that resolves to no
// nib is skipped, not projected as an error or a null entry.
func TestProject_NestedRelationSkipsDanglingIDs(t *testing.T) {
	n := &nib.Nib{ID: "a", BlockedBy: []string{"b1", "ghost"}}
	r := &fakeResolver{nibs: map[string]*nib.Nib{"b1": {ID: "b1", Status: "todo"}}}
	sel, _ := ParseFields("blocked-by(id,status)")
	p, err := Project(n, sel, r)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	got, _ := json.Marshal(p)
	want := `{"blocked_by":[{"id":"b1","status":"todo"}]}`
	if string(got) != want {
		t.Errorf("dangling-skip JSON = %s, want %s", got, want)
	}
}

// TestProject_BareRelationIsIdList covers bare relation fields: blocked-by read
// off the nib, blocking/mentions/mentioned-by via the resolver, each an id list.
func TestProject_BareRelationIsIdList(t *testing.T) {
	n := &nib.Nib{ID: "a", BlockedBy: []string{"b1"}}
	r := &fakeResolver{
		blocking:    map[string][]string{"a": {"c1", "c2"}},
		mentions:    map[string][]string{"a": {"m1"}},
		mentionedBy: map[string][]string{"a": {"m2"}},
	}
	sel, _ := ParseFields("blocking,blocked-by,mentions,mentioned-by")
	p, err := Project(n, sel, r)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	got, _ := json.Marshal(p)
	want := `{"blocking":["c1","c2"],"blocked_by":["b1"],"mentions":["m1"],"mentioned_by":["m2"]}`
	if string(got) != want {
		t.Errorf("bare relation JSON = %s, want %s", got, want)
	}
}

// TestProject_EmptyListRelationsSerializeAsArray asserts an empty relation
// serializes to [] (not null), so a consumer can index it unconditionally.
func TestProject_EmptyListRelationsSerializeAsArray(t *testing.T) {
	n := &nib.Nib{ID: "a"}
	r := &fakeResolver{}
	sel, _ := ParseFields("blocked-by,blocking")
	p, err := Project(n, sel, r)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	got, _ := json.Marshal(p)
	want := `{"blocking":[],"blocked_by":[]}`
	if string(got) != want {
		t.Errorf("empty relation JSON = %s, want %s", got, want)
	}
}

// TestProject_ComputedFields covers the computed scalars via the fake accessor.
func TestProject_ComputedFields(t *testing.T) {
	n := &nib.Nib{ID: "a"}
	r := &fakeResolver{
		childCount: map[string]int{"a": 3},
		progress:   map[string]any{"a": map[string]int{"done": 1, "total": 4}},
		ready:      map[string]bool{"a": true},
	}
	sel, _ := ParseFields("children,progress,ready")
	p, err := Project(n, sel, r)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	got, _ := json.Marshal(p)
	want := `{"children":3,"progress":{"done":1,"total":4},"ready":true}`
	if string(got) != want {
		t.Errorf("computed JSON = %s, want %s", got, want)
	}
}

// TestProject_StableMenuOrder asserts that regardless of the -f token order, the
// projected keys (and JSON) come out in canonical menu order — the stable
// ordering the ordered structure exists to guarantee.
func TestProject_StableMenuOrder(t *testing.T) {
	n := &nib.Nib{ID: "a", Title: "T", Status: "todo"}
	sel, err := ParseFields("status,id,title")
	if err != nil {
		t.Fatalf("ParseFields: %v", err)
	}
	p, err := Project(n, sel, nil)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	// Menu order is id, title, status (title precedes status, matching the ref
	// tier) — not the order the caller listed them.
	wantKeys := []string{"id", "title", "status"}
	got := p.Keys()
	if len(got) != len(wantKeys) {
		t.Fatalf("Keys() = %v, want %v", got, wantKeys)
	}
	for i := range wantKeys {
		if got[i] != wantKeys[i] {
			t.Errorf("Keys()[%d] = %q, want %q", i, got[i], wantKeys[i])
		}
	}
	b, _ := json.Marshal(p)
	want := `{"id":"a","title":"T","status":"todo"}`
	if string(b) != want {
		t.Errorf("JSON = %s, want %s (flat object, menu order)", b, want)
	}
}

// TestProject_ScalarValuesAndTypes covers the scalar value types, incl. the
// presentation defaults for type/priority and RFC3339 timestamps.
func TestProject_ScalarValuesAndTypes(t *testing.T) {
	tm := time.Date(2026, 5, 6, 14, 30, 0, 0, time.UTC)
	n := &nib.Nib{
		ID: "a", Slug: "s", Title: "T", Status: "todo",
		Estimate: "m", Parent: "p", Order: "a0", Path: "dir/a.md",
		Tags: []string{"x", "y"}, CreatedAt: &tm, UpdatedAt: &tm,
	}
	sel, _ := ViewFields("full")
	p, err := Project(n, sel, nil)
	if err == nil {
		t.Fatal("full view includes resolver-backed relations; expected resolver-required error with nil resolver")
	}
	_ = p

	// Scalar-only projection works without a resolver.
	sel2, _ := ParseFields("id,type,priority,tags,created_at")
	p2, err := Project(n, sel2, nil)
	if err != nil {
		t.Fatalf("Project scalar-only: %v", err)
	}
	if v, _ := p2.Get("type"); v != nib.DefaultType {
		t.Errorf("type = %v, want default %q", v, nib.DefaultType)
	}
	if v, _ := p2.Get("priority"); v != nib.DefaultPriority {
		t.Errorf("priority = %v, want default %q", v, nib.DefaultPriority)
	}
	got, _ := json.Marshal(p2)
	want := `{"id":"a","type":"task","priority":"normal","tags":["x","y"],"created_at":"2026-05-06T14:30:00Z"}`
	if string(got) != want {
		t.Errorf("scalar JSON = %s, want %s", got, want)
	}
}

// TestProject_ResolverRequired asserts that a computed/relation field with a nil
// resolver errors rather than silently omitting data, while a pure-scalar (and
// bare blocked-by) selection projects fine without one.
func TestProject_ResolverRequired(t *testing.T) {
	n := &nib.Nib{ID: "a", BlockedBy: []string{"b1"}}

	// Scalar + bare blocked-by: no resolver needed.
	sel, _ := ParseFields("id,blocked-by")
	if _, err := Project(n, sel, nil); err != nil {
		t.Errorf("scalar + bare blocked-by with nil resolver should succeed, got %v", err)
	}

	for _, spec := range []string{"children", "progress", "ready", "blocking", "mentions", "mentioned-by", "blocked-by(id)"} {
		s, err := ParseFields(spec)
		if err != nil {
			t.Fatalf("ParseFields(%q): %v", spec, err)
		}
		if _, err := Project(n, s, nil); err == nil {
			t.Errorf("Project(%q) with nil resolver should error", spec)
		}
	}
}

// TestProject_NilNib guards the nil-nib path.
func TestProject_NilNib(t *testing.T) {
	sel, _ := ParseFields("id")
	if _, err := Project(nil, sel, nil); err == nil {
		t.Fatal("Project(nil nib) should error")
	}
}

// TestProject_Exhaustiveness projects a fully-populated nib with every field
// selected and a complete resolver, asserting no field errors and that every
// menu field appears as a key. A future field added to the registry without a
// projection path makes this fire.
func TestProject_Exhaustiveness(t *testing.T) {
	tm := time.Date(2026, 5, 6, 14, 30, 0, 0, time.UTC)
	n := &nib.Nib{
		ID: "a", Slug: "s", Title: "T", Status: "todo", Type: "bug", Priority: "high",
		Estimate: "l", Tags: []string{"t"}, Parent: "p", Order: "a0",
		CreatedAt: &tm, UpdatedAt: &tm, Path: "a.md", Body: "b", BlockedBy: []string{"x"},
	}
	r := &fakeResolver{
		nibs:        map[string]*nib.Nib{"x": {ID: "x"}},
		childCount:  map[string]int{"a": 1},
		progress:    map[string]any{"a": 50},
		ready:       map[string]bool{"a": true},
		blocking:    map[string][]string{"a": {"y"}},
		mentions:    map[string][]string{"a": {"m"}},
		mentionedBy: map[string][]string{"a": {"n"}},
	}
	all := newSelection()
	for _, f := range FieldMenu() {
		all.add(f, nil)
	}
	p, err := Project(n, all, r)
	if err != nil {
		t.Fatalf("Project(all fields): %v", err)
	}
	for _, d := range registry {
		if _, ok := p.Get(d.key()); !ok {
			t.Errorf("projection missing key %q for field %q", d.key(), d.name)
		}
	}
	// The whole thing must still serialize.
	if _, err := json.Marshal(p); err != nil {
		t.Fatalf("Marshal(all): %v", err)
	}
}

func TestTextValue(t *testing.T) {
	tm := time.Date(2026, 5, 6, 14, 30, 0, 0, time.UTC)
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, ""},
		{"string", "hi", "hi"},
		{"empty string", "", ""},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"int", 3, "3"},
		{"string slice", []string{"a", "b"}, "a,b"},
		{"empty slice", []string{}, ""},
		{"time pointer", &tm, "2026-05-06T14:30:00Z"},
		{"nil time pointer", (*time.Time)(nil), ""},
		{"time value", tm, "2026-05-06T14:30:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TextValue(tt.in); got != tt.want {
				t.Errorf("TextValue(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestProject_TextRenderingOrder asserts the ordered structure gives a stable,
// menu-ordered sequence of (key, text) pairs for a later TSV renderer.
func TestProject_TextRenderingOrder(t *testing.T) {
	n := &nib.Nib{ID: "a", Title: "T", Status: "todo", Tags: []string{"x", "y"}}
	sel, _ := ParseFields("tags,title,id,status")
	p, err := Project(n, sel, nil)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var keys, texts []string
	for _, f := range p.Fields() {
		keys = append(keys, f.Key)
		texts = append(texts, TextValue(f.Value))
	}
	wantKeys := []string{"id", "title", "status", "tags"}
	wantTexts := []string{"a", "T", "todo", "x,y"}
	if strings.Join(keys, "|") != strings.Join(wantKeys, "|") {
		t.Errorf("keys = %v, want %v", keys, wantKeys)
	}
	if strings.Join(texts, "|") != strings.Join(wantTexts, "|") {
		t.Errorf("texts = %v, want %v", texts, wantTexts)
	}
}
