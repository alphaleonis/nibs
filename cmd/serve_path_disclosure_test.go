package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/testskip"
)

// storeSentinelSegment names the directory the fixture store is built under. It
// is distinctive so the assertions can look for the PROJECT directory on its
// own, not only for a whole absolute path: a message naming the project rather
// than the store, or naming half a path, is the same disclosure.
const storeSentinelSegment = "ue2s-disclosure-sentinel"

// lockDirSentinelSegment names the directory the fixture points the OS temp dir
// at. The lock file every mutation opens goes there — outside the store and
// outside the project, which is why it needs coverage of its own — and a
// sentinel name is what lets the assertions look for it the same way.
const lockDirSentinelSegment = "ue2s-lockdir-sentinel"

// servedMutation is one mutation field driven over real HTTP. field is the
// schema's own name for it, which is what the completeness subtest compares
// against the executable schema.
type servedMutation struct {
	field string
	doc   string
}

// servedMutations drives every field of the schema's Mutation type. The set is
// checked against the executable schema at runtime (see the "covers every
// mutation" subtest), so an added or renamed mutation fails rather than
// silently going unprobed.
//
// Config is selected as `prefix`, never `projectName`: a store's project name is
// its parent directory's name (config.Config.GetProjectName), which is the
// directory this guard plants its sentinel in — a field whose correct answer can
// be the needle is not one to select in a guard about absence.
var servedMutations = []servedMutation{
	{"createNib", `mutation { createNib(input: {title: "Added"}) { id } }`},
	{"updateNib", `mutation { updateNib(id: "t1", input: {title: "Changed"}) { id } }`},
	{"setParent", `mutation { setParent(id: "t1", parentId: "ep1") { id } }`},
	{"addBlocking", `mutation { addBlocking(id: "t1", targetId: "t2") { id } }`},
	{"removeBlocking", `mutation { removeBlocking(id: "t1", targetId: "t2") { id } }`},
	{"addBlockedBy", `mutation { addBlockedBy(id: "t1", targetId: "t3") { id } }`},
	{"removeBlockedBy", `mutation { removeBlockedBy(id: "t1", targetId: "t3") { id } }`},
	{"reorderNib", `mutation { reorderNib(id: "c2", first: true) { id } }`},
	{"reorderChildren", `mutation { reorderChildren(parentId: "ep1", childIds: ["c2", "c1"]) { id } }`},
	{"reorderSiblings", `mutation { reorderSiblings(siblingIds: ["c1", "c2"], first: true) { id } }`},
	{"renameArea", `mutation { renameArea(input: {path: "web", newName: "frontend"}) { prefix } }`},
	{"removeArea", `mutation { removeArea(input: {path: "web", unassign: true}) { prefix } }`},
	{"archiveNib", `mutation { archiveNib(id: "t3") }`},
	{"deleteNib", `mutation { deleteNib(id: "t2") }`},
}

// disclosureFixture is a served store plus every spelling of its own location
// that must never reach a client.
type disclosureFixture struct {
	server  *httptest.Server
	store   string
	project string
	lockDir string
	needles []pathNeedle
}

type pathNeedle struct {
	what string
	text string
}

// newDisclosureFixture builds a store under a sentinel-named project directory,
// serves it through the real GraphQL handler, and records the needles. The
// needles are computed BEFORE any breakage, since resolving a path this test is
// about to make unreadable would fail.
func newDisclosureFixture(t *testing.T) *disclosureFixture {
	t.Helper()

	project := filepath.Join(t.TempDir(), storeSentinelSegment)
	lockDir := filepath.Join(t.TempDir(), lockDirSentinelSegment)
	nibsDir := filepath.Join(project, ".nibs")
	mkdirAllT(t, storeDataDir(nibsDir))
	mkdirAllT(t, lockDir)
	writeFileT(t, filepath.Join(nibsDir, "areas.yml"),
		"areas:\n    - name: web\n      children:\n        - name: dashboard\n")

	// Redirect the OS temp directory before the Core derives its lock path from
	// it. TMPDIR is what os.TempDir reads on unix, TMP and TEMP what GetTempPath
	// reads on Windows; the mismatch check below is what says the redirect took.
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, lockDir)
	}

	core := nibcore.New(nibsDir, config.Default())
	if got := core.LockDir(); filepath.Clean(got) != filepath.Clean(lockDir) {
		t.Fatalf("the store locks in %s, not the redirected %s, so this fixture's lock needles prove nothing", got, lockDir)
	}
	if err := core.Load(); err != nil {
		t.Fatalf("load core: %v", err)
	}
	t.Cleanup(func() { _ = core.Close() })

	for _, n := range []*nib.Nib{
		{ID: "ep1", Slug: "epic", Title: "Epic", Type: "epic", Status: "todo"},
		{ID: "c1", Slug: "one", Title: "One", Type: "task", Status: "todo", Parent: "ep1"},
		{ID: "c2", Slug: "two", Title: "Two", Type: "task", Status: "todo", Parent: "ep1"},
		{ID: "t1", Slug: "member", Title: "Member", Type: "task", Status: "todo", Area: "web"},
		{ID: "t2", Slug: "target", Title: "Target", Type: "task", Status: "todo"},
		{ID: "t3", Slug: "other", Title: "Other", Type: "task", Status: "todo"},
	} {
		if err := core.Create(n); err != nil {
			t.Fatalf("create %s: %v", n.ID, err)
		}
	}

	server := httptest.NewServer(newGraphQLHandler(&App{Core: core}, wsTestInterval))
	t.Cleanup(server.Close)

	f := &disclosureFixture{server: server, store: nibsDir, project: project, lockDir: lockDir}
	f.needles = append(f.needles, pathNeedle{"the store directory", nibsDir})
	f.needles = append(f.needles, pathNeedle{"the project directory", project})
	f.needles = append(f.needles, pathNeedle{"the project directory's name", storeSentinelSegment})
	f.needles = append(f.needles, pathNeedle{"the lock directory", lockDir})
	f.needles = append(f.needles, pathNeedle{"the lock directory's name", lockDirSentinelSegment})
	f.needles = append(f.needles, spellingsOf(t, "the store directory", nibsDir)...)
	f.needles = append(f.needles, spellingsOf(t, "the project directory", project)...)
	f.needles = append(f.needles, spellingsOf(t, "the lock directory", lockDir)...)
	return f
}

// spellingsOf returns the other ways p can appear in a JSON response body: the
// resolved form (macOS resolves /var, a Windows runner resolves an 8.3 alias),
// the forward-slash form that Path-shaped values are normalized to, and the
// backslash-escaped form JSON encoding produces on Windows.
func spellingsOf(t *testing.T, what, p string) []pathNeedle {
	t.Helper()
	var out []pathNeedle
	seen := map[string]bool{p: true}
	add := func(label, s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, pathNeedle{what + " (" + label + ")", s})
	}
	add("forward slashes", filepath.ToSlash(p))
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		add("resolved", resolved)
		add("resolved, forward slashes", filepath.ToSlash(resolved))
	}
	for _, n := range slices.Clone(out) {
		add("JSON-escaped", jsonString(t, n.text))
	}
	add("JSON-escaped", jsonString(t, p))
	return out
}

// jsonString renders s the way it appears inside a JSON document, without the
// surrounding quotes — a Windows path's separators double.
func jsonString(t *testing.T, s string) string {
	t.Helper()
	encoded, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("encode %q: %v", s, err)
	}
	quoted := string(encoded[1 : len(encoded)-1])
	if quoted == s {
		return ""
	}
	return quoted
}

// post sends one mutation and returns the raw response body. The assertions are
// over these BYTES rather than over a decoded message, so anything that carries
// a path — an extension, a response-level field, a value this test does not
// model — is covered too.
func (f *disclosureFixture) post(t *testing.T, doc string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": doc})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	resp, err := http.Post(f.server.URL+"/graphql", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return raw
}

// TestServedMutationsDiscloseNoStorePath is the runtime guard behind
// newStorePathScrubber's canonical scope: a served refusal names none of the
// directories listed there, in any spelling.
//
// `nibs serve` answers an unauthenticated client, so an absolute path in a
// response discloses the operating-system username and the project layout. A
// path reaches a message two independent ways, and a fix aimed at either one
// alone ships green: the operating system embeds one in an *fs.PathError, and
// nibs' own wrappers interpolate one with %s before any OS error exists — the
// malformed and not-a-regular-file rows below carry no OS error at all.
//
// It asserts over the HTTP response body, which is the only place a
// presenter-level scrub is observable; a resolver-level assertion cannot see one.
func TestServedMutationsDiscloseNoStorePath(t *testing.T) {
	tests := []struct {
		name string
		// damage breaks the served store after it has loaded. It may skip.
		damage func(t *testing.T, f *disclosureFixture)
		// mustFail names the mutations this breakage has to make fail. Without
		// it a row that stopped biting would keep passing on empty evidence.
		mustFail []string
	}{
		{
			// No OS error is involved: config.LoadAreas interpolates the path
			// itself when yaml.Unmarshal refuses the content.
			name: "the areas vocabulary is malformed",
			damage: func(t *testing.T, f *disclosureFixture) {
				writeFileT(t, filepath.Join(f.store, "areas.yml"), "areas:\n  - name: [unclosed\n")
			},
			mustFail: []string{"renameArea", "removeArea"},
		},
		{
			// Also no OS error: config.ReadConfigFile stats the path, sees a
			// directory, and words its own refusal around it.
			name: "the areas vocabulary is a directory",
			damage: func(t *testing.T, f *disclosureFixture) {
				path := filepath.Join(f.store, "areas.yml")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				mkdirAllT(t, path)
			},
			mustFail: []string{"renameArea", "removeArea"},
		},
		{
			name: "the areas vocabulary is unreadable",
			damage: func(t *testing.T, f *disclosureFixture) {
				makeUnreadable(t, filepath.Join(f.store, "areas.yml"))
			},
			mustFail: []string{"renameArea", "removeArea"},
		},
		{
			name: "the data directory is unreadable",
			damage: func(t *testing.T, f *disclosureFixture) {
				makeUnreadable(t, storeDataDir(f.store))
			},
			mustFail: []string{"renameArea", "removeArea"},
		},
		{
			name: "the data directory is unwritable",
			damage: func(t *testing.T, f *disclosureFixture) {
				unwritableDataDirT(t, f.store)
			},
			mustFail: []string{"createNib"},
		},
		{
			// Every mutation opens the cross-process write lock before it
			// touches the store, and nibcore puts that file in the OS temp
			// directory — which is neither the store nor the project, so it is
			// covered separately. Removing the directory is the portable way to
			// make that open fail; the message it renders carries the whole
			// path.
			name: "the directory holding the write lock is gone",
			damage: func(t *testing.T, f *disclosureFixture) {
				if err := os.RemoveAll(f.lockDir); err != nil {
					t.Fatalf("remove the lock directory: %v", err)
				}
			},
			mustFail: []string{"createNib", "renameArea", "deleteNib"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newDisclosureFixture(t)
			tc.damage(t, f)

			failed := map[string]bool{}
			for _, m := range servedMutations {
				raw := f.post(t, m.doc)
				if hasGraphQLErrors(t, raw) {
					failed[m.field] = true
				}
				for _, n := range f.needles {
					if !bytes.Contains(raw, []byte(n.text)) {
						continue
					}
					t.Errorf("%s named %s (%s) in its response:\n%s",
						m.field, n.what, n.text, raw)
				}
			}
			for _, field := range tc.mustFail {
				if !failed[field] {
					t.Errorf("%s succeeded, so this breakage no longer reaches the code this row is about", field)
				}
			}
		})
	}

	// The completeness half reads the executable schema at RUNTIME, so a mutation
	// added or renamed in the SDL fails here instead of going unprobed. A
	// hand-kept list of the fields would detect no omission at all.
	t.Run("covers every mutation", func(t *testing.T) {
		covered := map[string]bool{}
		for _, m := range servedMutations {
			covered[m.field] = true
		}
		var missing []string
		for _, field := range graph.NewExecutableSchema(graph.Config{}).Schema().Mutation.Fields {
			if !covered[field.Name] {
				missing = append(missing, field.Name)
			}
		}
		sort.Strings(missing)
		if len(missing) != 0 {
			t.Errorf("the schema serves mutations this guard never drives: %s", strings.Join(missing, ", "))
		}
	})
}

// hasGraphQLErrors reports whether the response carries a GraphQL error.
func hasGraphQLErrors(t *testing.T, raw []byte) bool {
	t.Helper()
	var decoded struct {
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode response %s: %v", raw, err)
	}
	return len(decoded.Errors) != 0
}

// makeUnreadable chmods path to 0 and confirms the mode bites, skipping where it
// does not (Windows stores no mode; root ignores one). The original mode is put
// back on cleanup, since t.TempDir's own removal has to traverse it.
func makeUnreadable(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the fixture path this row damages is not there: %v", err)
	}
	if err := os.Chmod(path, 0); err != nil {
		testskip.Unavailable(t, testskip.UnreadablePaths, "os.Chmod(%s, 0): %v", filepath.Base(path), err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, info.Mode().Perm()) })
	if f, err := os.Open(path); err == nil {
		_ = f.Close()
		testskip.Unavailable(t, testskip.UnreadablePaths,
			"this process reads a mode-000 path anyway (running as root?)")
	}
}
