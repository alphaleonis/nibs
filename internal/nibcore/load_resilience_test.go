package nibcore

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
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
	writeNibFile(t, nibsDir, "gooda1--good.md", goodA)
	writeNibFile(t, nibsDir, "goodb1--good.md", goodB)
	writeNibFile(t, nibsDir, "dupmod1--broken.md", dupModeled)
	writeNibFile(t, nibsDir, "dupext1--broken.md", dupExtra)

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
	gotMod, err := os.ReadFile(filepath.Join(nibsDir, "dupmod1--broken.md"))
	if err != nil {
		t.Fatalf("reading skipped file: %v", err)
	}
	if string(gotMod) != dupModeled {
		t.Errorf("skipped duplicate-modeled-key file was modified on disk:\n got:\n%s\nwant:\n%s", gotMod, dupModeled)
	}
	gotExt, err := os.ReadFile(filepath.Join(nibsDir, "dupext1--broken.md"))
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

	writeNibFile(t, nibsDir, "good1--ok.md", good)
	writeNibFile(t, nibsDir, "manykeys1--dos.md", sb.String())

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

// TestLoadPropagatesWalkDirIOError pins that a WalkDir-LEVEL I/O error (an
// unreadable subdirectory surfaced through the callback's err parameter) still
// hard-fails Load() and is NOT swallowed by the per-file unparseable-skip. The
// per-file skip only catches loadNibReconciledLocked (read/parse) errors; the
// callback's own `if err != nil { return err }` must keep aborting the walk. This
// guards that seam against a future over-broad "simplify both branches" refactor.
func TestLoadPropagatesWalkDirIOError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod 0 does not deny directory reads for root; run as non-root to exercise the WalkDir I/O-error branch")
	}

	nibsDir := setupNibsDir(t)

	// A valid nib so the store isn't trivially empty.
	writeNibFile(t, nibsDir, "good1--ok.md", `---
version: 1
title: Good
status: todo
---

Body.
`)

	// An unreadable subdirectory: WalkDir surfaces the readdir permission error
	// through the callback's err parameter, which must abort Load.
	locked := filepath.Join(nibsDir, "locked")
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
	if err := core.Load(); err == nil {
		t.Fatal("Load() returned nil; a WalkDir-level I/O error must abort the load, not be swallowed by the per-file skip")
	}
}
