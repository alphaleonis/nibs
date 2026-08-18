package reprefix

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alphaleonis/nibs/internal/nib"
)

// Execute applies a RenamePlan to disk under the given nibs root.
// root is the absolute path to the .nibs directory.
//
// Operation order:
//  1. Pre-flight: refuse if plan.Collisions is non-empty
//  2. Rename every FilePlan.OldPath -> FilePlan.NewPath
//  3. Rewrite every renamed file: read, update ID + Parent + BlockedBy, write
//
// Execute does NOT:
//   - Update the project config (<store>/config.yml) — callers do that AFTER Execute
//     returns successfully, so a partial rename leaves the config pointing
//     at the old prefix for easier recovery.
//   - Coordinate with a file watcher — callers must ensure no fsnotify
//     watcher is monitoring the nibs directory while Execute runs.
//
// On partial failure, Execute returns an error naming the failing file and
// does NOT roll back. Re-running Execute with a fresh snapshot and plan
// converges from either failure point.
func Execute(plan *RenamePlan, root string) error {
	if plan == nil {
		return fmt.Errorf("reprefix.Execute: plan is nil")
	}
	if len(plan.Collisions) > 0 {
		return fmt.Errorf("reprefix.Execute: refusing to run, plan has %d collision(s): %v", len(plan.Collisions), plan.Collisions)
	}

	// Pass 1 renames files on disk; Pass 2 re-renders every renamed file.
	// The two passes are kept separate (rather than rewriting during rename)
	// so a partial failure leaves disk state a re-run with a fresh snapshot
	// can converge on. Every renamed file is re-rendered even if it has no
	// reference updates, so the in-file `# <id>` comment stays in sync with
	// the new filename. Rename is first because it is mechanical and cheap
	// to reason about on partial failure; rewrite second because it
	// operates on already-renamed files.
	if err := renameAll(plan.Files, root); err != nil {
		return err
	}
	if err := rewriteAll(plan.Files, root); err != nil {
		return err
	}
	return nil
}

// renameAll renames each FilePlan.OldPath -> FilePlan.NewPath under root.
// Returns an error wrapped with the failing file's relative path.
//
// No os.MkdirAll is needed here: reprefix.rewritePath only rewrites the
// basename, so filepath.Dir(newAbs) == filepath.Dir(oldAbs), and the
// source file's parent directory provably already exists.
func renameAll(files []FilePlan, root string) error {
	for _, fp := range files {
		oldAbs := filepath.Join(root, filepath.FromSlash(fp.OldPath))
		newAbs := filepath.Join(root, filepath.FromSlash(fp.NewPath))
		if err := os.Rename(oldAbs, newAbs); err != nil {
			return fmt.Errorf("reprefix.Execute: rename %s -> %s: %w", fp.OldPath, fp.NewPath, err)
		}
	}
	return nil
}

// rewriteAll updates the in-file ID/Parent/BlockedBy for every renamed nib.
// Runs over every file plan, not just those with reference updates, so the
// in-file `# <id>` comment is always re-rendered to match the new ID.
func rewriteAll(files []FilePlan, root string) error {
	for _, fp := range files {
		absPath := filepath.Join(root, filepath.FromSlash(fp.NewPath))
		if err := rewriteOne(absPath, fp); err != nil {
			return err
		}
	}
	return nil
}

// rewriteOne reads a single renamed nib, updates ID/Parent/BlockedBy, and
// writes it back. All errors are wrapped with the file's new relative path
// so callers can pinpoint partial-failure locations.
func rewriteOne(absPath string, fp FilePlan) error {
	b, mode, err := readNibForRewrite(absPath, fp)
	if err != nil {
		return err
	}
	b.ID = fp.NewID
	b.Parent = fp.NewParent
	b.BlockedBy = fp.NewBlockedBy
	data, err := b.Render()
	if err != nil {
		return fmt.Errorf("reprefix.Execute: render %s: %w", fp.NewPath, err)
	}

	// Write to a sibling temp file then atomically rename over the target so
	// a mid-write crash cannot leave the nib half-written. A half-written
	// file would fail nib.Parse on the next snapshot build and block the
	// idempotent-rerun recovery story.
	tmp := absPath + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("reprefix.Execute: write temp %s: %w", fp.NewPath, err)
	}
	if err := os.Rename(tmp, absPath); err != nil {
		_ = os.Remove(tmp) // best-effort cleanup
		return fmt.Errorf("reprefix.Execute: atomic replace %s: %w", fp.NewPath, err)
	}
	return nil
}

// readNibForRewrite opens absPath, parses it, and captures its mode. The
// file handle is closed before returning so that Windows can rename over
// absPath later without hitting a sharing violation on the open handle.
// The captured mode is returned so the subsequent atomic rename preserves
// any user-set permission bits (e.g. 0o640 group-private files). On
// Windows, mode bits are effectively ignored but the code path is still
// correct.
func readNibForRewrite(absPath string, fp FilePlan) (*nib.Nib, os.FileMode, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, 0, fmt.Errorf("reprefix.Execute: open %s: %w", fp.NewPath, err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("reprefix.Execute: stat %s: %w", fp.NewPath, err)
	}
	b, err := nib.Parse(f)
	if err != nil {
		return nil, 0, fmt.Errorf("reprefix.Execute: parse %s: %w", fp.NewPath, err)
	}
	return b, info.Mode().Perm(), nil
}
