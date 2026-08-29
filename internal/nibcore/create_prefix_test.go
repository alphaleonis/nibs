package nibcore

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/testskip"
)

// setupCoreWithStoredConfig is the arrangement every command actually has: the
// config travels INSIDE the store, and the process holds a copy it loaded at
// startup. setupTestCore hands its Core a config that was never written to disk,
// which is the one shape these tests cannot use — the whole question is what
// happens when the file and the copy disagree.
//
// The project config is loaded WITHOUT the user-level layer so the fixture's own
// values are the only ones in play; the re-read under test layers the user config
// the way the CLI does, and every field asserted below is one the project config
// sets, which wins over any user-level value.
func setupCoreWithStoredConfig(t *testing.T, body string) (*Core, string) {
	t.Helper()
	nibsDir := filepath.Join(t.TempDir(), store.DirName)
	layout := store.NewLayout(nibsDir)
	if err := os.MkdirAll(layout.DataDir(), 0755); err != nil {
		t.Fatalf("creating the test store: %v", err)
	}
	if err := os.WriteFile(layout.ConfigPath(), []byte(body), 0o644); err != nil {
		t.Fatalf("writing the store config: %v", err)
	}
	cfg, err := config.LoadFromStore(nibsDir)
	if err != nil {
		t.Fatalf("loading the store config: %v", err)
	}
	core := New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("loading the core: %v", err)
	}
	return core, nibsDir
}

func mustMint(t *testing.T, core *Core, title string) *nib.Nib {
	t.Helper()
	b := &nib.Nib{Title: title, Slug: nib.Slugify(title), Status: "todo"}
	if err := core.Create(b); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if b.ID == "" {
		t.Fatal("Create returned a nib with no id")
	}
	return b
}

// TestCreateFollowsTheStoredIDLengthWhenThePrefixStillAgrees pins the half of
// the re-read that is adopted rather than refused over.
//
// A config that declares a different id LENGTH takes nothing away from Create's
// collision guard: c.nibs is keyed under the same prefix either way, so a draw
// of another length is still checked against the ids it could collide with. So
// the draw follows the store, which is what a fresh process would have loaded.
// The prefix is the field a divergence is refused over — see the test below.
func TestCreateFollowsTheStoredIDLengthWhenThePrefixStillAgrees(t *testing.T) {
	core, nibsDir := setupCoreWithStoredConfig(t, "nibs:\n  prefix: old-\n  id_length: 4\n")

	if got := core.Config().Nibs.Prefix; got != "old-" {
		t.Fatalf("loaded prefix = %q, want old- (the fixture is wrong)", got)
	}

	stored := store.NewLayout(nibsDir).ConfigPath()
	if err := os.WriteFile(stored, []byte("nibs:\n  prefix: old-\n  id_length: 7\n"), 0o644); err != nil {
		t.Fatalf("rewriting the store config: %v", err)
	}

	minted := mustMint(t, core, "Minted After The Rewrite")

	if got := len(strings.TrimPrefix(minted.ID, "old-")); got != 7 {
		t.Errorf("minted id %q has a %d-char body, want the 7 the store declares", minted.ID, got)
	}
	// The file is named from the id, which is what makes a wrong draw permanent.
	if _, err := os.Stat(filepath.Join(nibsDir, filepath.FromSlash(minted.Path))); err != nil {
		t.Errorf("the minted nib's file: %v", err)
	}

	// The re-read is LOCAL to the draw: c.config is read off-lock in ~30 places
	// and handed out raw by Config(), so replacing it would race every one of them.
	if got := core.Config().Nibs.IDLength; got != 4 {
		t.Errorf("Config().Nibs.IDLength = %d after the create, want the untouched 4; the re-read must not swap the shared config", got)
	}
}

// TestCreateKeepsTheLoadedVocabularyWhenTheStoreHasNoConfig pins the absence
// answer. A store need not carry a config.yml — the CLI reads defaults for one
// that has none, and an embedder hands New its vocabulary directly — so there is
// nothing on disk that could have changed under this process. Adopting what Load
// answers for a missing file would silently discard the caller's prefix and mint
// every nib bare, and its id length with it: Load applies the system defaults on
// the way out, so a missing file comes back declaring the DEFAULT length rather
// than none.
//
// Absence is answered by the load itself (config.Config.LoadedFromFile) and not
// by a stat of Create's own. Two stats of one path are two observations, and a
// config.yml unlinked between them — which is all an ordinary `git -C .nibs
// checkout` of the store does — read as absent while the guard had already
// judged it present, and every create in that window refused, naming a
// re-prefix to the empty prefix that never happened.
func TestCreateKeepsTheLoadedVocabularyWhenTheStoreHasNoConfig(t *testing.T) {
	nibsDir := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(store.NewLayout(nibsDir).DataDir(), 0755); err != nil {
		t.Fatalf("creating the test store: %v", err)
	}
	cfg := config.DefaultWithPrefix("emb-")
	cfg.Nibs.IDLength = 7
	core := New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("loading the core: %v", err)
	}

	minted := mustMint(t, core, "Minted With No Stored Config")
	if !strings.HasPrefix(minted.ID, "emb-") {
		t.Errorf("minted id = %q, want the prefix the caller configured (emb-)", minted.ID)
	}
	if got := len(strings.TrimPrefix(minted.ID, "emb-")); got != 7 {
		t.Errorf("minted id %q has a %d-char body, want the 7 the caller configured — a store with no config declares no length either", minted.ID, got)
	}
}

// TestCreateFallsBackAndWarnsWhenTheStoreConfigCannotBeRead pins the third
// answer, the one that is neither "the store declares this" nor "the store
// declares nothing".
//
// A read that fails is evidence of nothing — least of all that the prefix
// changed — while the loaded copy is the vocabulary the rest of this create is
// already validating against. So the draw falls back to it rather than refusing,
// because refusing would trade a misnaming nothing gives reason to expect for a
// hard failure of `nibs new` on a transient read error. It says so, because a
// silent fallback here is indistinguishable from the bug above.
//
// The two rows fail at different depths — one in the parse, one before the file
// is even opened — and both must reach the warning rather than the silent
// absence branch, which only a load that read no file at all may take.
func TestCreateFallsBackAndWarnsWhenTheStoreConfigCannotBeRead(t *testing.T) {
	tests := []struct {
		name   string
		break_ func(t *testing.T, configPath string)
	}{
		{
			name: "a config that parses as nothing",
			break_: func(t *testing.T, configPath string) {
				t.Helper()
				if err := os.WriteFile(configPath, []byte("nibs:\n  prefix: [unterminated\n"), 0o644); err != nil {
					t.Fatalf("corrupting the store config: %v", err)
				}
			},
		},
		{
			// EACCES after a permission change, ELOOP, ENOTDIR, or EIO/ESTALE on
			// a network- or removable-mounted store under a long-running serve.
			// A loop is the one of those a test can produce unprivileged.
			name: "a config that cannot even be stat'd",
			break_: func(t *testing.T, configPath string) {
				t.Helper()
				if err := os.Remove(configPath); err != nil {
					t.Fatalf("clearing the store config: %v", err)
				}
				if err := os.Symlink(configPath, configPath); err != nil {
					testskip.SymlinkUnavailable(t, err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, nibsDir := setupCoreWithStoredConfig(t, "nibs:\n  prefix: old-\n  id_length: 4\n")
			var warn bytes.Buffer
			core.SetWarnWriter(&warn)

			stored := store.NewLayout(nibsDir).ConfigPath()
			tt.break_(t, stored)

			minted := mustMint(t, core, "Minted Over A Broken Config")
			if !strings.HasPrefix(minted.ID, "old-") {
				t.Errorf("minted id = %q, want the loaded prefix (old-) — an unreadable config is not a prefix change", minted.ID)
			}
			if !strings.Contains(warn.String(), stored) {
				t.Errorf("warning = %q, want it to name %s so the fallback is not silent", warn.String(), stored)
			}
		})
	}
}

// dataEntries is what the store's data directory holds, sorted, so a failure
// message can show the second file a blind create left behind.
func dataEntries(t *testing.T, nibsDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(store.NewLayout(nibsDir).DataDir())
	if err != nil {
		t.Fatalf("reading the data directory: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// TestCreateRefusesAStoreThatWasRePrefixedUnderTheLock is the other half of the
// set-prefix race, and the reason the draw cannot simply follow the store.
//
// Create's collision guard reads c.nibs, which is keyed by the ids this process
// loaded — every one of them under the RETIRED prefix once set-prefix has
// renamed the files. Drawing under the prefix the store now declares therefore
// draws from an id space the guard indexes nothing of: every id in the store is
// invisible to it and the 100-draw redraw loop can never fire. A draw that hits
// a renamed nib then writes a second file claiming its id, or — when the slug
// matches too — renames straight over it and destroys it, both silently.
//
// So a create whose store moved under the write lock is refused: this process's
// whole view is retired, not just the prefix (the parent, blocking and anchor
// ids the caller named are all under the old prefix as well), and a rerun reads
// the store as the winner left it.
func TestCreateRefusesAStoreThatWasRePrefixedUnderTheLock(t *testing.T) {
	tests := []struct {
		name string
		slug string
	}{
		{"a slug of its own", "minted-after-the-rename"},
		{"the slug the renamed nib already holds", "root"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, nibsDir := setupCoreWithStoredConfig(t, "nibs:\n  prefix: old-\n  id_length: 4\n")
			existing := &nib.Nib{ID: "old-aaaa", Slug: "root", Title: "Root", Status: "todo",
				Body: "the payload a blind create destroys"}
			if err := core.Create(existing); err != nil {
				t.Fatalf("seeding the store: %v", err)
			}

			// What set-prefix leaves behind, in its order: every nib file renamed,
			// then the config rewritten. Both land while this process waits.
			loaded := filepath.Join(nibsDir, filepath.FromSlash(existing.Path))
			renamed := filepath.Join(filepath.Dir(loaded), "new-aaaa--root.md")
			if err := os.Rename(loaded, renamed); err != nil {
				t.Fatalf("simulating the rename set-prefix made: %v", err)
			}
			before, err := os.ReadFile(renamed)
			if err != nil {
				t.Fatalf("reading the renamed nib: %v", err)
			}
			stored := store.NewLayout(nibsDir).ConfigPath()
			if err := os.WriteFile(stored, []byte("nibs:\n  prefix: new-\n  id_length: 4\n"), 0o644); err != nil {
				t.Fatalf("rewriting the store config: %v", err)
			}

			// The draw the guard cannot see: c.nibs holds old-aaaa, the store holds
			// new-aaaa, and nothing refreshed the index in between.
			swapIDGenerator(t, "new-aaaa")

			b := &nib.Nib{Title: "Minted After The Rename", Slug: tt.slug, Status: "todo"}
			err = core.Create(b)
			if err == nil {
				t.Fatalf("Create succeeded against a store re-prefixed under the lock: minted %q at %q; data/ now holds %v",
					b.ID, b.Path, dataEntries(t, nibsDir))
			}
			var moved *StoreRePrefixedError
			if !errors.As(err, &moved) {
				t.Fatalf("Create error = %v, want a *StoreRePrefixedError the CLI can classify", err)
			}
			if moved.Loaded != "old-" || moved.Declared != "new-" {
				t.Errorf("refusal names loaded %q / declared %q, want old- / new-", moved.Loaded, moved.Declared)
			}

			after, err := os.ReadFile(renamed)
			if err != nil {
				t.Fatalf("reading the renamed nib after the refusal: %v", err)
			}
			if !bytes.Equal(before, after) {
				t.Error("the refused create wrote over the nib the rename left at that name")
			}
			if got := dataEntries(t, nibsDir); len(got) != 1 || got[0] != "new-aaaa--root.md" {
				t.Errorf("data/ holds %v, want only the renamed nib — nothing may claim its id", got)
			}
		})
	}
}

// TestCreateKeepsTheLoadedVocabularyWhenTheConfigDeclaresNoPrefix is the third
// answer the divergence check must not swallow.
//
// A config file that sets no `nibs.prefix` — or sets it empty — declares nothing
// that could have retired an id. `nibs config set-prefix` appends the separator
// dash to its argument and validates the result, so the shortest prefix it can
// write is two characters; nothing it does empties the field. So the loaded
// prefix stands and the create proceeds, while what the file DOES declare (the
// id length) is adopted.
func TestCreateKeepsTheLoadedVocabularyWhenTheConfigDeclaresNoPrefix(t *testing.T) {
	nibsDir := filepath.Join(t.TempDir(), store.DirName)
	layout := store.NewLayout(nibsDir)
	if err := os.MkdirAll(layout.DataDir(), 0755); err != nil {
		t.Fatalf("creating the test store: %v", err)
	}
	if err := os.WriteFile(layout.ConfigPath(), []byte("nibs:\n  id_length: 6\n"), 0o644); err != nil {
		t.Fatalf("writing the store config: %v", err)
	}
	core := New(nibsDir, config.DefaultWithPrefix("emb-"))
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("loading the core: %v", err)
	}

	minted := mustMint(t, core, "Minted Over A Prefixless Config")
	if !strings.HasPrefix(minted.ID, "emb-") {
		t.Errorf("minted id = %q, want the loaded prefix (emb-) — a config that declares no prefix renames nothing", minted.ID)
	}
	if got := len(strings.TrimPrefix(minted.ID, "emb-")); got != 6 {
		t.Errorf("minted id %q has a %d-char body, want the 6 the file does declare", minted.ID, got)
	}
}

// TestCreateTellsALongLivedHolderToRestartRatherThanRerun pins the half of the
// refusal that is addressed to a reader rather than to the store.
//
// c.config is fixed at construction and nothing reloads it — the watcher never
// reads config.yml — so for a process that outlives one command the refusal is
// not transient: every later create fails identically for the rest of its life.
// "Rerun" is the whole repair for `nibs new`, which starts over anyway, and a
// no-op for `nibs serve`, which needs a restart. The message says whichever is
// true of the process it reaches.
func TestCreateTellsALongLivedHolderToRestartRatherThanRerun(t *testing.T) {
	tests := []struct {
		name      string
		longLived bool
		want      string
		notWant   string
	}{
		{name: "a command that exits after this create", want: "rerun", notWant: "restart"},
		{name: "a process holding the store", longLived: true, want: "restart", notWant: "rerun"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, nibsDir := setupCoreWithStoredConfig(t, "nibs:\n  prefix: old-\n  id_length: 4\n")
			if tt.longLived {
				// Asking is what marks the process, so the watch is started and
				// stopped again: nothing may race the rewrite below, and the mark
				// must survive the stop.
				if err := core.StartWatching(); err != nil {
					t.Fatalf("StartWatching: %v", err)
				}
				if err := core.StopWatching(); err != nil {
					t.Fatalf("StopWatching: %v", err)
				}
			}
			stored := store.NewLayout(nibsDir).ConfigPath()
			if err := os.WriteFile(stored, []byte("nibs:\n  prefix: new-\n  id_length: 4\n"), 0o644); err != nil {
				t.Fatalf("rewriting the store config: %v", err)
			}

			err := core.Create(&nib.Nib{Title: "Minted After The Rewrite", Slug: "minted", Status: "todo"})
			var moved *StoreRePrefixedError
			if !errors.As(err, &moved) {
				t.Fatalf("Create error = %v, want a *StoreRePrefixedError", err)
			}
			if moved.LongLived != tt.longLived {
				t.Errorf("refusal reports LongLived = %v, want %v", moved.LongLived, tt.longLived)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("refusal = %q, want it to prescribe %q", err.Error(), tt.want)
			}
			if strings.Contains(err.Error(), tt.notWant) {
				t.Errorf("refusal = %q, must not prescribe %q — it cannot work for this holder", err.Error(), tt.notWant)
			}

			// The refusal is stable, which is the whole reason the remedy has to
			// be true: a second create says the same thing, and for a long-lived
			// holder so does every one after it.
			second := core.Create(&nib.Nib{Title: "Minted Again", Slug: "minted-again", Status: "todo"})
			if second == nil || second.Error() != err.Error() {
				t.Errorf("second Create = %v, want the same refusal as the first", second)
			}
		})
	}
}

// TestCreateRefusesAStoredPrefixThatEscapesTheStore pins the third door onto
// nib.NewID. `nibs init` and `nibs config set-prefix` both put their argument
// through reprefix.ValidatePrefix, but nothing re-checks the value once it is IN
// the config file — mintingVocabulary reads nibs.prefix and hands it straight to
// the generator. A hand-edited (or hand-merged) config carrying a path separator
// therefore poisons every create in the store, with no flag involved.
//
// Create is where all three doors meet, so the refusal lives there and is
// asserted here on the id that actually reaches the filesystem.
func TestCreateRefusesAStoredPrefixThatEscapesTheStore(t *testing.T) {
	core, nibsDir := setupCoreWithStoredConfig(t, "nibs:\n  prefix: ../../pwn-\n  id_length: 4\n")

	// The store is <tmp>/.nibs, and data/../../ resolves to <tmp> — the first
	// directory outside the store a traversal reaches.
	outside := filepath.Dir(nibsDir)
	before, err := os.ReadDir(outside)
	if err != nil {
		t.Fatalf("reading %s: %v", outside, err)
	}

	b := &nib.Nib{Title: "Poisoned", Slug: "poisoned", Status: "todo"}
	if err := core.Create(b); err == nil {
		t.Fatalf("Create() = nil with id %q, want a refusal", b.ID)
	} else if !errors.Is(err, nib.ErrIDNotFilename) {
		t.Fatalf("Create() error = %v, want one wrapping nib.ErrIDNotFilename", err)
	}

	after, err := os.ReadDir(outside)
	if err != nil {
		t.Fatalf("reading %s: %v", outside, err)
	}
	if len(after) != len(before) {
		t.Errorf("a refused create wrote outside the store: %d entries -> %d", len(before), len(after))
	}
	if n := len(core.All()); n != 0 {
		t.Errorf("store holds %d nibs after a refused create, want 0", n)
	}
}

// TestCreateRefusesACallerSuppliedIDThatIsNotAFilename covers the direct
// entrypoint no prefix passes through: a caller that assigns Nib.ID itself. "."
// and ".." carry no separator, so the separator clause never sees them, yet both
// name a directory entry rather than a file.
func TestCreateRefusesACallerSuppliedIDThatIsNotAFilename(t *testing.T) {
	for _, id := range []string{".", "..", "sub/dir-x9z2"} {
		t.Run(id, func(t *testing.T) {
			core, _ := setupCoreWithStoredConfig(t, "nibs:\n  prefix: ok-\n  id_length: 4\n")
			b := &nib.Nib{ID: id, Title: "Direct", Slug: "direct", Status: "todo"}
			if err := core.Create(b); err == nil {
				t.Fatalf("Create(ID=%q) = nil, want a refusal", id)
			} else if !errors.Is(err, nib.ErrIDNotFilename) {
				t.Errorf("Create(ID=%q) error = %v, want one wrapping nib.ErrIDNotFilename", id, err)
			}
		})
	}
}

// reloadedGet answers the only question that matters about an accepted create:
// does the id come back from a store read off disk? Resolving it in the Core
// that minted it proves nothing — the id is in that map either way, and the
// whole defect is that the FILE decodes to something else.
func reloadedGet(t *testing.T, nibsDir, id string) (*nib.Nib, error) {
	t.Helper()
	cfg, err := config.LoadFromStore(nibsDir)
	if err != nil {
		t.Fatalf("re-loading the store config: %v", err)
	}
	fresh := New(nibsDir, cfg)
	fresh.SetWarnWriter(nil)
	if err := fresh.Load(); err != nil {
		t.Fatalf("re-loading the store: %v", err)
	}
	return fresh.Get(id)
}

// TestCreateRefusesAStoredPrefixThatCollidesWithTheSlugSeparator is the
// round-trip sibling of the traversal test above, through the same door: a
// hand-edited config.yml. `nibs init` and `nibs config set-prefix` both put
// their argument through reprefix.ValidatePrefix, and nothing re-reads the value
// once it is in the file — mintingVocabulary hands nibs.prefix straight to the
// generator.
//
// "a--b-" is the shape the pattern alone lets through: lowercase alphanumerics
// and dashes, ending in a dash. BuildFilename then joins the id and the slug
// with the very separator the prefix already carries, so ParseFilename splits
// inside the id on the next load and every nib in the store answers to "a".
func TestCreateRefusesAStoredPrefixThatCollidesWithTheSlugSeparator(t *testing.T) {
	core, nibsDir := setupCoreWithStoredConfig(t, "nibs:\n  prefix: a--b-\n  id_length: 4\n")

	before := dataEntries(t, nibsDir)

	b := &nib.Nib{Title: "Round Trip Probe", Slug: nib.Slugify("Round Trip Probe"), Status: "todo"}
	if err := core.Create(b); err == nil {
		t.Fatalf("Create() = nil with id %q, want a refusal", b.ID)
	} else if !errors.Is(err, nib.ErrIDNotRoundTrip) {
		t.Fatalf("Create() error = %v, want one wrapping nib.ErrIDNotRoundTrip", err)
	}

	if after := dataEntries(t, nibsDir); len(after) != len(before) {
		t.Errorf("a refused create wrote to data/: %v -> %v", before, after)
	}
	if n := len(core.All()); n != 0 {
		t.Errorf("store holds %d nibs after a refused create, want 0", n)
	}
}

// TestCreateRefusesACallerSuppliedIDThatDoesNotReadBack covers the door the
// stored prefix does not: `nibs new --prefix <p>` pre-composes the id from its
// flag in the CreateNib resolver and assigns it to Nib.ID, so mintingVocabulary
// — and with it every check that hangs off the store's declared prefix — is
// never reached. Setting b.ID directly is that path.
//
// The accepted rows are as load-bearing as the refused ones. A foreign prefix is
// a supported feature, and a dotted id is legal in a file name; what decides
// each case is only where ParseFilename splits, so the rule has to be shown
// letting through the shapes that survive it.
func TestCreateRefusesACallerSuppliedIDThatDoesNotReadBack(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		slug    string
		wantErr bool
	}{
		// The store below declares "tnib-", so every id here is foreign to it —
		// which is exactly what --prefix produces.
		{name: "double dash with a slug", id: "a--b-hmv7", slug: "rt-probe", wantErr: true},
		{name: "double dash slugless", id: "a--b-hmv7", wantErr: true},
		// A dotted prefix parts along the two branches ParseFilename tries in
		// order. With a slug the "--" BuildFilename writes comes first and wins,
		// so the id survives; with no slug the dot is the only separator in the
		// name and takes it apart at "c". Both rows are the same id — the slug
		// is the whole difference.
		{name: "dotted prefix slugless", id: "c.d-h1wy", wantErr: true},
		{name: "dotted prefix with a slug", id: "a.b-9k3y", slug: "dot-probe"},
		// A prefix the store does not declare carries no recognized prefix into
		// ParseFilename, so the prefix-aware branch never fires and the legacy
		// single-dash split takes the name apart at the prefix's own trailing
		// dash. A slug moves the split back ahead of it.
		{name: "foreign prefix slugless", id: "zz-924q", wantErr: true},
		{name: "foreign prefix with a slug", id: "zz-924q", slug: "has-slug"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, nibsDir := setupCoreWithStoredConfig(t, "nibs:\n  prefix: tnib-\n  id_length: 4\n")
			before := dataEntries(t, nibsDir)

			b := &nib.Nib{ID: tt.id, Title: "Prefixed", Slug: tt.slug, Status: "todo"}
			err := core.Create(b)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Create(ID=%q, Slug=%q) = nil, want a refusal", tt.id, tt.slug)
				}
				if !errors.Is(err, nib.ErrIDNotRoundTrip) {
					t.Fatalf("Create(ID=%q, Slug=%q) error = %v, want one wrapping nib.ErrIDNotRoundTrip", tt.id, tt.slug, err)
				}
				if after := dataEntries(t, nibsDir); len(after) != len(before) {
					t.Errorf("a refused create wrote to data/: %v -> %v", before, after)
				}
				if n := len(core.All()); n != 0 {
					t.Errorf("store holds %d nibs after a refused create, want 0", n)
				}
				return
			}

			if err != nil {
				t.Fatalf("Create(ID=%q, Slug=%q) = %v, want it accepted", tt.id, tt.slug, err)
			}
			// Read back from disk, not from the Core that wrote it: the id lives
			// in the file name and nowhere else, so a fresh load is the only
			// thing that can tell whether it survived.
			got, err := reloadedGet(t, nibsDir, tt.id)
			if err != nil {
				t.Fatalf("re-loaded store: Get(%q) = %v, want the nib the create returned (data/ holds %v)", tt.id, err, dataEntries(t, nibsDir))
			}
			if got.ID != tt.id {
				t.Errorf("re-loaded store: Get(%q).ID = %q, want %q", tt.id, got.ID, tt.id)
			}
		})
	}
}
