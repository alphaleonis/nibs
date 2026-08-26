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
// The short-ID charset [0-9a-z] this pattern builds on is owned by
// internal/nib (nib.IsIDChar / idAlphabet, the single source of truth);
// consumers such as nibcore's search gate derive from it. Revisit those
// derived charset checks if this pattern loosens.
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
//
// The link fields below are exactly nib.LinkSpelling's four, and that is the
// completeness bar FOR FRONT MATTER: an id-valued front-matter field missing
// here is a reference left naming a nib the rename retired. The other
// path-shaped fields are deliberately absent because none of them holds an id —
// Area is a plain path, Documents are repo-relative paths, MilestoneOrder is a
// fractional index, and Extra is opaque unknown keys.
//
// A nib BODY is outside the bar: `[[id]]` and `#id` mentions there are id-valued
// too, and Execute re-renders a nib without touching its body, so they are left
// naming the retired id. Extending the rewrite over bodies is tracked as its own
// work, not covered here.
type NibSnapshot struct {
	ID string // e.g. "nibs-abc123"
	// Path is the forward-slash relative path to the nib file under the nibs
	// root. The basename MUST begin with ID — e.g. "archive/tnib-abc--slug.md"
	// for ID "tnib-abc". BuildPlan enforces this invariant.
	Path      string
	Parent    string   // empty if no parent
	Milestone string   // empty if not enqueued in a milestone
	BlockedBy []string // empty/nil if no blockers
	// Blocking is the legacy v0 spelling of the blocked-by edge. v1+ derives
	// blocking from other nibs' BlockedBy and never writes it, but nib.Render
	// re-emits whatever a v0 file carries, so a store with the v0→v1 migration
	// still deferred has real `blocking:` ids that must be retargeted too.
	//
	// It is retargeted rather than dropped: clearing the field belongs to that
	// migration, which transfers each edge onto its target first (see
	// nibcore's v0→v1 step and the same stance in nibcore/link_health.go).
	// A rename dropping it would destroy edges no other field records yet.
	Blocking []string
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
// Equal Old/New values for a link field mean "no change needed" — callers
// can use HasReferenceUpdates to decide whether to rewrite the file body.
//
// The four link pairs mirror NibSnapshot's; see its doc comment for why those
// four and no others.
type FilePlan struct {
	OldPath string
	NewPath string
	OldID   string
	NewID   string

	OldParent    string
	NewParent    string
	OldMilestone string
	NewMilestone string
	OldBlockedBy []string
	NewBlockedBy []string
	OldBlocking  []string
	NewBlocking  []string
}

// HasReferenceUpdates reports whether any of the nib's cross-references
// (parent, milestone, blocked_by or legacy blocking) need to be rewritten
// under the new prefix.
func (fp FilePlan) HasReferenceUpdates() bool {
	return fp.OldParent != fp.NewParent ||
		fp.OldMilestone != fp.NewMilestone ||
		!slices.Equal(fp.OldBlockedBy, fp.NewBlockedBy) ||
		!slices.Equal(fp.OldBlocking, fp.NewBlocking)
}

// BuildPlan computes a RenamePlan from a snapshot of nibs plus old/new prefix.
// It performs no disk I/O. The returned plan preserves the input order of the
// snapshot. The targetExists callback must be non-nil; pass a stub that always
// returns false if collision detection is not relevant to the caller.
func BuildPlan(snapshot []NibSnapshot, oldPrefix, newPrefix string, targetExists TargetExistsFunc) (*RenamePlan, error) {
	// The old prefix is whatever a past `nibs init` produced for this
	// project. It may not match the strict rules we apply to new prefixes
	// (e.g. projects initialized from a dir named "boardGameTracker" end
	// up with "boardGameTracker-" — 17 chars, uppercase). Accepting such
	// prefixes is the entire point of this command. We only reject the
	// empty case, which would otherwise cause `strings.CutPrefix(id, "")`
	// to succeed on every ID and double-prefix every file.
	if oldPrefix == "" {
		return nil, fmt.Errorf("old prefix: must not be empty")
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
			OldMilestone: n.Milestone,
			NewMilestone: rewriteRef(n.Milestone, oldPrefix, newPrefix),
			OldBlockedBy: slices.Clone(n.BlockedBy),
			NewBlockedBy: rewriteRefs(n.BlockedBy, oldPrefix, newPrefix),
			OldBlocking:  slices.Clone(n.Blocking),
			NewBlocking:  rewriteRefs(n.Blocking, oldPrefix, newPrefix),
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
