package nibcore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/testdata/fixtures"
)

// A nib file that OMITS the `priority:` line — exactly what CreateNib writes when
// the client omits priority (the resolver only sets Priority when provided).
// `type:` is present, as every app-created nib carries it.
const missingPriorityFile = "nopri1--missing-priority.md"
const missingPriorityContent = `---
version: 1
title: No Priority
status: todo
type: task
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---

Body.
`

// A nib file that OMITS the `type:` line (hand-authored — the app always writes
// a type). Priority is present so the test isolates the missing-type axis.
const missingTypeFile = "notype1--missing-type.md"
const missingTypeContent = `---
version: 1
title: No Type
status: todo
priority: high
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---

Body.
`

// TestETagNoFalseConflictOnMissingDefault is the primary regression guard:
// a nib LOADED from a file that omits a presentation-defaulted field
// (`priority:` or `type:`) must have Get().ETag() equal to CurrentETag() (no
// false conflict), an if-match Update using the Get() etag must succeed with no
// concurrent on-disk change, AND that Update must NOT persist the default back to
// disk — the file must still omit the field (the core invariant of the refactor:
// an Update writing the effective value would re-diverge the on-disk bytes).
// The bug only manifests after a Load/watcher reload (loadNib used to synthesize
// a presentation default the bare-parse CurrentETag does not), so the test forces
// the Load path via setupLoadedCore.
func TestETagNoFalseConflictOnMissingDefault(t *testing.T) {
	tests := []struct {
		name        string
		file        string
		content     string
		id          string
		absentField string // frontmatter key the file omits; Update must not add it
	}{
		{"missing priority", missingPriorityFile, missingPriorityContent, "nopri1", "priority:"},
		{"missing type", missingTypeFile, missingTypeContent, "notype1", "type:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsDir := setupNibsDir(t)
			writeNibFile(t, nibsDir, tt.file, tt.content)
			core := setupLoadedCore(t, nibsDir)

			b, err := core.Get(tt.id)
			if err != nil {
				t.Fatalf("Get() error: %v", err)
			}

			inMemory := b.ETag()
			stored, err := core.CurrentETag(tt.id)
			if err != nil {
				t.Fatalf("CurrentETag() error: %v", err)
			}
			if stored != inMemory {
				t.Fatalf("false conflict: CurrentETag=%s != in-memory ETag=%s for a file missing %s", stored, inMemory, tt.absentField)
			}

			updated := b.Clone()
			updated.Title = tt.name + " (edited)"
			if err := core.Update(updated, &inMemory); err != nil {
				var mismatch *ETagMismatchError
				if errors.As(err, &mismatch) {
					t.Fatalf("if-match Update false-conflicted: provided=%s current=%s", mismatch.Provided, mismatch.Current)
				}
				t.Fatalf("if-match Update failed: %v", err)
			}

			// Core invariant: the Update must NOT persist the presentation default
			// back to disk — the file must still OMIT the defaulted field. Re-Get to
			// pick up the (possibly renamed) path after the title change.
			after, err := core.Get(tt.id)
			if err != nil {
				t.Fatalf("Get() after Update error: %v", err)
			}
			disk, err := os.ReadFile(filepath.Join(nibsDir, after.Path))
			if err != nil {
				t.Fatalf("ReadFile(%s) error: %v", after.Path, err)
			}
			if strings.Contains(string(disk), tt.absentField) {
				t.Errorf("Update persisted %q to disk; the default must stay omitted:\n%s", tt.absentField, disk)
			}
		})
	}
}

// TestETagJustCreatedOmittingPriorityRoundTrips pins the create path: a nib
// created in-session WITHOUT a priority (never Loaded) must still round-trip an
// immediate if-match Update. loadNib synthesizes no defaults, so this path must
// hold on its own — Create keeps Priority empty, Render omits it, and the bare-parse stored
// etag equals the in-memory etag.
func TestETagJustCreatedOmittingPriorityRoundTrips(t *testing.T) {
	nibsDir := setupNibsDir(t)
	core := setupLoadedCore(t, nibsDir)

	b := &nib.Nib{ID: "fresh1", Title: "Fresh", Status: "todo", Type: "task", Version: 1}
	if err := core.Create(b); err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	got, err := core.Get("fresh1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Priority != "" {
		t.Fatalf("just-created nib gained a synthesized Priority %q; the stored nib must stay empty", got.Priority)
	}
	ifMatch := got.ETag()

	updated := got.Clone()
	updated.Title = "Fresh (edited)"
	if err := core.Update(updated, &ifMatch); err != nil {
		t.Fatalf("immediate if-match Update on a just-created priority-less nib failed: %v", err)
	}
}

// TestETagNoFalseConflictAcrossSampleFixture is the end-to-end regression net:
// it copies the real sample fixture, injects a priority-omitting and a
// type-omitting nib (the fixture itself carries neither), reloads, and asserts
// that EVERY nib's in-memory ETag() equals its CurrentETag() — i.e. no
// default-omitting nib false-conflicts on an if-match Update.
func TestETagNoFalseConflictAcrossSampleFixture(t *testing.T) {
	root := fixtures.CopySampleProject(t)
	nibsDir := fixtures.NibsPath(root)

	// Inject default-omitting nibs into the (mutable) copy.
	writeNibFile(t, nibsDir, missingPriorityFile, missingPriorityContent)
	writeNibFile(t, nibsDir, missingTypeFile, missingTypeContent)

	cfg := config.Default()
	core := New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	for _, b := range core.All() {
		stored, err := core.computeStoredETag(b)
		if err != nil {
			t.Fatalf("computeStoredETag(%s) unexpected error: %v", b.ID, err)
		}
		if stored != b.ETag() {
			t.Errorf("nib %s (path %s) false-conflicts: CurrentETag=%s != in-memory ETag=%s",
				b.ID, b.Path, stored, b.ETag())
		}
	}

	// And prove an if-match Update on the injected priority-less nib succeeds.
	b, err := core.Get("nopri1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	ifMatch := b.ETag()
	updated := b.Clone()
	updated.Title = "No Priority (edited via fixture)"
	if err := core.Update(updated, &ifMatch); err != nil {
		t.Fatalf("if-match Update on injected priority-less fixture nib failed: %v", err)
	}
}
