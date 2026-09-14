package reprefix

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alphaleonis/nibs/internal/fsutil"
	"github.com/alphaleonis/nibs/internal/nib"
)

// Execute applies plan under root, the store directory: it renames every file,
// then re-renders each with its new id, link fields and full-form body mentions.
// It refuses a plan with collisions. Update the config only after it succeeds,
// and run it with no watcher on the store. A failure is not rolled back: the
// error names the file, and earlier renames and rewrites stay on disk. A plan
// BuildPlan derives from the store that failure left resumes it: a row it
// already renamed has OldPath == NewPath and is only rewritten, and rewriting
// an already-rewritten file changes nothing.
func Execute(plan *RenamePlan, root string) error {
	if plan == nil {
		return fmt.Errorf("reprefix.Execute: plan is nil")
	}
	if len(plan.Collisions) > 0 {
		return fmt.Errorf("reprefix.Execute: refusing to run, plan has %d collision(s): %v", len(plan.Collisions), plan.Collisions)
	}

	// One fsync per directory, flushed even when a pass aborts.
	var pending fsutil.DirSyncBatch
	defer pending.Flush()

	// Every renamed file is re-rendered, even with no link changes, so its
	// `# <id>` line matches the new name.
	if err := renameAll(plan.Files, root, &pending); err != nil {
		return err
	}
	if err := rewriteAll(plan, root, &pending); err != nil {
		return err
	}
	return nil
}

// renameAll renames each file under root and records its directory in pending.
// No MkdirAll: rewritePath keeps the directory, so it already exists.
func renameAll(files []FilePlan, root string, pending *fsutil.DirSyncBatch) error {
	for _, fp := range files {
		if fp.OldPath == fp.NewPath {
			continue
		}
		oldAbs := filepath.Join(root, filepath.FromSlash(fp.OldPath))
		newAbs := filepath.Join(root, filepath.FromSlash(fp.NewPath))
		if err := os.Rename(oldAbs, newAbs); err != nil {
			return fmt.Errorf("reprefix.Execute: rename %s -> %s: %w", fp.OldPath, fp.NewPath, err)
		}
		pending.Add(filepath.Dir(newAbs))
	}
	return nil
}

// rewriteAll re-renders every renamed nib and records the directory each write
// returns in pending.
func rewriteAll(plan *RenamePlan, root string, pending *fsutil.DirSyncBatch) error {
	for _, fp := range plan.Files {
		absPath := filepath.Join(root, filepath.FromSlash(fp.NewPath))
		dir, err := rewriteOne(absPath, fp, plan.OldPrefix, plan.NewPrefix)
		pending.Add(dir)
		if err != nil {
			return err
		}
	}
	return nil
}

// rewriteOne rewrites one renamed nib and returns the directory still to flush,
// or "" with an error that names the file.
func rewriteOne(absPath string, fp FilePlan, oldPrefix, newPrefix string) (string, error) {
	b, mode, err := readNibForRewrite(absPath, fp)
	if err != nil {
		return "", err
	}
	b.ID = fp.NewID
	b.Parent = fp.NewParent
	b.Milestone = fp.NewMilestone
	b.BlockedBy = fp.NewBlockedBy
	b.Blocking = fp.NewBlocking
	b.Body = rewriteBodyMentions(b.Body, oldPrefix, newPrefix)
	data, err := b.Render()
	if err != nil {
		return "", fmt.Errorf("reprefix.Execute: render %s: %w", fp.NewPath, err)
	}

	// Write atomically; only the directory flush is left to the caller's batch.
	dir, err := fsutil.AtomicWriteFileDeferDirSync(absPath, data, mode)
	if err != nil {
		return "", fmt.Errorf("reprefix.Execute: atomic replace %s: %w", fp.NewPath, err)
	}
	return dir, nil
}

// readNibForRewrite parses absPath and returns its permission bits for the write
// back. The file is closed before it returns, so the replace can rename over it.
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

// rewriteBodyMentions retargets every full-form `#<oldPrefix><rest>` mention in
// body to newPrefix. nib.ExtractMentionSpans decides what is a mention; short-form
// mentions are left alone, as link fields are. Splice from the highest offset
// down, since each replacement shifts everything after it.
func rewriteBodyMentions(body, oldPrefix, newPrefix string) string {
	// An empty oldPrefix would retarget every short-form mention, and Execute does
	// not run BuildPlan's check against it.
	if oldPrefix == "" {
		return body
	}

	spans := nib.ExtractMentionSpans(body)
	out := body
	for i := len(spans) - 1; i >= 0; i-- {
		s := spans[i]
		rest, ok := strings.CutPrefix(s.Token, oldPrefix)
		if !ok {
			continue
		}
		out = out[:s.Start] + "#" + newPrefix + rest + out[s.Stop:]
	}
	return out
}
