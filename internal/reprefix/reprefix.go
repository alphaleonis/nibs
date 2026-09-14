// Package reprefix renames every nib and its references when a store's prefix
// changes: BuildPlan plans from an in-memory snapshot, and Execute applies the plan.
package reprefix

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/alphaleonis/nibs/internal/safetext"
)

// prefixPattern matches lowercase alphanumerics and dashes that start with an
// alphanumeric and end in a dash. Charset checks elsewhere derive from
// nib.IsIDChar; revisit them if this loosens.
var prefixPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*-$`)

const (
	minPrefixLen = 2  // one character plus the dash
	maxPrefixLen = 16 // a soft cap; long prefixes crowd id display
)

// ValidatePrefix checks a new prefix: 2 to 16 characters matching prefixPattern,
// with no double dash. Ids minted without it, from `nibs new --prefix` or a
// hand-edited config, are still checked by nib.ValidateIDRoundTrip in Core.Create.
func ValidatePrefix(s string) error {
	if len(s) < minPrefixLen || len(s) > maxPrefixLen {
		return fmt.Errorf("invalid prefix %q: length must be between %d and %d characters", s, minPrefixLen, maxPrefixLen)
	}
	if !prefixPattern.MatchString(s) {
		return fmt.Errorf("invalid prefix %q: must match %s", s, prefixPattern.String())
	}
	if i := strings.Index(s, "--"); i >= 0 {
		return fmt.Errorf("invalid prefix %q: a double dash is what a nib file name puts between the id and the slug, so every nib named under this prefix would read back as %q — the text before that double dash", s, s[:i])
	}
	return nil
}

// NibSnapshot is the part of a loaded nib BuildPlan needs. Its link fields are
// nib.LinkSpelling's four, the id-valued front-matter fields. Supply each as it
// should be written back (see nib.RawLinks): only ids carrying the old prefix are
// rewritten. Body mentions are not planned; Execute rewrites them.
type NibSnapshot struct {
	ID string // e.g. "nibs-abc123"
	// Path is the forward-slash path under the store root; its basename must
	// begin with ID (BuildPlan checks).
	Path      string
	Parent    string   // empty if no parent
	Milestone string   // empty if not enqueued in a milestone
	BlockedBy []string // empty/nil if no blockers
	// Blocking is legacy v0 data. Retarget it, do not drop it; clearing it belongs
	// to the v0→v1 migration.
	Blocking []string
}

// TargetExistsFunc reports whether a path under the store root already exists.
type TargetExistsFunc func(relPath string) bool

// RenamePlan is every change needed to move a snapshot from OldPrefix to
// NewPrefix.
type RenamePlan struct {
	OldPrefix  string
	NewPrefix  string
	Files      []FilePlan
	Collisions []string // target paths that already exist; non-empty means the plan is not executable
}

// FilePlan is one nib's rename and front-matter link updates; equal Old and New
// values mean no change. Execute may still rewrite the body's mentions.
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

// PrefixesOverlap reports whether one prefix starts with the other, the case in
// which BuildPlan cannot resume a partly re-prefixed snapshot.
func PrefixesOverlap(a, b string) bool {
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// OverlappingPrefixResumeError is BuildPlan's refusal to resume a partly
// re-prefixed snapshot when one prefix starts with the other: an id carrying
// the longer prefix also carries the shorter, so whether a run already renamed
// it cannot be told from the id.
type OverlappingPrefixResumeError struct {
	OldPrefix string
	NewPrefix string
	ID        string // the first snapshot id carrying the new prefix
}

func (e *OverlappingPrefixResumeError) Error() string {
	return fmt.Sprintf("cannot resume from prefix %q to %q: nib %q carries the new prefix, and because one prefix starts with the other its id does not show whether it was already renamed",
		safetext.Strip(e.OldPrefix), e.NewPrefix, safetext.Strip(e.ID))
}

// BuildPlan computes a RenamePlan from a snapshot of nibs plus old/new prefix.
// It performs no disk I/O. The returned plan preserves the input order of the
// snapshot. The targetExists callback must be non-nil; pass a stub that always
// returns false if collision detection is not relevant to the caller.
//
// A row whose id carries newPrefix is one a failed run already renamed, so a
// rerun over a partly re-prefixed store resumes: the row plans no rename, is
// still rewritten, and its own path is not a collision. Each id is classified
// by the longer of the two prefixes it carries, and when one prefix starts with
// the other a snapshot holding any already-renamed row is refused with an
// OverlappingPrefixResumeError.
func BuildPlan(snapshot []NibSnapshot, oldPrefix, newPrefix string, targetExists TargetExistsFunc) (*RenamePlan, error) {
	// Accept any existing prefix except "": CutPrefix succeeds with it on every id.
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

	collided := make(map[string]bool)
	addCollision := func(path string) {
		if collided[path] {
			return
		}
		collided[path] = true
		plan.Collisions = append(plan.Collisions, path)
	}
	seenNewPath := make(map[string]bool)
	overlapping := PrefixesOverlap(oldPrefix, newPrefix)

	for _, n := range snapshot {
		hasOld := strings.HasPrefix(n.ID, oldPrefix)
		hasNew := strings.HasPrefix(n.ID, newPrefix)
		renamed := hasNew && (!hasOld || len(newPrefix) > len(oldPrefix))
		if !hasOld && !hasNew {
			return nil, fmt.Errorf("snapshot contains nib %q which does not have the expected prefix %q", safetext.Strip(n.ID), safetext.Strip(oldPrefix))
		}
		if renamed && overlapping {
			return nil, &OverlappingPrefixResumeError{OldPrefix: oldPrefix, NewPrefix: newPrefix, ID: n.ID}
		}
		basename := n.Path
		if idx := strings.LastIndex(n.Path, "/"); idx >= 0 {
			basename = n.Path[idx+1:]
		}
		if !strings.HasPrefix(basename, n.ID) {
			id := safetext.Strip(n.ID)
			return nil, fmt.Errorf("nib %q: path basename %q does not start with id %q", id, safetext.Strip(basename), id)
		}
		oldID, newID, newPath := n.ID, rewriteID(n.ID, oldPrefix, newPrefix), rewritePath(n.Path, oldPrefix, newPrefix)
		if renamed {
			oldID, newID, newPath = oldPrefix+strings.TrimPrefix(n.ID, newPrefix), n.ID, n.Path
		}
		fp := FilePlan{
			OldPath:      n.Path,
			NewPath:      newPath,
			OldID:        oldID,
			NewID:        newID,
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
		if !renamed && targetExists(fp.NewPath) {
			addCollision(fp.NewPath)
		}
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
