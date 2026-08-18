package nibcore

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
)

// TestLoadSkipsUnparseableFiles covers duplicate-key resilience: yaml.v3 hard-errors
// on a duplicate front-matter key (both a modeled key and an unmodeled/Extra
// key), where yaml.v2 silently took last-wins. loadFromDisk must NOT abort the
// whole WalkDir on one such file — a single pre-existing malformed nib must
// degrade to one missing nib (log-and-skip), never a dead store where every
// valid nib is unreachable.
func TestLoadSkipsUnparseableFiles(t *testing.T) {
	nibsDir := setupNibsDir(t)

	// Two valid nibs that must survive the load.
	const goodA = `---
version: 1
title: Good A
status: todo
type: task
priority: normal
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---

Body A.
`
	const goodB = `---
version: 1
title: Good B
status: todo
type: task
priority: normal
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---

Body B.
`
	// Malformed: duplicate MODELED key (title twice). yaml.v3 rejects this.
	const dupModeled = `---
version: 1
title: First Title
title: Second Title
status: todo
---

Body dup-modeled.
`
	// Malformed: duplicate UNMODELED/Extra key (assignee twice). yaml.v3 rejects
	// this too, via the inline Extra catch-all.
	const dupExtra = `---
version: 1
title: Dup Extra
status: todo
assignee: alice
assignee: bob
---

Body dup-extra.
`
	writeNibFile(t, storeData(t, nibsDir), "gooda1--good.md", goodA)
	writeNibFile(t, storeData(t, nibsDir), "goodb1--good.md", goodB)
	writeNibFile(t, storeData(t, nibsDir), "dupmod1--broken.md", dupModeled)
	writeNibFile(t, storeData(t, nibsDir), "dupext1--broken.md", dupExtra)

	var warnBuf bytes.Buffer
	core := New(nibsDir, config.Default())
	core.SetWarnWriter(&warnBuf)

	// Load must SUCCEED despite the two malformed files.
	if err := core.Load(); err != nil {
		t.Fatalf("Load() returned an error; one bad file must not brick the whole store: %v", err)
	}

	// The two valid nibs must be present.
	if _, err := core.Get("gooda1"); err != nil {
		t.Errorf("valid nib gooda1 missing after load: %v", err)
	}
	if _, err := core.Get("goodb1"); err != nil {
		t.Errorf("valid nib goodb1 missing after load: %v", err)
	}

	// The malformed nibs must be absent (skipped), not present.
	if _, err := core.Get("dupmod1"); err == nil {
		t.Errorf("malformed nib dupmod1 was loaded; it should have been skipped")
	}
	if _, err := core.Get("dupext1"); err == nil {
		t.Errorf("malformed nib dupext1 was loaded; it should have been skipped")
	}

	// A warning must have been emitted naming each skipped file.
	warnings := warnBuf.String()
	if !strings.Contains(warnings, "dupmod1--broken.md") {
		t.Errorf("no warning naming the skipped duplicate-modeled-key file:\n%s", warnings)
	}
	if !strings.Contains(warnings, "dupext1--broken.md") {
		t.Errorf("no warning naming the skipped duplicate-Extra-key file:\n%s", warnings)
	}

	// The skipped files' bytes must be left untouched on disk (skip = not loaded,
	// never delete/rewrite).
	gotMod, err := os.ReadFile(dataPath(nibsDir, "dupmod1--broken.md"))
	if err != nil {
		t.Fatalf("reading skipped file: %v", err)
	}
	if string(gotMod) != dupModeled {
		t.Errorf("skipped duplicate-modeled-key file was modified on disk:\n got:\n%s\nwant:\n%s", gotMod, dupModeled)
	}
	gotExt, err := os.ReadFile(dataPath(nibsDir, "dupext1--broken.md"))
	if err != nil {
		t.Fatalf("reading skipped file: %v", err)
	}
	if string(gotExt) != dupExtra {
		t.Errorf("skipped duplicate-Extra-key file was modified on disk:\n got:\n%s\nwant:\n%s", gotExt, dupExtra)
	}
}

// TestLoadSkipsManyKeyFrontMatterDoS covers the key-count DoS: a crafted
// front-matter block with an excessive key count parses successfully but is
// O(N²) in yaml.v3, so nib.Parse caps it with a normal error. loadFromDisk
// must log-and-skip that file (fast) rather than hang, while still loading the
// valid nibs beside it. Generation is bounded (a few thousand short keys) so the
// test's own footprint stays tiny; nib.Parse rejects it before the quadratic
// decode.
func TestLoadSkipsManyKeyFrontMatterDoS(t *testing.T) {
	nibsDir := setupNibsDir(t)

	const good = `---
version: 1
title: Good
status: todo
type: task
priority: normal
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---

Body.
`
	// Build a many-key front-matter block: well above nib.Parse's key-count bound
	// but with short keys, so it stays small on disk yet triggers the cap.
	var sb strings.Builder
	sb.WriteString("---\nversion: 1\ntitle: Many Keys\nstatus: todo\n")
	for i := 0; i < 6000; i++ {
		fmt.Fprintf(&sb, "k%06d: v\n", i)
	}
	sb.WriteString("---\n\nBody.\n")

	writeNibFile(t, storeData(t, nibsDir), "good1--ok.md", good)
	writeNibFile(t, storeData(t, nibsDir), "manykeys1--dos.md", sb.String())

	var warnBuf bytes.Buffer
	core := New(nibsDir, config.Default())
	core.SetWarnWriter(&warnBuf)

	if err := core.Load(); err != nil {
		t.Fatalf("Load() returned an error; a many-key DoS file must be skipped, not fail the load: %v", err)
	}

	// The valid nib must be present.
	if _, err := core.Get("good1"); err != nil {
		t.Errorf("valid nib good1 missing after load: %v", err)
	}
	// The DoS nib must be absent (skipped).
	if _, err := core.Get("manykeys1"); err == nil {
		t.Errorf("many-key DoS nib was loaded; it should have been skipped")
	}
	if !strings.Contains(warnBuf.String(), "manykeys1--dos.md") {
		t.Errorf("no warning naming the skipped many-key file:\n%s", warnBuf.String())
	}
}

// TestLoadClassifiesFencelessFileAsDiagnostic pins the single classification
// of a fence-less .md: it is an UnparseableFile load diagnostic ("no front
// matter — not a nib file"), never an empty v0 nib. The phantom-nib class it
// retires: a README in the store appeared in every query, an ungated `nibs
// set` could rewrite it into a nib render, and `nibs check` reported all
// clear while the migration scan called the same file a problem.
func TestLoadClassifiesFencelessFileAsDiagnostic(t *testing.T) {
	nibsDir := setupNibsDir(t)
	const readme = "# Store notes\n\nNo front matter here.\n"
	writeNibFile(t, storeData(t, nibsDir), "README.md", readme)
	writeNibFile(t, storeData(t, nibsDir), "good1--ok.md", "---\nversion: 1\ntitle: Good\nstatus: todo\n---\n\nBody.\n")

	core := New(nibsDir, config.Default())
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error; a fence-less file must degrade per file, not fail the load: %v", err)
	}
	if _, err := core.Get("good1"); err != nil {
		t.Errorf("valid nib missing after load: %v", err)
	}
	if _, err := core.Get("README"); err == nil {
		t.Error("fence-less README.md loaded as a nib; it must be a diagnostic only")
	}

	unparseable, _ := core.LoadDiagnostics()
	if len(unparseable) != 1 || unparseable[0].Path != "data/README.md" {
		t.Fatalf("LoadDiagnostics unparseable = %+v, want exactly README.md", unparseable)
	}
	if !strings.Contains(unparseable[0].Reason, "no front matter") {
		t.Errorf("diagnostic reason = %q, want it to name the missing front matter", unparseable[0].Reason)
	}

	// Classification never writes: the document's bytes stay untouched.
	got, err := os.ReadFile(dataPath(nibsDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != readme {
		t.Errorf("load modified the fence-less file:\n%s", got)
	}
}

// TestLoadSkipsDotDirectories pins Core.Load's walk scope to the shared
// store-content definition (WalkStoreFiles): a .md file inside a dot
// directory (`.git` of a store repo, editor state like `.obsidian`/`.trash`)
// is not store content and must not load as a nib — the migration scans skip
// those directories, so a file loading here would be invisible to every
// pre-migration gate yet visible to every query. Boundary pins: archive/
// stays included, and a top-level dot FILE is still store content (the dot
// rule prunes directories only; files are classified per file like any other).
func TestLoadSkipsDotDirectories(t *testing.T) {
	nibsDir := setupNibsDir(t)

	const nibBody = `---
version: 1
title: A Nib
status: todo
---

Body.
`
	writeNibFile(t, storeData(t, nibsDir), "top1--one.md", nibBody)
	writeNibFile(t, storeArchive(t, nibsDir), "arc1--old.md", nibBody)
	if err := os.MkdirAll(dataPath(nibsDir, ".obsidian", "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeNibFile(t, storeData(t, nibsDir), filepath.Join(".obsidian", "cache", "note1--x.md"), nibBody)
	writeNibFile(t, storeData(t, nibsDir), ".dot1--lockish.md", nibBody)

	core := New(nibsDir, config.Default())
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if _, err := core.Get("top1"); err != nil {
		t.Errorf("top-level nib missing after load: %v", err)
	}
	if _, err := core.Get("arc1"); err != nil {
		t.Errorf("archived nib missing after load (archive/ is store content): %v", err)
	}
	if _, err := core.Get("note1"); err == nil {
		t.Error("dot-directory file loaded as a nib; dot directories are not store content")
	}
	if _, err := core.Get(".dot1"); err != nil {
		t.Errorf("top-level dot FILE missing after load (only dot directories are pruned): %v", err)
	}
}

// TestLoadPropagatesWalkDirIOError pins that a WalkDir-LEVEL I/O error (an
// unreadable subdirectory surfaced through the callback's err parameter) still
// hard-fails Load() and is NOT swallowed by the per-file unparseable-skip. The
// per-file skip only catches loadNib (read/parse) errors; the
// callback's own `if err != nil { return err }` must keep aborting the walk. This
// guards that seam against a future over-broad "simplify both branches" refactor.
func TestLoadPropagatesWalkDirIOError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0 does not deny directory reads on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("chmod 0 does not deny directory reads for root; run as non-root to exercise the WalkDir I/O-error branch")
	}

	nibsDir := setupNibsDir(t)

	// A valid nib so the store isn't trivially empty.
	writeNibFile(t, storeData(t, nibsDir), "good1--ok.md", `---
version: 1
title: Good
status: todo
---

Body.
`)

	// An unreadable subdirectory: WalkDir surfaces the readdir permission error
	// through the callback's err parameter, which must abort Load.
	locked := dataPath(nibsDir, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatalf("chmod 0: %v", err)
	}
	// Restore perms so TempDir cleanup can remove the subdirectory.
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	core := New(nibsDir, config.Default())
	core.SetWarnWriter(nil)
	err := core.Load()
	if err == nil {
		t.Fatal("Load() returned nil; a WalkDir-level I/O error must abort the load, not be swallowed by the per-file skip")
	}
	// The error must name the entry it failed on, ROOTED. os.dirFS trims the root
	// prefix from its *PathError, so the bare error says "open locked: permission
	// denied" — which does not say which store, or whether it was under data/ or
	// archive/, and `nibs check` reports it verbatim.
	if !strings.Contains(err.Error(), locked) {
		t.Errorf("Load() error = %q, want it to name %s", err, locked)
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("Load() error = %q, want errors.Is(err, fs.ErrPermission) to still hold through the wrap", err)
	}
}
