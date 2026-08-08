package nibcore

import (
	"fmt"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

func TestSearch_IDMatch_ShortIDExact(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)
	defer func() { _ = core.Close() }()

	nibs := []*nib.Nib{
		{ID: "nibs-5a8k", Title: "Fix login flow", Body: "Details about login"},
		{ID: "nibs-zz99", Title: "Other work", Body: "Unrelated content"},
	}
	for _, b := range nibs {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	results, err := core.Search("5a8k")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != "nibs-5a8k" {
		t.Errorf("Search(5a8k) = %v, want [nibs-5a8k]", rawIDList(results))
	}
}

func TestSearch_IDMatch_FullIDExact(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)
	defer func() { _ = core.Close() }()

	nibs := []*nib.Nib{
		{ID: "nibs-5a8k", Title: "Fix login flow", Body: "Details about login"},
		{ID: "nibs-zz99", Title: "Other work", Body: "Unrelated content"},
	}
	for _, b := range nibs {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	results, err := core.Search("nibs-5a8k")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != "nibs-5a8k" {
		t.Errorf("Search(nibs-5a8k) = %v, want [nibs-5a8k]", rawIDList(results))
	}
}

func TestSearch_IDMatch_BarePrefixDoesNotMatchAll(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)
	defer func() { _ = core.Close() }()

	nibs := []*nib.Nib{
		{ID: "nibs-5a8k", Title: "Fix login flow", Body: "Details about login"},
		{ID: "nibs-zz99", Title: "Other work", Body: "Unrelated content"},
	}
	for _, b := range nibs {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	for _, query := range []string{"nibs", "nibs-"} {
		results, err := core.Search(query)
		if err != nil {
			t.Fatalf("Search(%q) error = %v", query, err)
		}
		if len(results) != 0 {
			t.Errorf("Search(%q) = %v, want []", query, rawIDList(results))
		}
	}
}

func TestSearch_IDMatch_UnionWithTextHits(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)
	defer func() { _ = core.Close() }()

	// nibs-5a8k matches the query both by ID and by body text; the others
	// match by body text only.
	nibs := []*nib.Nib{
		{ID: "nibs-5a8k", Title: "Fix login flow", Body: "also mentions 5a8k here"},
		{ID: "nibs-bb22", Title: "Other work", Body: "one mention of 5a8k in a longer sentence body"},
		{ID: "nibs-cc33", Title: "More work", Body: "5a8k 5a8k 5a8k"},
	}
	for _, b := range nibs {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	results, err := core.Search("5a8k")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	got := rawIDList(results)
	if len(got) != 3 {
		t.Fatalf("Search(5a8k) = %v, want 3 results", got)
	}
	// The ID match leading is code-owned ordering; the text hits follow in
	// Bleve relevance order, whose inter-document ranking is not pinned here
	// (it may shift across Bleve upgrades).
	if got[0] != "nibs-5a8k" {
		t.Fatalf("Search(5a8k) first result = %s, want nibs-5a8k (got %v)", got[0], got)
	}
	rest := map[string]bool{got[1]: true, got[2]: true}
	if !rest["nibs-bb22"] || !rest["nibs-cc33"] {
		t.Fatalf("Search(5a8k) text hits = %v, want nibs-bb22 and nibs-cc33", got[1:])
	}
}

func TestSearch_IDMatch_MultiWordQuerySkipsIDMatching(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)
	defer func() { _ = core.Close() }()

	nibs := []*nib.Nib{
		{ID: "nibs-5a8k", Title: "Fix parser", Body: "Tokenizer details"},
		{ID: "nibs-bb22", Title: "Login page", Body: "Implement login form"},
	}
	for _, b := range nibs {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	// "5a8k" is an ID fragment, but multi-word queries are Bleve query-string
	// syntax and must not trigger ID matching.
	results, err := core.Search("login 5a8k")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != "nibs-bb22" {
		t.Errorf("Search(login 5a8k) = %v, want [nibs-bb22]", rawIDList(results))
	}
}

func TestSearch_IDMatch_UppercaseQuery(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)
	defer func() { _ = core.Close() }()

	nibs := []*nib.Nib{
		{ID: "nibs-5a8k", Title: "Fix login flow", Body: "Details about login"},
		{ID: "nibs-zz99", Title: "Other work", Body: "Unrelated content"},
	}
	for _, b := range nibs {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	// Pins case-insensitivity through the production Search path, not just
	// the matchesIDQuery test seam.
	results, err := core.Search("5A8K")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != "nibs-5a8k" {
		t.Errorf("Search(5A8K) = %v, want [nibs-5a8k]", rawIDList(results))
	}
}

func TestSearch_IDMatch_TrailingWhitespaceTrimmed(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)
	defer func() { _ = core.Close() }()

	nibs := []*nib.Nib{
		{ID: "nibs-5a8k", Title: "Fix login flow", Body: "Details about login"},
		{ID: "nibs-zz99", Title: "Other work", Body: "Unrelated content"},
	}
	for _, b := range nibs {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	// Pastes commonly carry a trailing space; it must not disable ID matching.
	results, err := core.Search("5a8k ")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != "nibs-5a8k" {
		t.Errorf("Search(%q) = %v, want [nibs-5a8k]", "5a8k ", rawIDList(results))
	}
}

func TestIDMatchesLocked_CappedAtDefaultSearchLimit(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)
	defer func() { _ = core.Close() }()

	// Populate the in-memory map directly; idMatchesLocked never touches the
	// index or disk, and Create-ing 1000+ files here would be pure overhead.
	// Writing without core.mu is safe only because this test is
	// single-goroutine and the watcher is never started.
	for i := range DefaultSearchLimit + 5 {
		id := fmt.Sprintf("nibs-aa%04d", i)
		core.nibs[id] = &nib.Nib{ID: id}
	}

	core.mu.RLock()
	matches := core.idMatchesLocked("aa", DefaultSearchLimit)
	uncapped := core.idMatchesLocked("aa", 0)
	core.mu.RUnlock()

	if len(matches) != DefaultSearchLimit {
		t.Fatalf("idMatchesLocked() returned %d matches, want %d", len(matches), DefaultSearchLimit)
	}
	// Capped after sorting: the kept set is the DefaultSearchLimit smallest IDs.
	wantLast := fmt.Sprintf("nibs-aa%04d", DefaultSearchLimit-1)
	if got := matches[len(matches)-1].ID; got != wantLast {
		t.Errorf("last kept match = %s, want %s", got, wantLast)
	}

	// limit <= 0 means no cap — the bound SearchAll asks for, so that an
	// intersection against an already-bounded working set is not fed a
	// store-wide top-N.
	if len(uncapped) != DefaultSearchLimit+5 {
		t.Errorf("idMatchesLocked(limit 0) returned %d matches, want all %d", len(uncapped), DefaultSearchLimit+5)
	}
}

func TestIDMatchesLocked_ForeignPrefixIDs(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)
	defer func() { _ = core.Close() }()

	// IDs come from filenames unvalidated: a foreign-prefix nib like task-42
	// (from an id--slug filename such as task-42--fix.md, or a hand-authored
	// file) can coexist under the nibs- prefix, its short ID keeping the
	// hyphen. Legacy single-hyphen names don't produce it — nib.ParseFilename
	// splits task-42.md at the first hyphen into id "task". Populate the map
	// directly like the cap test; writing without core.mu is safe only
	// because this test is single-goroutine and the watcher is never started.
	core.nibs["task-42"] = &nib.Nib{ID: "task-42"}
	core.nibs["nibs-5a8k"] = &nib.Nib{ID: "nibs-5a8k"}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		// A Bleve negation query must never be treated as an ID fragment,
		// even though the hyphenated foreign short ID contains it.
		{"bleve negation excluded by charset gate", "-42", nil},
		// Charset-clean queries deliberately substring-match foreign short
		// IDs: fragment lookup of legacy nibs is desirable.
		{"charset-clean word matches foreign short id", "task", []string{"task-42"}},
		{"charset-clean digits match foreign short id", "42", []string{"task-42"}},
		// A foreign full ID must match by exact equality even though its
		// hyphen fails the fragment charset gate.
		{"foreign full id exact", "task-42", []string{"task-42"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core.mu.RLock()
			matches := core.idMatchesLocked(tt.query, DefaultSearchLimit)
			core.mu.RUnlock()

			got := rawIDList(matches)
			if len(got) != len(tt.want) {
				t.Fatalf("idMatchesLocked(%q) = %v, want %v", tt.query, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("idMatchesLocked(%q) = %v, want %v", tt.query, got, tt.want)
				}
			}
		})
	}
}

func TestMatchesIDQuery(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		id     string
		prefix string
		want   bool
	}{
		{"short id substring at start", "5a8", "nibs-5a8k", "nibs-", true},
		{"short id substring at end", "a8k", "nibs-5a8k", "nibs-", true},
		{"short id exact", "5a8k", "nibs-5a8k", "nibs-", true},
		{"short id non-substring", "9q7", "nibs-5a8k", "nibs-", false},
		{"one-char fragment below min length", "a", "nibs-5a8k", "nibs-", false},
		{"two-char fragment meets min length", "5a", "nibs-5a8k", "nibs-", true},
		{"one-char fragment below min length empty prefix", "a", "5a8k", "", false},
		{"full form one-char remainder unaffected by min length", "nibs-5", "nibs-5a8k", "nibs-", true},
		{"full form exact", "nibs-5a8k", "nibs-5a8k", "nibs-", true},
		{"full form prefix", "nibs-5a", "nibs-5a8k", "nibs-", true},
		{"full form non-prefix fragment", "nibs-a8k", "nibs-5a8k", "nibs-", false},
		{"bare prefix without dash", "nibs", "nibs-5a8k", "nibs-", false},
		{"bare prefix with dash", "nibs-", "nibs-5a8k", "nibs-", false},
		{"empty query", "", "nibs-5a8k", "nibs-", false},
		{"empty prefix substring", "5a8", "5a8k", "", true},
		{"empty prefix non-substring", "9q7", "5a8k", "", false},
		{"short id uppercase query", "5A8K", "nibs-5a8k", "nibs-", true},
		{"full form uppercase query", "NIBS-5A8K", "nibs-5a8k", "nibs-", true},
		{"multi-word query skips id matching", "user AND login", "nibs-5a8k", "nibs-", false},
		{"id fragment with trailing space is trimmed", "5a8k ", "nibs-5a8k", "nibs-", true},
		{"full form with surrounding whitespace is trimmed", " nibs-5a8k ", "nibs-5a8k", "nibs-", true},
		{"full form with extra word", "nibs-5a8k extra", "nibs-5a8k", "nibs-", false},
		{"bleve negation does not match hyphenated foreign id", "-42", "task-42", "nibs-", false},
		{"charset-clean query matches foreign short id", "task", "task-42", "nibs-", true},
		{"foreign full id exact", "task-42", "task-42", "nibs-", true},
		{"empty prefix hyphenated full id exact", "task-42", "task-42", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesIDQuery(tt.query, tt.id, tt.prefix); got != tt.want {
				t.Errorf("matchesIDQuery(%q, %q, %q) = %v, want %v", tt.query, tt.id, tt.prefix, got, tt.want)
			}
		})
	}
}
