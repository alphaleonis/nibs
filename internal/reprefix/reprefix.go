// Package reprefix computes a plan for renaming all nibs and their
// cross-references when a project's nib prefix changes. It performs no disk
// I/O — callers supply an in-memory snapshot and the planner returns a
// RenamePlan that an executor (or a dry-run printer) can consume.
package reprefix

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// prefixPattern is the regex a valid nib prefix must match: lowercase
// alphanumerics followed by zero or more lowercase alphanumerics or dashes,
// ending in a trailing dash.
var prefixPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*-$`)

const (
	// minPrefixLen is one letter plus trailing dash ("a-").
	minPrefixLen = 2
	// maxPrefixLen is a soft cap: prefixes this long already crowd ID display
	// in lists and filenames. Loosen if a project has a legitimate need.
	maxPrefixLen = 16
)

// ValidatePrefix checks a prefix against the repo's convention:
// lowercase alphanumerics followed by a trailing dash, length 2–16.
// Returns nil if valid, or a descriptive error.
func ValidatePrefix(s string) error {
	if len(s) < minPrefixLen || len(s) > maxPrefixLen {
		return fmt.Errorf("invalid prefix %q: length must be between %d and %d characters", s, minPrefixLen, maxPrefixLen)
	}
	if !prefixPattern.MatchString(s) {
		return fmt.Errorf("invalid prefix %q: must match %s", s, prefixPattern.String())
	}
	return nil
}

// NibSnapshot is a minimal view of a loaded nib — just what the builder needs.
// It decouples reprefix from nibcore.
type NibSnapshot struct {
	ID string // e.g. "nibs-abc123"
	// Path is the forward-slash relative path to the nib file under the nibs
	// root. The basename MUST begin with ID — e.g. "archive/tnib-abc--slug.md"
	// for ID "tnib-abc". BuildPlan enforces this invariant.
	Path      string
	Parent    string   // empty if no parent
	BlockedBy []string // empty/nil if no blockers
}

// TargetExistsFunc reports whether a relative path already exists under the
// nibs root. Tests pass a stub; a real executor will pass an os.Stat-backed impl.
type TargetExistsFunc func(relPath string) bool

// RenamePlan is the complete set of changes required to retarget a snapshot
// from one prefix to another. It carries no hidden state — callers consume it
// directly.
type RenamePlan struct {
	OldPrefix  string
	NewPrefix  string
	Files      []FilePlan
	Collisions []string // target paths that already exist; non-empty means the plan is not executable
}

// FilePlan describes the rename and reference updates for a single nib.
// Equal Old/New values for parent/blocked_by mean "no change needed" — callers
// can use HasReferenceUpdates to decide whether to rewrite the file body.
type FilePlan struct {
	OldPath string
	NewPath string
	OldID   string
	NewID   string

	OldParent    string
	NewParent    string
	OldBlockedBy []string
	NewBlockedBy []string
}

// HasReferenceUpdates reports whether any of the nib's cross-references
// (parent or blocked_by) need to be rewritten under the new prefix.
func (fp FilePlan) HasReferenceUpdates() bool {
	if fp.OldParent != fp.NewParent {
		return true
	}
	if len(fp.OldBlockedBy) != len(fp.NewBlockedBy) {
		return true
	}
	for i := range fp.OldBlockedBy {
		if fp.OldBlockedBy[i] != fp.NewBlockedBy[i] {
			return true
		}
	}
	return false
}

// BuildPlan computes a RenamePlan from a snapshot of nibs plus old/new prefix.
// It performs no disk I/O. The returned plan preserves the input order of the
// snapshot. The targetExists callback must be non-nil; pass a stub that always
// returns false if collision detection is not relevant to the caller.
func BuildPlan(snapshot []NibSnapshot, oldPrefix, newPrefix string, targetExists TargetExistsFunc) (*RenamePlan, error) {
	if err := ValidatePrefix(oldPrefix); err != nil {
		return nil, fmt.Errorf("old prefix: %w", err)
	}
	if err := ValidatePrefix(newPrefix); err != nil {
		return nil, fmt.Errorf("new prefix: %w", err)
	}
	if oldPrefix == newPrefix {
		return nil, fmt.Errorf("new prefix %q is the same as the old prefix; nothing to do", newPrefix)
	}
	if targetExists == nil {
		return nil, fmt.Errorf("targetExists callback is required")
	}
	plan := &RenamePlan{
		OldPrefix: oldPrefix,
		NewPrefix: newPrefix,
		Files:     make([]FilePlan, 0, len(snapshot)),
	}

	// Track collisions in a set so on-disk and intra-plan collisions
	// never produce duplicate entries.
	collided := make(map[string]bool)
	addCollision := func(path string) {
		if collided[path] {
			return
		}
		collided[path] = true
		plan.Collisions = append(plan.Collisions, path)
	}
	seenNewPath := make(map[string]bool)

	for _, n := range snapshot {
		if !strings.HasPrefix(n.ID, oldPrefix) {
			return nil, fmt.Errorf("snapshot contains nib %q which does not have the expected prefix %q", n.ID, oldPrefix)
		}
		// Enforce the NibSnapshot.Path invariant: the basename must begin with ID.
		basename := n.Path
		if idx := strings.LastIndex(n.Path, "/"); idx >= 0 {
			basename = n.Path[idx+1:]
		}
		if !strings.HasPrefix(basename, n.ID) {
			return nil, fmt.Errorf("nib %q: path basename %q does not start with id %q", n.ID, basename, n.ID)
		}
		fp := FilePlan{
			OldPath:      n.Path,
			NewPath:      rewritePath(n.Path, oldPrefix, newPrefix),
			OldID:        n.ID,
			NewID:        rewriteID(n.ID, oldPrefix, newPrefix),
			OldParent:    n.Parent,
			NewParent:    rewriteRef(n.Parent, oldPrefix, newPrefix),
			OldBlockedBy: slices.Clone(n.BlockedBy),
			NewBlockedBy: rewriteRefs(n.BlockedBy, oldPrefix, newPrefix),
		}
		plan.Files = append(plan.Files, fp)
		if targetExists(fp.NewPath) {
			addCollision(fp.NewPath)
		}
		// Intra-plan collision: two snapshot rows mapped to the same NewPath.
		if seenNewPath[fp.NewPath] {
			addCollision(fp.NewPath)
		}
		seenNewPath[fp.NewPath] = true
	}

	return plan, nil
}

// rewriteID replaces the leading oldPrefix on an ID with newPrefix.
// If the ID does not start with oldPrefix it is returned unchanged.
func rewriteID(id, oldPrefix, newPrefix string) string {
	if rest, ok := strings.CutPrefix(id, oldPrefix); ok {
		return newPrefix + rest
	}
	return id
}

// rewriteRef rewrites a single reference (e.g. a parent ID). Empty strings
// pass through unchanged.
func rewriteRef(ref, oldPrefix, newPrefix string) string {
	if ref == "" {
		return ""
	}
	return rewriteID(ref, oldPrefix, newPrefix)
}

// rewriteRefs rewrites every entry in a slice of references. A nil input
// produces a nil output so downstream equality checks work naturally.
func rewriteRefs(refs []string, oldPrefix, newPrefix string) []string {
	if refs == nil {
		return nil
	}
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = rewriteRef(r, oldPrefix, newPrefix)
	}
	return out
}

// rewritePath rewrites the leading filename portion of a forward-slash path
// so that a file like "archive/tnib-abc--slug.md" becomes
// "archive/new-abc--slug.md". Only the basename is touched — the directory
// portion is preserved verbatim.
func rewritePath(path, oldPrefix, newPrefix string) string {
	idx := strings.LastIndex(path, "/")
	dir := ""
	base := path
	if idx >= 0 {
		dir = path[:idx+1]
		base = path[idx+1:]
	}
	if rest, ok := strings.CutPrefix(base, oldPrefix); ok {
		base = newPrefix + rest
	}
	return dir + base
}
