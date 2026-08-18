package reprefix

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/alphaleonis/nibs/internal/fsutil"
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

	// One directory fsync per directory the run touched, not one per file, and
	// deferred so a run that aborts partway still flushes what it already
	// committed — every return below leaves earlier renames and rewrites on
	// disk, and a flush placed after the two passes would be skipped exactly
	// then. Both passes record into the same batch because they touch the same
	// directories: rewritePath only rewrites a basename, so each rewrite lands
	// in the directory its own rename created.
	pending := dirSyncBatch{}
	defer pending.flush()

	// Pass 1 renames files on disk; Pass 2 re-renders every renamed file.
	// The two passes are kept separate (rather than rewriting during rename)
	// so a partial failure leaves disk state a re-run with a fresh snapshot
	// can converge on. Every renamed file is re-rendered even if it has no
	// reference updates, so the in-file `# <id>` comment stays in sync with
	// the new filename. Rename is first because it is mechanical and cheap
	// to reason about on partial failure; rewrite second because it
	// operates on already-renamed files.
	if err := renameAll(plan.Files, root, pending); err != nil {
		return err
	}
	if err := rewriteAll(plan.Files, root, pending); err != nil {
		return err
	}
	return nil
}

// dirSyncBatch collects the distinct directories a run wrote into, so it pays
// one directory fsync per DIRECTORY rather than one per file.
//
// A set rather than a single remembered directory because one plan spans
// several: a nib's Path carries the content directory it lives in, so archived
// nibs sit under archive/ while active ones sit under data/, which itself
// tolerates subdirectories that rewritePath preserves verbatim. Syncing one
// hardcoded directory would silently drop the durability of every rename and
// rewrite outside it.
type dirSyncBatch map[string]struct{}

// add records a directory to flush. The empty string is ignored, so a caller
// can hand it the result of a failed write without a guard.
func (b dirSyncBatch) add(dir string) {
	if dir == "" {
		return
	}
	b[dir] = struct{}{}
}

// flush fsyncs each collected directory once, in a deterministic order.
// Best-effort, like every directory sync here: see fsutil.AtomicWriteFile's
// "does not promise" list.
func (b dirSyncBatch) flush() {
	dirs := make([]string, 0, len(b))
	for dir := range b {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		fsutil.SyncDir(dir)
	}
}

// renameAll renames each FilePlan.OldPath -> FilePlan.NewPath under root,
// recording every directory it renamed into so the caller's batch flushes it.
// Returns an error wrapped with the failing file's relative path.
//
// The new names get their own flush rather than relying on a later pass. On the
// success path the rewrite pass
// would flush the same directories anyway, since it writes into the directory
// each rename created — but when this pass aborts, the rewrite pass never runs
// and nothing else would ever flush the renames that already landed. Recording
// here makes the pass durable on its own rather than durable only when a later
// pass happens to cover it, and it costs one fsync per directory, not one per
// file.
//
// No os.MkdirAll is needed here: reprefix.rewritePath only rewrites the
// basename, so filepath.Dir(newAbs) == filepath.Dir(oldAbs), and the
// source file's parent directory provably already exists.
func renameAll(files []FilePlan, root string, pending dirSyncBatch) error {
	for _, fp := range files {
		oldAbs := filepath.Join(root, filepath.FromSlash(fp.OldPath))
		newAbs := filepath.Join(root, filepath.FromSlash(fp.NewPath))
		if err := os.Rename(oldAbs, newAbs); err != nil {
			return fmt.Errorf("reprefix.Execute: rename %s -> %s: %w", fp.OldPath, fp.NewPath, err)
		}
		pending.add(filepath.Dir(newAbs))
	}
	return nil
}

// rewriteAll updates the in-file ID/Parent/BlockedBy for every renamed nib.
// Runs over every file plan, not just those with reference updates, so the
// in-file `# <id>` comment is always re-rendered to match the new ID.
//
// Each write defers its directory flush to the caller's batch, so N rewrites
// into one directory pay a single fsync instead of N identical ones. What the
// loop records is the directory each write actually landed in rather than an
// assumption about it, so the flush obligation the writer hands back is always
// discharged.
//
// That recording is redundant TODAY and no test can prove otherwise: a rewrite
// lands where its own rename put the file, so the rename pass has already
// recorded every directory this one would, and removing the add() here leaves
// the suite green. It stays because discarding the directory
// AtomicWriteFileDeferDirSync hands back is the mistake its contract warns
// about, and because the redundancy holds only while rewritePath touches the
// basename alone. Anything that moved a nib between directories would make this
// line load-bearing with nothing to notice it had been dropped.
func rewriteAll(files []FilePlan, root string, pending dirSyncBatch) error {
	for _, fp := range files {
		absPath := filepath.Join(root, filepath.FromSlash(fp.NewPath))
		dir, err := rewriteOne(absPath, fp)
		pending.add(dir)
		if err != nil {
			return err
		}
	}
	return nil
}

// rewriteOne reads a single renamed nib, updates ID/Parent/BlockedBy, and
// writes it back. All errors are wrapped with the file's new relative path
// so callers can pinpoint partial-failure locations.
//
// It returns the directory the write landed in, whose entry is not yet
// flushed — the caller owes it to a dirSyncBatch — and the empty string on
// error, since a failed write never reached its rename.
func rewriteOne(absPath string, fp FilePlan) (string, error) {
	b, mode, err := readNibForRewrite(absPath, fp)
	if err != nil {
		return "", err
	}
	b.ID = fp.NewID
	b.Parent = fp.NewParent
	b.BlockedBy = fp.NewBlockedBy
	data, err := b.Render()
	if err != nil {
		return "", fmt.Errorf("reprefix.Execute: render %s: %w", fp.NewPath, err)
	}

	// Every nib write goes through the shared atomic writer, so this bulk pass
	// gets the same contract as a single-nib save: a UNIQUELY named temp file
	// (a fixed "<path>.tmp" would collide between two writers racing on one
	// nib), fsynced before the rename, so a mid-write crash can leave neither a
	// half-written file nor a renamed-but-empty one. Either would fail nib.Parse
	// on the next snapshot build and block the idempotent-rerun recovery story.
	// Only the directory flush is deferred, to the caller's batch.
	dir, err := fsutil.AtomicWriteFileDeferDirSync(absPath, data, mode)
	if err != nil {
		return "", fmt.Errorf("reprefix.Execute: atomic replace %s: %w", fp.NewPath, err)
	}
	return dir, nil
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
