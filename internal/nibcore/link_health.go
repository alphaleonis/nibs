package nibcore

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/fsutil"
	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibtypes"
)

// BrokenLink represents a link to a non-existent nib.
type BrokenLink struct {
	NibID    string `json:"nib_id"`
	LinkType string `json:"link_type"`
	Target   string `json:"target"`
}

// SelfLink represents a nib linking to itself.
type SelfLink struct {
	NibID    string `json:"nib_id"`
	LinkType string `json:"link_type"`
}

// Cycle represents a circular dependency in links.
type Cycle struct {
	LinkType string   `json:"link_type"`
	Path     []string `json:"path"`
}

// BrokenDocument represents a document link to a non-existent file.
type BrokenDocument struct {
	NibID string `json:"nib_id"`
	Path  string `json:"path"`
}

// UnparseableFile is a .md file under the nibs root that failed to parse (or
// could not be read) during the last load and was therefore SKIPPED: the nib is
// absent from every query, and no query result hints at why. The load warns on
// stderr; this finding is what makes the omission actionable.
type UnparseableFile struct {
	// NibID is derived from the FILENAME, so it names the nib that went missing.
	// Empty when the filename yields no id.
	NibID string `json:"nib_id"`
	// Path is relative to the nibs root with forward slashes, like nib.Path.
	Path string `json:"path"`
	// Reason is the underlying parse/read error, verbatim.
	Reason string `json:"reason"`
}

// DuplicateID is two on-disk files whose filenames parse to the SAME nib id.
// The load keeps only one of them, so the other's contents sit on disk
// reachable through no query, and which one wins depends on walk order.
//
// N files sharing one id produce N-1 entries, chained in load order
// (b shadows a, then c shadows b), so only the LAST entry's Loaded file is the
// final occupant of the id.
type DuplicateID struct {
	NibID string `json:"nib_id"`
	// Loaded and Shadowed are relative to the nibs root with forward slashes,
	// like UnparseableFile.Path.
	Loaded   string `json:"loaded"`
	Shadowed string `json:"shadowed"`
}

// InvalidEnum is a loaded nib carrying an out-of-enum field value (an unknown
// status/type/priority/estimate — e.g. the legacy `priority: deferred` on a
// store whose migration has not run, or a hand-edited typo). The value loads
// exactly as written (see loadFromDisk's diagnostic warning), and every update
// that leaves it in place is then refused by ValidateEnums — the same dead end
// InvalidAxis describes. This finding names the file to repair.
type InvalidEnum struct {
	NibID string `json:"nib_id"`
	// Reason is ValidateEnums' message, naming the field, the value, and the
	// accepted enum members.
	Reason string `json:"reason"`
}

// InvalidAxis is a loaded nib whose assignment axes violate its type's axis
// rule (nibtypes.ValidateAxes: a milestone-typed nib carrying a `milestone:`
// or `area:` value). The rule is strict on the write paths only, so the value
// loads as written (see loadFromDisk's diagnostic warning) — and then every
// update that leaves both the type and the offending keys as they are is
// refused. `--clear milestone` and `--clear area` apply to the subject ABOVE
// the guards, so they clear the axis instead of being refused by it; see
// ClearAxesCommand for why that escape is ONE command.
//
// Retyping also reconciles the two and carries no command: it is refused while
// nibs are still assigned to the milestone, and again when the `area:` it would
// keep is undeclared.
type InvalidAxis struct {
	NibID string `json:"nib_id"`
	// Path is relative to the nibs root with forward slashes, like nib.Path.
	Path string `json:"path"`
	// Reason is ValidateAxes' message, naming the axis the type refuses.
	Reason string `json:"reason"`
	// Axes names EVERY axis key the nib carries that its type refuses, in
	// front-matter order. Reason speaks for the first of them only; the escape
	// has to drop them all at once.
	Axes []string `json:"axes"`
}

// UndeclaredArea is a loaded nib whose `area:` names a path the store's areas
// vocabulary does not declare. The value loads, lists, filters and renders
// exactly as written (read-tolerance is deliberate, see Core.ValidateArea), so
// this finding is the only surface that names the file.
//
// Do not fold the rule into ValidateEnums: two of its callers are not write
// paths — loadFromDisk and CheckAllLinks — so the LOADER would start warning.
//
// `--fix` applies neither remediation — it would have to invent an area or
// decide the assignment should go. A store that declares NO areas produces none
// of these; see Core.CheckAllLinks.
type UndeclaredArea struct {
	NibID string `json:"nib_id"`
	// Path is relative to the nibs root with forward slashes, like nib.Path.
	Path string `json:"path"`
	// Area is the undeclared value as the file spells it, already rendered by
	// config for a message (control characters neutralized, length bounded).
	Area string `json:"area"`
	// Declared is the vocabulary the store DOES declare, as config.AreaError
	// carries it — bounded the same way.
	Declared string `json:"declared"`
}

// AxisKeysNoun names the offending keys in prose, matching what
// ClearAxesCommand clears, so a message never says "key" beside a command that
// drops two.
func AxisKeysNoun(axes []string) string {
	if len(axes) > 1 {
		return "both axis keys"
	}
	return "the axis key"
}

// ClearAxesCommand renders the one command that drops every axis key a nib's
// type refuses. Keep it one command rather than one per key: on a nib carrying
// both, `--clear milestone` alone is still refused for the area and `--clear
// area` alone for the milestone, so two alternatives would be two commands of
// which neither works. nibID is expected already rendered for the surface it is
// printed on.
func ClearAxesCommand(nibID string, axes []string) string {
	var b strings.Builder
	b.WriteString("nibs set ")
	b.WriteString(nibID)
	for _, axis := range axes {
		b.WriteString(" --clear ")
		b.WriteString(axis)
	}
	return b.String()
}

// InvalidHierarchy is a loaded nib whose parent's type the hierarchy rules
// refuse (nibtypes.ValidateParentType: a milestone parented under a milestone,
// a feature under a task). The rule is strict on the write paths that set a
// parent or change a type, so an offender reaches the store through a hand edit
// or as the leftover of an earlier rule set — the v2 migration leaves illegal
// nests untouched — and then loads, lists and renders like any other nib. This
// finding is the one surface that names it.
type InvalidHierarchy struct {
	NibID string `json:"nib_id"`
	// Path is relative to the nibs root with forward slashes, like nib.Path.
	Path string `json:"path"`
	// ParentID is the resolved parent's full id, however the file spells it.
	ParentID string `json:"parent_id"`
	// ChildType and ParentType are the two nibs' effective types — a type-less
	// nib is judged as the default type, the way every write path judges it.
	ChildType  string `json:"child_type"`
	ParentType string `json:"parent_type"`
	// Allowed is the set of parent types that WOULD be legal for the child,
	// empty when the child type takes no parent at all.
	Allowed []string `json:"allowed_parents,omitempty"`
	// Reason is the HierarchyError's message, naming the rule in prose.
	Reason string `json:"reason"`
}

// InvalidMilestoneTarget is a loaded nib whose `milestone:` names a nib that
// EXISTS but is not milestone-typed (the same rule
// membership.ResolvedMilestoneID applies). The rule is strict on the write
// paths that assign — `nibs set <id> --milestone <feature-id>` is refused — so
// an offender reaches the store through a hand edit or as data predating the
// rule, and then loads and lists like any other nib while the assignment
// resolves to nothing: no membership, no milestone queue, yet its `milestone:`
// field reads back the bad target.
//
// A MILESTONE-typed nib carrying `milestone:` is not reported here — see the
// exclusion where the finding is raised.
type InvalidMilestoneTarget struct {
	NibID string `json:"nib_id"`
	// Path is relative to the nibs root with forward slashes, like nib.Path.
	Path string `json:"path"`
	// Target is the resolved full id of the target, however the file spells it.
	Target string `json:"target"`
	// TargetType is the target's effective type — a type-less nib is judged as
	// the default type, the way every write path judges it.
	TargetType string `json:"target_type"`
}

// AssignmentConflict is a loaded nib assigned to a milestone while one of its
// structural ancestors is assigned too — the shape decision 1.2 rules out (a
// nib and one of its ancestors are never both assigned). The rule is strict on
// the write paths that assign or reparent, so an offender reaches the store
// through a hand edit or data that predates the rule, and then schedules like
// any other nib — counted in BOTH queues. One finding per nib, naming its
// NEAREST assigned ancestor; a deeper conflict shows up as that ancestor's own
// finding.
type AssignmentConflict struct {
	NibID string `json:"nib_id"`
	// Path is relative to the nibs root with forward slashes, like nib.Path.
	Path string `json:"path"`
	// Milestone is the resolved full id of the nib's own assignment.
	Milestone string `json:"milestone"`
	// AncestorID is the nearest assigned ancestor's full id, and
	// AncestorMilestone the resolved full id of ITS assignment.
	AncestorID        string `json:"ancestor_id"`
	AncestorMilestone string `json:"ancestor_milestone"`
}

// ClosedMilestoneQueue is a loaded MILESTONE carrying a status that RELEASES
// its dependents (config.StatusReleasesDependents — today completed and
// scrapped) while open work is still assigned to its queue: decision 1.5's
// refusal standing in the store as a fact.
//
// The close, the assignment and the type flip each refuse it (graph's
// MilestoneQueueOpenError, MilestoneReleasedError and MilestoneRetypeError,
// plus `nibs close`'s own gate), and a refusal only ever sees the write it
// refuses — so once the state stands this is the one surface that names it.
// Deferred is an offense on NEITHER side: a deferred MILESTONE holds its queue
// on purpose, and a deferred MEMBER is closed and does not hold it open.
type ClosedMilestoneQueue struct {
	NibID string `json:"nib_id"`
	// Path is relative to the nibs root with forward slashes, like nib.Path.
	Path string `json:"path"`
	// Status is the releasing status the milestone carries.
	Status string `json:"status"`
	// Open is the open queue entries' full ids, in the same queue order
	// graph.OpenQueueEntries yields, so report and refusal name one set.
	Open []string `json:"open"`
}

// NearMissKey is a loaded nib carrying an unknown front-matter key whose
// spelling is a near miss of a modeled key (a dash for the underscore, a case
// variant, stray underscores — the rule is nib.ModeledKeyResembling's). The key
// parses losslessly into Extra and renders back — read tolerance is unchanged —
// but no filter or query consults Extra, so the value the author meant to set
// has no effect. This finding is the one surface that names it.
type NearMissKey struct {
	NibID string `json:"nib_id"`
	// Path is relative to the nibs root with forward slashes, like nib.Path.
	Path string `json:"path"`
	// Key is the unknown key exactly as the file spells it.
	Key string `json:"key"`
	// Modeled is the modeled front-matter key the spelling resembles.
	Modeled string `json:"modeled"`
}

// LinkCheckResult contains all nib integrity issues found.
//
// A finding derivable from the loaded nibs alone is filled by
// CheckAllLinksInMap; one needing the config, or evidence of what did NOT load,
// is added by Core.CheckAllLinks. Each field below says which, and the field
// order is the `--json` key order.
type LinkCheckResult struct {
	BrokenLinks     []BrokenLink     `json:"broken_links"`
	SelfLinks       []SelfLink       `json:"self_links"`
	Cycles          []Cycle          `json:"cycles"`
	BrokenDocuments []BrokenDocument `json:"broken_documents"`

	// Load-time integrity — Core.CheckAllLinks only; the evidence is the files
	// that did not load.
	UnparseableFiles []UnparseableFile `json:"unparseable_files"`
	DuplicateIDs     []DuplicateID     `json:"duplicate_ids"`

	// Field integrity — Core.CheckAllLinks only; needs the config's enum tables.
	InvalidEnums []InvalidEnum `json:"invalid_enums"`

	// Axis integrity — config-free (nibtypes.ValidateAxes).
	InvalidAxes []InvalidAxis `json:"invalid_axes"`

	// Area integrity — Core.CheckAllLinks only; needs the areas vocabulary.
	UndeclaredAreas []UndeclaredArea `json:"undeclared_areas"`

	// Hierarchy integrity — config-free (nibtypes.ValidateParentType).
	InvalidHierarchies []InvalidHierarchy `json:"invalid_hierarchies"`

	// Milestone-target integrity — config-free.
	InvalidMilestoneTargets []InvalidMilestoneTarget `json:"invalid_milestone_targets"`

	// Assignment integrity — config-free.
	AssignmentConflicts []AssignmentConflict `json:"assignment_conflicts"`

	// Key integrity — config-free.
	NearMissKeys []NearMissKey `json:"near_miss_keys"`

	// Queue integrity — Core.CheckAllLinks only; which statuses close and which
	// release their dependents is the config's answer. The derivation stays pure
	// in closedMilestoneQueuesInMap; only the two role predicates cross.
	ClosedMilestoneQueues []ClosedMilestoneQueue `json:"closed_milestone_queues"`
}

// HasIssues returns true if any issues were found.
func (r *LinkCheckResult) HasIssues() bool {
	return r.TotalIssues() > 0
}

// TotalIssues returns the total count of all issues.
func (r *LinkCheckResult) TotalIssues() int {
	return len(r.BrokenLinks) + len(r.SelfLinks) + len(r.Cycles) + len(r.BrokenDocuments) + r.LoadIssues() + r.EnumIssues() + r.AxisIssues() + r.AreaIssues() + r.HierarchyIssues() + r.AssignmentIssues() + r.MilestoneTargetIssues() + r.NearMissIssues() + r.QueueIssues()
}

// LoadIssues returns the count of load-time integrity issues alone. `nibs check`
// renders each category under its own heading; that is what this counter and the
// ones below it are for.
func (r *LinkCheckResult) LoadIssues() int {
	return len(r.UnparseableFiles) + len(r.DuplicateIDs)
}

// EnumIssues returns the count of out-of-enum field findings alone.
func (r *LinkCheckResult) EnumIssues() int {
	return len(r.InvalidEnums)
}

// AxisIssues returns the count of axis-rule findings alone.
func (r *LinkCheckResult) AxisIssues() int {
	return len(r.InvalidAxes)
}

// AreaIssues returns the count of undeclared-area findings alone.
func (r *LinkCheckResult) AreaIssues() int {
	return len(r.UndeclaredAreas)
}

// HierarchyIssues returns the count of hierarchy-rule findings alone.
func (r *LinkCheckResult) HierarchyIssues() int {
	return len(r.InvalidHierarchies)
}

// AssignmentIssues returns the count of assignment-exclusivity findings alone.
func (r *LinkCheckResult) AssignmentIssues() int {
	return len(r.AssignmentConflicts)
}

// MilestoneTargetIssues returns the count of invalid-milestone-target findings
// alone.
func (r *LinkCheckResult) MilestoneTargetIssues() int {
	return len(r.InvalidMilestoneTargets)
}

// NearMissIssues returns the count of near-miss key findings alone.
func (r *LinkCheckResult) NearMissIssues() int {
	return len(r.NearMissKeys)
}

// QueueIssues returns the count of closed-milestone-queue findings alone.
func (r *LinkCheckResult) QueueIssues() int {
	return len(r.ClosedMilestoneQueues)
}

// CheckAllLinksInMap validates all links across all nibs.
// When projectRoot is empty, document filesystem checks are skipped.
// This is a pure function that operates on a map of nibs without locking.
//
// Resolve a parent, milestone or blockedBy target through normalizeIDInMap —
// the exact id, then configPrefix prepended — never by a bare map lookup: a
// short-form target that resolves is not broken, and Core.FixBrokenLinks repeats
// these checks before it deletes. A target that resolves
// back to the nib holding it is a self link, however it was spelled; one that
// resolves to nothing is reported under the spelling the file holds.
func CheckAllLinksInMap(nibs map[string]*nib.Nib, projectRoot, configPrefix string) *LinkCheckResult {
	result := &LinkCheckResult{
		BrokenLinks:             []BrokenLink{},
		SelfLinks:               []SelfLink{},
		Cycles:                  []Cycle{},
		BrokenDocuments:         []BrokenDocument{},
		UnparseableFiles:        []UnparseableFile{},
		DuplicateIDs:            []DuplicateID{},
		InvalidEnums:            []InvalidEnum{},
		InvalidAxes:             []InvalidAxis{},
		UndeclaredAreas:         []UndeclaredArea{},
		InvalidHierarchies:      []InvalidHierarchy{},
		InvalidMilestoneTargets: []InvalidMilestoneTarget{},
		AssignmentConflicts:     []AssignmentConflict{},
		NearMissKeys:            []NearMissKey{},
		ClosedMilestoneQueues:   []ClosedMilestoneQueue{},
	}

	for _, b := range nibs {
		if b.Parent != "" {
			fullID, ok := normalizeIDInMap(nibs, b.Parent, configPrefix)
			switch {
			case !ok:
				result.BrokenLinks = append(result.BrokenLinks, BrokenLink{
					NibID:    b.ID,
					LinkType: "parent",
					Target:   b.Parent,
				})
			case fullID == b.ID:
				result.SelfLinks = append(result.SelfLinks, SelfLink{
					NibID:    b.ID,
					LinkType: "parent",
				})
			}
		}

		// A milestone target resolves as a parent does, then is judged once more
		// on its TYPE: only a milestone-typed one confers membership, so a
		// resolvable non-milestone target leaves the nib in no queue while its
		// `milestone:` field still reads back.
		if b.Milestone != "" {
			fullID, ok := normalizeIDInMap(nibs, b.Milestone, configPrefix)
			switch {
			case !ok:
				result.BrokenLinks = append(result.BrokenLinks, BrokenLink{
					NibID:    b.ID,
					LinkType: "milestone",
					Target:   b.Milestone,
				})
			case fullID == b.ID:
				result.SelfLinks = append(result.SelfLinks, SelfLink{
					NibID:    b.ID,
					LinkType: "milestone",
				})
			// A MILESTONE-typed subject is excluded: InvalidAxes already names
			// it, and its whole `milestone:` key has to go — naming the target's
			// type here would send the reader to repoint a key they must delete.
			case nibs[fullID].EffectiveType() != "milestone" && b.EffectiveType() != "milestone":
				result.InvalidMilestoneTargets = append(result.InvalidMilestoneTargets, InvalidMilestoneTarget{
					NibID:      b.ID,
					Path:       b.Path,
					Target:     fullID,
					TargetType: nibs[fullID].EffectiveType(),
				})
			}
		}

		if projectRoot != "" {
			for _, docPath := range b.Documents {
				absPath := filepath.Join(projectRoot, docPath)
				if _, err := os.Stat(absPath); os.IsNotExist(err) {
					result.BrokenDocuments = append(result.BrokenDocuments, BrokenDocument{
						NibID: b.ID,
						Path:  docPath,
					})
				}
			}
		}

		// blocked_by only: blocking is derived, never persisted.
		for _, blocker := range b.BlockedBy {
			fullID, ok := normalizeIDInMap(nibs, blocker, configPrefix)
			switch {
			case !ok:
				result.BrokenLinks = append(result.BrokenLinks, BrokenLink{
					NibID:    b.ID,
					LinkType: "blocked_by",
					Target:   blocker,
				})
			case fullID == b.ID:
				result.SelfLinks = append(result.SelfLinks, SelfLink{
					NibID:    b.ID,
					LinkType: "blocked_by",
				})
			}
		}
	}

	// The loop above walks the map, so sorting is what makes the report and the
	// --json envelope stable run to run. Three of the four need a compound key:
	// one nib can hold a broken parent, a broken milestone and several broken
	// blockers at once, and name several missing documents, so an id-only key
	// would leave those entries tied. A nib carries exactly one milestone.
	sort.Slice(result.BrokenLinks, func(i, j int) bool {
		x, y := result.BrokenLinks[i], result.BrokenLinks[j]
		if x.NibID != y.NibID {
			return x.NibID < y.NibID
		}
		if x.LinkType != y.LinkType {
			return x.LinkType < y.LinkType
		}
		return x.Target < y.Target
	})
	sort.Slice(result.SelfLinks, func(i, j int) bool {
		x, y := result.SelfLinks[i], result.SelfLinks[j]
		if x.NibID != y.NibID {
			return x.NibID < y.NibID
		}
		return x.LinkType < y.LinkType
	})
	sort.Slice(result.BrokenDocuments, func(i, j int) bool {
		x, y := result.BrokenDocuments[i], result.BrokenDocuments[j]
		if x.NibID != y.NibID {
			return x.NibID < y.NibID
		}
		return x.Path < y.Path
	})
	sort.Slice(result.InvalidMilestoneTargets, func(i, j int) bool {
		return result.InvalidMilestoneTargets[i].NibID < result.InvalidMilestoneTargets[j].NibID
	})

	// Only these two link types need cycle checks: blocking is derived from
	// blocked_by, and milestone is a flat assignment nothing traverses.
	for _, linkType := range []string{"blocked_by", "parent"} {
		cycles := FindCyclesInMap(nibs, linkType)
		result.Cycles = append(result.Cycles, cycles...)
	}

	// Per-nib findings, sorted by id and near-miss keys by key: map order would
	// otherwise shuffle the report run to run.
	ids := make([]string, 0, len(nibs))
	for id := range nibs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		b := nibs[id]
		// Axis integrity: see InvalidAxis.
		if err := nibtypes.ValidateAxes(b.EffectiveType(), b.Milestone, b.Area); err != nil {
			result.InvalidAxes = append(result.InvalidAxes, InvalidAxis{
				NibID:  b.ID,
				Path:   b.Path,
				Reason: err.Error(),
				Axes:   nibtypes.RefusedAxes(b.EffectiveType(), b.Milestone, b.Area),
			})
		}
		// Hierarchy integrity: see InvalidHierarchy. An unresolvable parent is
		// already a broken link (its type is unknowable) and a self parent is
		// already a self link, so neither is judged again here.
		if b.Parent != "" {
			if parentID, ok := normalizeIDInMap(nibs, b.Parent, configPrefix); ok && parentID != b.ID {
				parent := nibs[parentID]
				if err := nibtypes.ValidateParentType(b.EffectiveType(), parent.EffectiveType()); err != nil {
					finding := InvalidHierarchy{
						NibID:      b.ID,
						Path:       b.Path,
						ParentID:   parentID,
						ChildType:  b.EffectiveType(),
						ParentType: parent.EffectiveType(),
						Reason:     err.Error(),
					}
					var herr *nibtypes.HierarchyError
					if errors.As(err, &herr) {
						finding.Allowed = herr.Allowed
					}
					result.InvalidHierarchies = append(result.InvalidHierarchies, finding)
				}
			}
		}
		// Assignment integrity: see AssignmentConflict. The walk reads RESOLVED
		// assignments, so a dangling or non-milestone one conflicts with
		// nothing — each is its own finding above (a BrokenLink, an
		// InvalidMilestoneTarget), neither conferring the membership
		// exclusivity is about. A visited set bounds the walk on a hand-edited
		// parent cycle.
		if ms := resolvedMilestoneInMap(nibs, b, configPrefix); ms != "" {
			visited := map[string]bool{b.ID: true}
			for cur := b; cur.Parent != ""; {
				parentID, ok := normalizeIDInMap(nibs, cur.Parent, configPrefix)
				if !ok || visited[parentID] {
					break
				}
				visited[parentID] = true
				parent := nibs[parentID]
				if ancestorMS := resolvedMilestoneInMap(nibs, parent, configPrefix); ancestorMS != "" {
					result.AssignmentConflicts = append(result.AssignmentConflicts, AssignmentConflict{
						NibID:             b.ID,
						Path:              b.Path,
						Milestone:         ms,
						AncestorID:        parent.ID,
						AncestorMilestone: ancestorMS,
					})
					break
				}
				cur = parent
			}
		}
		// Key integrity: see NearMissKey.
		keys := make([]string, 0, len(b.Extra))
		for key := range b.Extra {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if modeled, ok := nib.ModeledKeyResembling(key); ok {
				result.NearMissKeys = append(result.NearMissKeys, NearMissKey{
					NibID:   b.ID,
					Path:    b.Path,
					Key:     key,
					Modeled: modeled,
				})
			}
		}
	}

	return result
}

// resolvedMilestoneInMap returns b's `milestone:` target when it resolves (exact
// id, then the prefix prepended) to a milestone-typed nib, "" otherwise.
//
// It does NOT apply membership.ResolvedMilestoneID's subject test, which answers
// "" for a milestone-typed b as well.
func resolvedMilestoneInMap(nibs map[string]*nib.Nib, b *nib.Nib, configPrefix string) string {
	if b.Milestone == "" {
		return ""
	}
	targetID, ok := normalizeIDInMap(nibs, b.Milestone, configPrefix)
	if !ok {
		return ""
	}
	target := nibs[targetID]
	if target == nil || target.EffectiveType() != "milestone" {
		return ""
	}
	return targetID
}

// closedMilestoneQueuesInMap derives every ClosedMilestoneQueue finding over the
// map: each milestone whose status releases its dependents, paired with the open
// work still assigned to it, sorted by milestone id.
//
// This is graph.OpenQueueEntries' rule over a different substrate — DIRECT
// assignees only, milestone-typed members skipped, queue order from
// nib.SortByMilestoneOrder. Resolve the assignment by CALLING
// membership.ResolvedMilestoneID, never by restating its clauses, and take no
// configPrefix: expanding a shorthand id here would part the report from the
// refusal it mirrors. No store can present the divergence that would expose —
// Load canonicalizes stored link ids (see canonicalize.go) — so the call
// couples the two derivations rather than fixing an observed defect.
//
// isClosed and releasesDependents are NOT interchangeable: a deferred member is
// closed and does not hold its milestone open, while a deferred milestone
// releases nothing and is no offense at all. The caller supplies them because a
// pure function over a map cannot reach the project config.
func closedMilestoneQueuesInMap(nibs map[string]*nib.Nib, isClosed, releasesDependents func(string) bool) []ClosedMilestoneQueue {
	lookup := func(id string) *nib.Nib { return nibs[id] }
	open := make(map[string][]*nib.Nib)
	for _, b := range nibs {
		if b.EffectiveType() == "milestone" || isClosed(b.Status) {
			continue
		}
		if ms := membership.ResolvedMilestoneID(b, lookup); ms != "" {
			open[ms] = append(open[ms], b)
		}
	}

	var findings []ClosedMilestoneQueue
	for id, members := range open {
		m := nibs[id]
		if !releasesDependents(m.Status) {
			continue
		}
		nib.SortByMilestoneOrder(members)
		ids := make([]string, len(members))
		for i, b := range members {
			ids[i] = b.ID
		}
		findings = append(findings, ClosedMilestoneQueue{NibID: m.ID, Path: m.Path, Status: m.Status, Open: ids})
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].NibID < findings[j].NibID })
	return findings
}

// CheckAllLinks validates all links across all nibs and adds the load-time
// integrity problems retained from the last Load.
//
// Those two categories cannot be derived from c.nibs — their evidence is the
// files that did NOT make it in. They describe the last LOAD: a long-lived
// process whose watcher has since reconciled a file keeps reporting what its
// Load saw, until the next Load.
//
// The retained slices are copied out rather than shared: this holds only a read
// lock, so handing out the stored backing array would let a caller mutate Core
// state without one.
func (c *Core) CheckAllLinks() *LinkCheckResult {
	c.mu.RLock()
	defer c.mu.RUnlock()
	projectRoot := filepath.Dir(c.root)
	result := CheckAllLinksInMap(c.nibs, projectRoot, c.configPrefix())
	result.UnparseableFiles = append(result.UnparseableFiles, c.unparseableFiles...)
	result.DuplicateIDs = append(result.DuplicateIDs, c.duplicateIDs...)

	// Field integrity: the enum tables are the config's, which the pure map
	// function does not carry. Sorted by id, or map order shuffles the report.
	ids := make([]string, 0, len(c.nibs))
	for id := range c.nibs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := c.ValidateEnums(c.nibs[id]); err != nil {
			result.InvalidEnums = append(result.InvalidEnums, InvalidEnum{NibID: id, Reason: err.Error()})
		}
	}

	// Area integrity: here for the same reason as the enums — the areas
	// vocabulary is the config's answer. See UndeclaredArea.
	//
	// A store declaring NO areas is exempt wholesale. Every nib it would name is
	// a write dead end anyway (Core.ValidateArea refuses a stored value whether
	// or not a vocabulary exists), and the answer for such a store is one config
	// edit, not N findings. The cost: no read surface names those nibs, so the
	// dead end shows up only on an attempted write.
	if c.config != nil && c.Areas().Declared() {
		for _, id := range ids {
			b := c.nibs[id]
			// A type that refuses `area:` outright is already an InvalidAxis
			// finding whose remedy is to drop the key, so naming a declared
			// value beside it would prescribe one this nib may not carry. Axis
			// before area, as preValidateSubject and closeMemberOwnGuards order
			// it; the milestone axis is passed empty so only the area decides.
			if nibtypes.ValidateAxes(b.EffectiveType(), "", b.Area) != nil {
				continue
			}
			var areaErr *config.AreaError
			if errors.As(c.ValidateArea(b), &areaErr) {
				result.UndeclaredAreas = append(result.UndeclaredAreas, UndeclaredArea{
					NibID: id, Path: b.Path, Area: areaErr.Path, Declared: areaErr.Declared,
				})
			}
		}
	}

	// Queue integrity: the status ROLES are the config's answer. The derivation
	// stays pure; only the two predicates cross.
	result.ClosedMilestoneQueues = append(result.ClosedMilestoneQueues,
		closedMilestoneQueuesInMap(c.nibs, c.closedStatusPredicate(), c.releasesDependentsPredicate())...)
	return result
}

// LoadDiagnostics returns the load-time integrity problems retained from the
// last Load: files on disk that are NOT answerable through the store (skipped
// unparseable files, and losers of id collisions). Ask it before rewriting a
// store — `nibs migrate` refuses to run on one that did not load cleanly, since
// migrating around a skipped file can silently drop edges to it. Slices are
// copied out, matching CheckAllLinks.
func (c *Core) LoadDiagnostics() ([]UnparseableFile, []DuplicateID) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]UnparseableFile(nil), c.unparseableFiles...),
		append([]DuplicateID(nil), c.duplicateIDs...)
}

// FindCyclesInMap detects all cycles for a specific link type using DFS.
// This is a pure function that operates on a map of nibs without locking.
func FindCyclesInMap(nibs map[string]*nib.Nib, linkType string) []Cycle {
	var cycles []Cycle
	visited := make(map[string]bool)
	inStack := make(map[string]bool)
	seenCycles := make(map[string]bool)

	var dfs func(id string, path []string)
	dfs = func(id string, path []string) {
		if inStack[id] {
			cycleStart := -1
			for i, p := range path {
				if p == id {
					cycleStart = i
					break
				}
			}
			if cycleStart >= 0 {
				// Canonicalize before STORING, not just before keying: this
				// Path is what `nibs check` renders and --json serializes, and
				// the walk's own rotation is an artifact of map order.
				cyclePath := canonicalCyclePath(append(path[cycleStart:], id))
				key := canonicalCycleKey(cyclePath)
				if !seenCycles[key] {
					seenCycles[key] = true
					cycles = append(cycles, Cycle{
						LinkType: linkType,
						Path:     cyclePath,
					})
				}
			}
			return
		}

		if visited[id] {
			return
		}

		visited[id] = true
		inStack[id] = true

		b, ok := nibs[id]
		if ok {
			var targets []string
			switch linkType {
			case "parent":
				if b.Parent != "" {
					targets = []string{b.Parent}
				}
			case "blocked_by":
				targets = b.BlockedBy
			}

			for _, target := range targets {
				// Skip self-references (they're tracked separately as SelfLinks)
				if target == id {
					continue
				}
				dfs(target, append(path, id))
			}
		}

		inStack[id] = false
	}

	// Sorted, because `visited` spans the whole walk: a cycle whose nodes were
	// finished under an earlier root is never explored, so root order decides
	// WHICH cycles are reported and not merely in what order. It is the only
	// nondeterminism left — BlockedBy is a slice and adjacency is a keyed lookup.
	//
	// That buys determinism, not completeness: the reported set is a SUBSET of
	// the elementary cycles. Existence is never missed — a directed graph has a
	// cycle exactly when some DFS finds a back edge — so fixing the listed loop
	// surfaces the next one on the next run.
	roots := slices.Sorted(maps.Keys(nibs))
	for _, id := range roots {
		if !visited[id] {
			dfs(id, nil)
		}
	}

	// No two entries share a Path: the dedup key above is derived from that same
	// path, so a repeat would already have been dropped. Comparing whole paths is
	// therefore a total order, and sort.Slice's instability cannot show through.
	sort.Slice(cycles, func(i, j int) bool {
		return slices.Compare(cycles[i].Path, cycles[j].Path) < 0
	})

	return cycles
}

// canonicalCyclePath rotates a cycle to start at its smallest id, so one loop
// has one rendering no matter which of its nodes a walk entered at.
//
// The input closes back on its start (the last element repeats the first) and so
// does the result; `nibs check` renders the loop as "a → b → a".
func canonicalCyclePath(path []string) []string {
	if len(path) <= 1 {
		return path
	}

	cycle := path[:len(path)-1]

	minIdx := 0
	for i, id := range cycle {
		if id < cycle[minIdx] {
			minIdx = i
		}
	}

	rotated := make([]string, 0, len(cycle)+1)
	for i := range cycle {
		rotated = append(rotated, cycle[(minIdx+i)%len(cycle)])
	}
	return append(rotated, rotated[0])
}

// canonicalCycleKey keys a cycle for duplicate detection, from its canonical
// rotation.
func canonicalCycleKey(path []string) string {
	if len(path) <= 1 {
		return ""
	}

	rotated := canonicalCyclePath(path)
	return strings.Join(rotated[:len(rotated)-1], "->")
}

// RemoveLinksTo removes every parent, milestone and blockedBy link that
// RESOLVES to the given target from all nibs. Returns the number of links
// removed.
//
// Both ends of the comparison are spelling-independent. The TARGET resolves
// through the same exact-id-then-prefix-prepended rule as Core.Get
// (normalizeIDInMap); a STORED link matches when IT resolves to that same nib,
// by the same rule. A literal equality against the id AS GIVEN matches as well,
// which is what strips links to a target that resolves to nothing — an
// unresolvable id is carried verbatim, so verbatim is the only way to name it.
//
// The legacy v0 Blocking field is left untouched, matching CheckAllLinksInMap
// and FixBrokenLinks: in v1+ blocking is derived from other nibs' BlockedBy and
// is not a link source (see nib.Nib.Blocking). Clearing it belongs to the
// v0→v1 migration.
//
// An empty target names no nib and is refused up front. That refusal is
// load-bearing, not a shortcut for the O(N) walk: `""` DOES resolve in a store
// holding a nib whose id is exactly the configured prefix — a hand-written
// `nibs-.md` — so without it an empty target would strip that nib's incoming
// links. Separately, pointsAtTarget rejects an empty LINK id.
//
// Copy-on-write, per the canonical live-pointer invariant (see
// NibReader.GetSnapshot in internal/graph/interfaces.go): the changed fields
// here are non-Path — Parent, a torn string, and BlockedBy, a memory-unsafe torn
// slice header — so a changed nib is cloned, persisted and reinstalled under its
// key. Ranging over c.nibs while reassigning an existing key's value is safe in
// Go.
//
// CONCURRENCY: this whole-store sweep takes the per-operation cross-process
// write lock the single-nib writers take — c.mu, then the flock (see
// Core.acquireWriteLock) — which excludes `nibs config set-prefix` for its
// duration. It therefore cannot be called under AcquireStoreLock, whose flock is
// the same per-descriptor one (see flock.go).
//
// The write is the NON-CREATING one (updateOnDiskDeferDirSync), for the reason
// the other whole-store sweep states — see Core.rewriteAreaAssignmentsLocked.
func (c *Core) RemoveLinksTo(targetID string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Refused, not optimized away — see the doc comment.
	if targetID == "" {
		return 0, nil
	}

	unlock, lockErr := c.acquireWriteLock()
	if lockErr != nil {
		return 0, lockErr
	}
	defer func() { _ = unlock() }()

	configPrefix := c.configPrefix()
	fullID, resolved := c.normalizeIDForLookupLocked(targetID)

	pointsAtTarget := func(linkID string) bool {
		if linkID == "" {
			return false // an unset Parent or Milestone is not a link
		}
		if linkID == targetID {
			return true
		}
		if !resolved {
			return false
		}
		linkFullID, ok := normalizeIDInMap(c.nibs, linkID, configPrefix)
		return ok && linkFullID == fullID
	}

	// One directory fsync per directory the sweep touched, not one per nib.
	// Deferred so an aborted sweep still flushes what it did write: the first
	// error returns with the earlier files already renamed into place.
	var pending fsutil.DirSyncBatch
	defer pending.Flush()

	removed := 0
	for id, b := range c.nibs {
		// Detect changes by READING the stored pointer only — never mutate it,
		// so an unchanged nib skips the Clone() below.
		removeParent := pointsAtTarget(b.Parent)
		removeMilestone := pointsAtTarget(b.Milestone)
		removeBlocker := slices.ContainsFunc(b.BlockedBy, pointsAtTarget)

		if !removeParent && !removeMilestone && !removeBlocker {
			continue
		}

		clone := b.Clone()
		if removeParent {
			clone.Parent = ""
			removed++
		}
		if removeMilestone {
			clone.Milestone = ""
			removed++
		}
		if removeBlocker {
			before := len(clone.BlockedBy)
			clone.BlockedBy = slices.DeleteFunc(clone.BlockedBy, pointsAtTarget)
			removed += before - len(clone.BlockedBy)
		}

		dir, err := c.updateOnDiskDeferDirSync(clone)
		pending.Add(dir)
		if err != nil {
			return removed, err
		}
		c.nibs[id] = clone
	}

	return removed, nil
}

// SkippedIDSet builds the set of ids whose file is present on disk but was not
// loaded (unparseable/unreadable — see UnparseableFile), so a link naming one of
// them is unresolvable-for-now rather than broken. Each id is entered under BOTH
// spellings a link may hold — as the filename derives it, and with the
// configured prefix trimmed — the same two normalizeIDInMap resolves, so a
// consumer tests a link target with one plain map probe.
//
// Build from this, not from a private copy: Core.FixBrokenLinks' keep-don't-erase
// gate and cmd/check's report partition both do, so what `--fix` preserves and
// what the report claims cannot disagree.
func SkippedIDSet(unparseable []UnparseableFile, prefix string) map[string]bool {
	if len(unparseable) == 0 {
		return nil
	}
	skipped := make(map[string]bool, 2*len(unparseable))
	for _, uf := range unparseable {
		if uf.NibID == "" {
			continue // filename yields no id, so no link can name it
		}
		skipped[uf.NibID] = true
		if short := strings.TrimPrefix(uf.NibID, prefix); short != "" {
			skipped[short] = true
		}
	}
	return skipped
}

// skippedIDsLocked returns SkippedIDSet for the files that failed THIS load.
// Must be called with c.mu held.
//
// Their nibs are absent from c.nibs, so every link naming one resolves to
// nothing and CheckAllLinks reports it broken. That report is correct — the link
// cannot be followed right now — but do not let `--fix` delete such a link: the
// target is on disk needing a YAML repair, and repairing it does NOT bring back
// an edge already erased. `nibs migrate` refuses to run at all while a file is
// unparseable, for the same reason.
func (c *Core) skippedIDsLocked() map[string]bool {
	return SkippedIDSet(c.unparseableFiles, c.configPrefix())
}

// FixBrokenLinks removes all broken links (links to non-existent nibs) and self-references.
// Returns the number of issues fixed.
//
// It restates the parent, milestone, blockedBy and document checks
// CheckAllLinksInMap makes, resolving each link target through normalizeIDInMap
// the same way — but it KEEPS a link whose target is only skipped this load (see
// skippedIDsLocked), so `nibs check --fix` removes a SUBSET of the broken links,
// self links and broken documents `nibs check` reported; cmd/check partitions
// its report the same way. The other categories are left untouched and printed
// as not auto-fixable: cycles, and the two load-time conditions.
//
// A link that resolves is left exactly as stored: nothing here rewrites a
// short id into its full form.
//
// Copy-on-write for the same reason as RemoveLinksTo: mutate a clone and
// reinstall it rather than editing the stored pointer in place, so no off-lock
// reader ever sees a stored pointer's non-Path fields torn mid-write. See the
// canonical live-pointer / copy-on-write invariant at NibReader.GetSnapshot
// (internal/graph/interfaces.go). Documents is made copy-on-write here too, for
// the same discipline.
//
// CONCURRENCY and the non-creating write are RemoveLinksTo's — read them there.
// This sweep holds the lock longer: it stats one file per document link on top
// of the walk of every nib, so a store with many document links parks concurrent
// writers for that long.
func (c *Core) FixBrokenLinks() (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	unlock, lockErr := c.acquireWriteLock()
	if lockErr != nil {
		return 0, lockErr
	}
	defer func() { _ = unlock() }()

	projectRoot := filepath.Dir(c.root)
	configPrefix := c.configPrefix()
	skipped := c.skippedIDsLocked()

	// One directory fsync per directory the sweep touched, not one per nib.
	// Deferred so an aborted sweep still flushes what it did write: the first
	// error returns with the earlier files already renamed into place.
	var pending fsutil.DirSyncBatch
	defer pending.Flush()

	fixed := 0
	for id, b := range c.nibs {
		// Detect changes by READING the stored pointer only — never mutate it.

		// Dropped when the parent resolves back to this nib (self) or to nothing
		// (broken), but NOT when its file is merely skipped this load.
		fixParent := false
		if b.Parent != "" {
			fullID, ok := normalizeIDInMap(c.nibs, b.Parent, configPrefix)
			fixParent = (!ok && !skipped[b.Parent]) || fullID == b.ID
		}

		fixMilestone := false
		if b.Milestone != "" {
			fullID, ok := normalizeIDInMap(c.nibs, b.Milestone, configPrefix)
			fixMilestone = (!ok && !skipped[b.Milestone]) || fullID == b.ID
		}

		var newBlockedBy []string
		for _, blocker := range b.BlockedBy {
			fullID, ok := normalizeIDInMap(c.nibs, blocker, configPrefix)
			if (!ok && !skipped[blocker]) || fullID == b.ID {
				continue
			}
			newBlockedBy = append(newBlockedBy, blocker)
		}
		blockedRemoved := len(b.BlockedBy) - len(newBlockedBy)

		var newDocs []string
		for _, docPath := range b.Documents {
			absPath := filepath.Join(projectRoot, docPath)
			if _, err := os.Stat(absPath); !os.IsNotExist(err) {
				newDocs = append(newDocs, docPath)
			}
		}
		docsRemoved := len(b.Documents) - len(newDocs)

		if !fixParent && !fixMilestone && blockedRemoved == 0 && docsRemoved == 0 {
			continue
		}

		clone := b.Clone()
		if fixParent {
			clone.Parent = ""
			fixed++
		}
		if fixMilestone {
			clone.Milestone = ""
			fixed++
		}
		if blockedRemoved > 0 {
			clone.BlockedBy = newBlockedBy
			fixed += blockedRemoved
		}
		if docsRemoved > 0 {
			clone.Documents = newDocs
			fixed += docsRemoved
		}

		dir, err := c.updateOnDiskDeferDirSync(clone)
		pending.Add(dir)
		if err != nil {
			return fixed, err
		}
		c.nibs[id] = clone
	}

	return fixed, nil
}
