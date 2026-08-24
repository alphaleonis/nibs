package nibcore

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

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
// absent from every query, and no query result hints at why. Reporting it is
// the only way a user learns that `nibs list` is under-reporting.
type UnparseableFile struct {
	// NibID is derived from the FILENAME (which parses whatever the contents
	// are), so it names the nib that went missing. Empty when the filename
	// yields no id.
	NibID string `json:"nib_id"`
	// Path is relative to the nibs root with forward slashes, the same shape as
	// nib.Path, so a diagnostic names a file the way every other nibs surface
	// does.
	Path string `json:"path"`
	// Reason is the underlying parse/read error, verbatim — the user has to
	// repair the file by hand, so the report has to say what is wrong with it.
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
// exactly as written (see loadFromDisk's diagnostic warning) — this finding is
// what makes it visible: filters, ranking and the web UI all assume enum
// validity, and nothing else authoritatively re-checks it after load.
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
// refused. Dropping the keys is reachable: `--clear milestone` and
// `--clear area` (updateNib's milestone and area inputs on every other client)
// apply to the subject ABOVE the guards, so they clear the axis instead of
// being refused by it. This finding names the nib whose ordinary edits are
// dead-ended until that clear runs — see ClearAxesCommand for why it is ONE
// command and not a choice between two.
//
// Retyping is the other way to reconcile the type with the axis, and the plain
// diagnostics deliberately do not prescribe it, because whether it works
// depends on state they do not report: it is refused while nibs are still
// assigned to the milestone (a milestone's ordinary state), and refused again
// when the `area:` it would keep is one the vocabulary no longer declares. On
// an empty queue with a declared area it succeeds, and it is then the only
// escape that keeps the assignment — which is exactly why `--fix` names it, as
// the CHOICE it will not make for the author rather than as a command to run.
// The clear is what holds in every state, so it is what carries a command.
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

// ClearAxesCommand renders the one command that drops every axis key a nib's
// type refuses.
//
// It is one command rather than one per key because clearing a single key
// leaves the write refused by the next: on a nib carrying both, `--clear
// milestone` alone is refused for the area and `--clear area` alone is refused
// for the milestone, so offering them as alternatives names two commands of
// which neither works. nibID is expected already rendered for the surface it
// is printed on — this only assembles the grammar, so that every surface that
// prescribes the escape prescribes the same one.
// AxisKeysNoun names the offending keys in prose, matching what
// ClearAxesCommand clears, so a message never says "key" beside a command that
// drops two.
func AxisKeysNoun(axes []string) string {
	if len(axes) > 1 {
		return "both axis keys"
	}
	return "the axis key"
}

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
// parent or change a type, so an offender reaches the store only through a
// hand edit or as the leftover of an earlier rule set — the v2 migration
// deliberately leaves illegal nests untouched — and it then loads, lists and
// renders like any other nib. This finding is the one surface that names the
// file and the rule it breaks.
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
// EXISTS but is not milestone-typed (nibtypes' rule, the same one
// membership.ResolvedMilestoneID applies). The rule is strict on the write
// paths that assign — `nibs set <id> --milestone <feature-id>` is refused — so
// an offender reaches the store only through a hand edit or as data predating
// the rule, and it then loads and lists like any other nib while the
// assignment resolves to nothing: the nib confers no membership, sits in the
// backlog, and appears in no milestone queue, yet its `milestone:` field reads
// back the bad target. This finding is the one surface that names it.
//
// A MILESTONE-typed nib carrying `milestone:` is deliberately not reported
// here — see the exclusion where the finding is raised.
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
// nib and one of its ancestors are never both assigned; files are the whole
// planning truth and membership is derived, never inherited). The rule is
// strict on the write paths that assign or reparent, so an offender reaches
// the store only through a hand edit or data that predates the rule, and it
// then loads, lists and schedules like any other nib — counted in BOTH queues.
// This finding is the one surface that names the pair. One finding per nib,
// naming its NEAREST assigned ancestor; a deeper conflict shows up as that
// ancestor's own finding.
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
// refusal, standing in the store as a fact rather than as a rejected write.
//
// The CLOSING transition is refused on every client — `nibs close`'s gate
// (cmd/close_queue.go) and updateNib's backstop (graph.MilestoneQueueOpenError),
// covering the CLI, the web, the TUI and `nibs graphql`. The state is NOT
// unreachable, though, and this finding is not here only for hand edits and
// pre-rule data: the ASSIGNMENT door is still open, since assigning work to an
// already-closed milestone checks the target's type and never its status
// (nibs-l5df). So ordinary use still lands it, from the other side.
//
// It then loads, lists and schedules like any other nib, and the queue keeps
// carrying work planned for a wave that has finished. This finding is the one
// surface that names it — and, while a door stays open, the only one that can:
// a refusal only ever sees the write it refuses, never the state that write
// leaves behind by another route.
//
// Deferred is not an offense on EITHER side, and for two different reasons: a
// deferred MILESTONE holds its queue on purpose (a parked wave is coming back),
// and a deferred MEMBER is closed and so does not hold the milestone open.
type ClosedMilestoneQueue struct {
	NibID string `json:"nib_id"`
	// Path is relative to the nibs root with forward slashes, like nib.Path.
	Path string `json:"path"`
	// Status is the releasing status the milestone carries.
	Status string `json:"status"`
	// Open is the open queue entries' full ids, in queue order — the same set
	// and the same order graph.OpenQueueEntries yields, so the report and the
	// refusal name one set.
	Open []string `json:"open"`
}

// NearMissKey is a loaded nib carrying an unknown front-matter key whose
// spelling is a near miss of a modeled key (a dash for the underscore, a case
// variant, stray underscores — the rule is nib.ModeledKeyResembling's). The
// key parses losslessly into Extra and renders back — read tolerance is
// unchanged — but no filter or query consults Extra, so a mistyped
// `milestone-order:` silently drops the nib out of every milestone view. This
// finding is the one surface that names it.
type NearMissKey struct {
	NibID string `json:"nib_id"`
	// Path is relative to the nibs root with forward slashes, like nib.Path.
	Path string `json:"path"`
	// Key is the unknown key exactly as the file spells it.
	Key string `json:"key"`
	// Modeled is the modeled front-matter key the spelling resembles.
	Modeled string `json:"modeled"`
}

// LinkCheckResult contains all nib integrity issues found: the link issues
// derivable from the loaded nibs, plus the load-time integrity issues that are
// derivable only from what did NOT make it into the store.
type LinkCheckResult struct {
	BrokenLinks     []BrokenLink     `json:"broken_links"`
	SelfLinks       []SelfLink       `json:"self_links"`
	Cycles          []Cycle          `json:"cycles"`
	BrokenDocuments []BrokenDocument `json:"broken_documents"`

	// Load-time integrity. Populated only by Core.CheckAllLinks, which can read
	// what the last load retained; CheckAllLinksInMap sees a map of nibs that
	// loaded successfully and so has no evidence of either condition.
	UnparseableFiles []UnparseableFile `json:"unparseable_files"`
	DuplicateIDs     []DuplicateID     `json:"duplicate_ids"`

	// Field integrity. Populated only by Core.CheckAllLinks — enum validity
	// needs the config's enum tables, which the pure map function does not
	// carry.
	InvalidEnums []InvalidEnum `json:"invalid_enums"`

	// Axis integrity. Derivable from the nibs alone (the axis rule is
	// nibtypes.ValidateAxes, config-free), so the pure map function carries it.
	InvalidAxes []InvalidAxis `json:"invalid_axes"`

	// Hierarchy integrity. Derivable from the nibs alone (the parent-type rule
	// is nibtypes.ValidateParentType, config-free; the prefix a parent id may
	// need is already threaded in for the link checks), so the pure map
	// function carries it.
	InvalidHierarchies []InvalidHierarchy `json:"invalid_hierarchies"`

	// Milestone-target integrity. Derivable from the nibs alone (a type
	// comparison over a target the link checks already resolve), so the pure
	// map function carries it.
	InvalidMilestoneTargets []InvalidMilestoneTarget `json:"invalid_milestone_targets"`

	// Assignment integrity. Derivable from the nibs alone (exclusivity along
	// the parent chain needs only the links and the prefix already threaded
	// in), so the pure map function carries it.
	AssignmentConflicts []AssignmentConflict `json:"assignment_conflicts"`

	// Key integrity. Derivable from the nibs alone (the modeled key set is a
	// compile-time fact of the nib package), so the pure map function carries it.
	NearMissKeys []NearMissKey `json:"near_miss_keys"`

	// Queue integrity. Populated only by Core.CheckAllLinks — which statuses
	// close and which release their dependents is the config's answer
	// (Config.IsClosedStatus / Config.StatusReleasesDependents), the same
	// reason InvalidEnums is filled there. The derivation itself is pure and
	// lives in closedMilestoneQueuesInMap; only the two role predicates are
	// threaded in, exactly as the blocking queries thread
	// releasesDependentsPredicate, so nibcore keeps no status list of its own.
	ClosedMilestoneQueues []ClosedMilestoneQueue `json:"closed_milestone_queues"`
}

// HasIssues returns true if any issues were found.
func (r *LinkCheckResult) HasIssues() bool {
	return r.TotalIssues() > 0
}

// TotalIssues returns the total count of all issues.
func (r *LinkCheckResult) TotalIssues() int {
	return len(r.BrokenLinks) + len(r.SelfLinks) + len(r.Cycles) + len(r.BrokenDocuments) + r.LoadIssues() + r.EnumIssues() + r.AxisIssues() + r.HierarchyIssues() + r.AssignmentIssues() + r.MilestoneTargetIssues() + r.NearMissIssues() + r.QueueIssues()
}

// LoadIssues returns the count of load-time integrity issues alone. Callers
// that report the two kinds separately — `nibs check` renders them under their
// own heading, and neither is auto-fixable — need to count them apart from the
// link categories.
func (r *LinkCheckResult) LoadIssues() int {
	return len(r.UnparseableFiles) + len(r.DuplicateIDs)
}

// EnumIssues returns the count of out-of-enum field findings alone, for the
// same render-them-apart reason as LoadIssues.
func (r *LinkCheckResult) EnumIssues() int {
	return len(r.InvalidEnums)
}

// AxisIssues returns the count of axis-rule findings alone, for the same
// render-them-apart reason as LoadIssues.
func (r *LinkCheckResult) AxisIssues() int {
	return len(r.InvalidAxes)
}

// HierarchyIssues returns the count of hierarchy-rule findings alone, for the
// same render-them-apart reason as LoadIssues.
func (r *LinkCheckResult) HierarchyIssues() int {
	return len(r.InvalidHierarchies)
}

// AssignmentIssues returns the count of assignment-exclusivity findings alone,
// for the same render-them-apart reason as LoadIssues.
func (r *LinkCheckResult) AssignmentIssues() int {
	return len(r.AssignmentConflicts)
}

// MilestoneTargetIssues returns the count of invalid-milestone-target findings
// alone, for the same render-them-apart reason as LoadIssues.
func (r *LinkCheckResult) MilestoneTargetIssues() int {
	return len(r.InvalidMilestoneTargets)
}

// NearMissIssues returns the count of near-miss key findings alone, for the
// same render-them-apart reason as LoadIssues.
func (r *LinkCheckResult) NearMissIssues() int {
	return len(r.NearMissKeys)
}

// QueueIssues returns the count of closed-milestone-queue findings alone, for
// the same render-them-apart reason as LoadIssues.
func (r *LinkCheckResult) QueueIssues() int {
	return len(r.ClosedMilestoneQueues)
}

// CheckAllLinksInMap validates all links across all nibs.
// When projectRoot is empty, document filesystem checks are skipped.
// This is a pure function that operates on a map of nibs without locking.
//
// A parent, milestone or blockedBy target is resolved through normalizeIDInMap —
// the exact id, then the configured prefix prepended — so it is broken only when
// no nib answers to it under either spelling. That is the same rule Core.Get
// and findActiveBlockersInMap apply, which matters because Core.FixBrokenLinks
// repeats these checks and writes: a bare map lookup here called a resolvable
// short-form target broken, and `nibs check --fix` then deleted it from the
// file. configPrefix is threaded in because a pure map function cannot reach
// the project config itself.
//
// Resolution also decides self versus broken: a target that resolves back to
// the nib holding it is a self link however it was spelled.
//
// The cycle pass below, the reverse traversals (findIncomingLinksInMap,
// isBlockingInMap) and the setParent cycle guard all walk exact map keys, and
// are correct because every id in the store is already full: the loader
// resolves short-form link ids once, at the disk-read boundary (see
// canonicalize.go). Resolving again here is what keeps this check honest for
// the ids canonicalization deliberately leaves verbatim — an id naming no nib
// is broken however it is spelled, and the report names the spelling the file
// holds, which is what `--fix` would drop.
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
		InvalidHierarchies:      []InvalidHierarchy{},
		InvalidMilestoneTargets: []InvalidMilestoneTarget{},
		AssignmentConflicts:     []AssignmentConflict{},
		NearMissKeys:            []NearMissKey{},
		ClosedMilestoneQueues:   []ClosedMilestoneQueue{},
	}

	// Check for broken links and self-references
	for _, b := range nibs {
		// Check parent link. Target reports the spelling as stored, which is
		// what `--fix` would drop.
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

		// Check milestone link, resolved under the same rule as parent. A
		// target that resolves is judged once more, on its TYPE: only a
		// milestone-typed one confers membership (membership.ResolvedMilestoneID's
		// rule, which is also what the assigning write path refuses), so a
		// resolvable non-milestone target leaves the nib in no queue while its
		// `milestone:` field still reads back. The same resolution answers all
		// three cases — resolving a second time by another rule would part this
		// report from the refusal it mirrors.
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
			// A MILESTONE-typed subject is excluded: its type takes no
			// assignment axis at all, so InvalidAxes already names it and the
			// whole key has to go. Naming the target's type here too would send
			// the reader to repoint a key they are about to delete.
			case nibs[fullID].EffectiveType() != "milestone" && b.EffectiveType() != "milestone":
				result.InvalidMilestoneTargets = append(result.InvalidMilestoneTargets, InvalidMilestoneTarget{
					NibID:      b.ID,
					Path:       b.Path,
					Target:     fullID,
					TargetType: nibs[fullID].EffectiveType(),
				})
			}
		}

		// Check document paths exist on disk (skip when projectRoot is empty)
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

		// Check blocked_by links (single-side: blocking not persisted)
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

	// The loop above walks the map, so its findings arrive in whatever order
	// Go hands out the keys. Sorting is what makes the report and the --json
	// envelope stable run to run, the same reason the per-nib pass below walks
	// sorted ids.
	sort.Slice(result.InvalidMilestoneTargets, func(i, j int) bool {
		return result.InvalidMilestoneTargets[i].NibID < result.InvalidMilestoneTargets[j].NibID
	})

	// Check for cycles in blocked_by and parent links
	// (blocking is derived from blocked_by, so only these two need cycle
	// checks; milestone is a flat assignment nothing traverses transitively,
	// so a milestone loop cannot hang any walk)
	for _, linkType := range []string{"blocked_by", "parent"} {
		cycles := FindCyclesInMap(nibs, linkType)
		result.Cycles = append(result.Cycles, cycles...)
	}

	// Per-nib field findings, sorted by id (and near-miss keys by key) — both
	// maps would otherwise shuffle the report run to run.
	ids := make([]string, 0, len(nibs))
	for id := range nibs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		b := nibs[id]
		// Axis integrity: the axis rule is strict on the write paths only, so a
		// hand-edited offender loads as written — and then every update of it
		// that keeps both the type and the offending keys is refused. This
		// finding is what names the file to fix, and the keys the fix drops.
		if err := nibtypes.ValidateAxes(b.EffectiveType(), b.Milestone, b.Area); err != nil {
			result.InvalidAxes = append(result.InvalidAxes, InvalidAxis{
				NibID:  b.ID,
				Path:   b.Path,
				Reason: err.Error(),
				Axes:   nibtypes.RefusedAxes(b.EffectiveType(), b.Milestone, b.Area),
			})
		}
		// Hierarchy integrity: the parent-type rule is strict only on the write
		// paths that set a parent or change a type, and the v2 migration
		// deliberately leaves an illegal nest untouched — so a file-level
		// offender loads, lists and renders normally and no other surface
		// reports it. A parent that does not resolve is already a broken link
		// (its type is unknowable), and one resolving to the nib itself is
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
		// Assignment integrity: exclusivity along the parent chain is strict
		// only on the write paths that assign or reparent, so an assigned nib
		// under an assigned ancestor reaches the store by hand edit or from
		// data predating the rule, and is then scheduled in both queues. The
		// walk reads RESOLVED assignments — the target must exist and be
		// milestone-typed, membership.ResolvedMilestoneID's rule, which is
		// also what the write path judges — so a dangling or non-milestone
		// assignment conflicts with nothing; each is its own finding above (a
		// BrokenLink, an InvalidMilestoneTarget), not a conflict here, since
		// neither confers the membership exclusivity is about. Parents resolve
		// the way every link check resolves them, and a visited set bounds the
		// walk on a hand-edited cycle.
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
		// Key integrity: an Extra key spelled a near miss from a modeled key
		// (nib.ModeledKeyResembling's rule) loads losslessly but is invisible to
		// every filter, so it is reported here.
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

// resolvedMilestoneInMap is membership.ResolvedMilestoneID's rule over the
// map: b's `milestone:` target when it resolves (exact id, then the prefix
// prepended) to a milestone-typed nib, "" otherwise.
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
// map: each milestone whose status releases its dependents, paired with the
// open work still assigned to it.
//
// It is graph.OpenQueueEntries' rule read over a different substrate — DIRECT
// assignees only (work belonging to the milestone through an assigned ancestor
// carries no assignment of its own), milestone-typed members skipped, queue
// order from nib.SortByMilestoneOrder. A report naming a wider or narrower set
// than the refusal would send a reader to repair something no write surface
// objects to.
//
// Which is why the assignment is resolved by calling membership.ResolvedMilestoneID
// itself rather than by restating its clauses: that is the function both refusals
// reach through OpenQueueEntries -> View.DirectMembers, so the three answers agree
// by construction rather than by two prose descriptions staying in step. Hence no
// configPrefix parameter — expanding a shorthand id here would part this function
// from the refusals it exists to mirror.
//
// The agreement is what is guaranteed; it is NOT a claim that a shorthand id is
// inert system-wide. ResolvedMilestoneID has no rule of its own — it answers
// through the Lookup its caller supplies, and Compute's is an exact byID map
// while the ordering engine, the milestone filter and cmd/close.go pass a
// Reader.Get-backed closure, which DOES prefix-expand (nibcore.Core.Get).
//
// Nor is this guarding an observed defect: Core.Load canonicalizes link ids in
// memory before any of this runs, so CheckAllLinks is handed assignments already
// in full form and a store cannot in practice present the divergent case. This
// couples a pure function to the rule it mirrors so the two cannot drift apart
// later; it changes no reported finding today.
// Pinned by TestClosedMilestoneQueueAgreesWithMembership.
//
// isClosed and releasesDependents are supplied by the caller because this is a
// pure function over a map and cannot reach the project config itself, the same
// arrangement isBlockedInMap has. The two are NOT interchangeable: a deferred
// member is closed and does not hold its milestone open, while a deferred
// milestone releases nothing and is no offense at all.
//
// Findings come back sorted by milestone id, since map order would shuffle the
// report run to run.
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
// files that did NOT make it in — so they are read from the Core rather than
// recomputed by CheckAllLinksInMap. They describe the last load: a CLI command
// loads once and then checks, so what it reports is the state it is querying.
// A long-lived process whose watcher has since reconciled an individual file
// keeps reporting what its Load saw, until the next Load.
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

	// Field integrity: re-validate enums against the config here (not in the
	// pure map function, which carries no config). Values load as written —
	// see loadFromDisk — so this is the report that makes an out-of-enum value
	// actionable. Sorted by id: map order would shuffle the report run to run.
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

	// Queue integrity: same reason it is here and not in the pure map function
	// — the status ROLES are the config's answer. The derivation stays pure;
	// only the two predicates cross.
	result.ClosedMilestoneQueues = append(result.ClosedMilestoneQueues,
		closedMilestoneQueuesInMap(c.nibs, c.closedStatusPredicate(), c.releasesDependentsPredicate())...)
	return result
}

// LoadDiagnostics returns the load-time integrity problems retained from the
// last Load: files on disk that are NOT answerable through the store (skipped
// unparseable files, and losers of id collisions). It exists for callers that
// must know whether the loaded store is a faithful picture of the whole
// directory before acting on it — the migrate command refuses to rewrite a
// store that did not load cleanly, since migrating around a skipped file can
// silently drop edges to it. Slices are copied out, matching CheckAllLinks.
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
	seenCycles := make(map[string]bool) // To avoid duplicate cycle reports

	var dfs func(id string, path []string)
	dfs = func(id string, path []string) {
		if inStack[id] {
			// Found a cycle - find where the cycle starts
			cycleStart := -1
			for i, p := range path {
				if p == id {
					cycleStart = i
					break
				}
			}
			if cycleStart >= 0 {
				cyclePath := append(path[cycleStart:], id)
				// Create a canonical key to avoid duplicate cycles
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
			// Get targets based on link type
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

	for id := range nibs {
		if !visited[id] {
			dfs(id, nil)
		}
	}

	return cycles
}

// canonicalCycleKey creates a unique key for a cycle to detect duplicates.
// It normalizes the cycle by starting from the smallest ID.
func canonicalCycleKey(path []string) string {
	if len(path) <= 1 {
		return ""
	}

	// Remove the duplicate end element (cycle closes back)
	cycle := path[:len(path)-1]

	// Find the minimum element to use as start
	minIdx := 0
	for i, id := range cycle {
		if id < cycle[minIdx] {
			minIdx = i
		}
	}

	// Rotate to start from minimum
	key := ""
	for i := 0; i < len(cycle); i++ {
		idx := (minIdx + i) % len(cycle)
		if i > 0 {
			key += "->"
		}
		key += cycle[idx]
	}

	return key
}

// RemoveLinksTo removes every parent, milestone and blockedBy link that
// RESOLVES to the given target from all nibs. Returns the number of links
// removed.
//
// Both ends of the comparison are spelling-independent, because neither end is
// under a caller's control:
//
//   - The TARGET is resolved through the same exact-id-then-prefix-prepended
//     rule as Core.Get and Core.Delete (normalizeIDInMap), so a short id names
//     the nib it names everywhere else. Requiring callers to pass a resolved id
//     would make this the only id-taking Core mutator to do so, and its failure
//     mode is silent — (0, nil), no error, links left dangling behind a delete.
//   - A STORED link matches when IT resolves to that same nib, by the same rule.
//     Canonicalization resolves stored link ids to their full form at the
//     disk-read boundary, but Core.Create stores a nib's links exactly as given
//     and runs no such pass (see canonicalize.go), so a short-form link can sit
//     in the store while its prefixed target is the only nib answering to it.
//
// A literal equality against the id AS GIVEN matches as well, which is what
// strips links to a target that resolves to nothing — an unresolvable id is
// carried verbatim by design, so verbatim is the only way to name it. That leg
// serves a direct Core caller repairing links left behind by a nib that is
// already gone; no production caller reaches it, since the GraphQL DeleteNib
// resolver resolves its target before calling and so always passes one that
// resolves.
//
// A store holding BOTH a bare token `tgt` and its prefixed twin `nibs-tgt` keeps
// them as separate edges throughout: `tgt` resolves to itself by exact match, so
// unlinking either twin leaves the other's incoming links alone.
//
// The legacy v0 Blocking field is deliberately untouched, matching the rest of
// the link-health family (CheckAllLinksInMap, FixBrokenLinks): in v1+ blocking
// is derived from other nibs' BlockedBy, and Blocking is not a link source. In a
// loaded store it survives only where the v0→v1 migration was deferred, and
// there it is a faithful record of the file's own bytes — Render re-emits it so
// the canonical etag matches (see nib.Nib.Blocking). Clearing it belongs to that
// migration, not here.
//
// An empty target names no nib and is refused up front. The early return is
// load-bearing, not a shortcut for the O(N) walk: `""` DOES resolve in a store
// holding a nib whose id is exactly the configured prefix — a hand-written
// `nibs-.md`, which Get("") answers with — so without the refusal an empty
// target would strip that nib's incoming links. Separately, pointsAtTarget
// rejects an empty LINK id, so a nib whose Parent is unset is never an incoming
// link to anything.
//
// Copy-on-write: for every nib that actually changes we clone it, mutate the
// clone, persist the clone, and reinstall it under c.nibs[id] — the stored
// pointer is never edited in place. This upholds the canonical live-pointer /
// copy-on-write invariant (see NibReader.GetSnapshot in
// internal/graph/interfaces.go): the changed fields here (Parent, a torn string;
// BlockedBy, a memory-unsafe torn slice header) are non-Path, so they must land
// on a fresh pointer, leaving any off-lock reader still holding the old one a
// stable, unmutated value. Ranging over c.nibs while reassigning an existing
// key's value is safe in Go.
func (c *Core) RemoveLinksTo(targetID string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if targetID == "" {
		return 0, nil
	}

	configPrefix := c.configPrefix()
	fullID, resolved := c.normalizeIDForLookupLocked(targetID)

	// Resolving the link costs a map lookup, so it runs only for a link the
	// literal compare already rejected, and only when the target resolves at all.
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
			// Every occurrence of every matching spelling goes; count the drops
			// via the length delta (matching FixBrokenLinks).
			before := len(clone.BlockedBy)
			clone.BlockedBy = slices.DeleteFunc(clone.BlockedBy, pointsAtTarget)
			removed += before - len(clone.BlockedBy)
		}

		dir, err := c.saveToDiskDeferDirSync(clone)
		pending.Add(dir)
		if err != nil {
			return removed, err
		}
		c.nibs[id] = clone
	}

	return removed, nil
}

// FixBrokenLinks removes all broken links (links to non-existent nibs) and self-references.
// Returns the number of issues fixed.
//
// It restates the parent, milestone, blockedBy and document checks
// CheckAllLinksInMap makes, resolving each link target through
// normalizeIDInMap the same way, so
// `nibs check --fix` removes exactly the broken links, self links and broken
// documents `nibs check` reported. The other reported categories are left
// untouched here and the command prints them as not auto-fixable instead:
// cycles, and the two load-time conditions (an unparseable file, whose repair
// means editing YAML the user wrote, and a duplicate id, whose resolution means
// choosing which file to lose).
//
// A link that resolves is left exactly as stored: nothing here rewrites a
// short id into its full form.
//
// Copy-on-write for the same reason as RemoveLinksTo: mutate a clone and
// reinstall it rather than editing the stored pointer in place, so no off-lock
// reader ever sees a stored pointer's non-Path fields torn mid-write. See the
// canonical live-pointer / copy-on-write invariant at NibReader.GetSnapshot
// (internal/graph/interfaces.go). Documents is made copy-on-write here too, for
// the same discipline, so any future off-lock reader of it is safe as well.
// SkippedIDSet builds the set of ids whose file is present on disk but was
// not loaded (unparseable/unreadable — see UnparseableFile), so a link naming
// one of them is unresolvable-for-now rather than broken. Each skipped id is
// entered under BOTH spellings a link may hold — as the filename derives it,
// and with the configured prefix trimmed — the same two spellings
// normalizeIDInMap resolves, so consumers test a link target with one plain
// map probe of the spelling the file holds.
//
// It is the ONE builder for this rule: Core.FixBrokenLinks' keep-don't-erase
// gate and cmd/check's report partition both build from it, so what --fix
// preserves and what the report claims can never disagree by construction.
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
//
// Their nibs are absent from c.nibs, so every link naming one resolves to
// nothing and CheckAllLinks reports it broken. That report is correct — the
// link genuinely cannot be followed right now — but "fixing" it by deletion is
// not: the target is sitting on disk needing a YAML repair, and repairing it
// does NOT bring back an edge already erased. The migrate command takes the
// same posture for the same reason, refusing to run while a file is
// unparseable rather than destroying edges around it.
//
// This became urgent when `nibs check` started REPORTING unparseable files
// (nibs-968i): the user is now told a file is broken, and the obvious next step
// is --fix. Must be called with c.mu held.
func (c *Core) skippedIDsLocked() map[string]bool {
	return SkippedIDSet(c.unparseableFiles, c.configPrefix())
}

func (c *Core) FixBrokenLinks() (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

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

		// Parent is dropped when it resolves back to this nib (self) or
		// resolves to nothing (broken) — but NOT when it names a nib whose file
		// is on disk and merely failed to load (see skippedIDsLocked).
		fixParent := false
		if b.Parent != "" {
			fullID, ok := normalizeIDInMap(c.nibs, b.Parent, configPrefix)
			fixParent = (!ok && !skipped[b.Parent]) || fullID == b.ID
		}

		// Milestone follows the same rule as parent, skipped-target gate
		// included.
		fixMilestone := false
		if b.Milestone != "" {
			fullID, ok := normalizeIDInMap(c.nibs, b.Milestone, configPrefix)
			fixMilestone = (!ok && !skipped[b.Milestone]) || fullID == b.ID
		}

		// Surviving blocked_by set (drop self-refs and links to missing nibs).
		// Survivors keep the spelling they were stored with.
		var newBlockedBy []string
		for _, blocker := range b.BlockedBy {
			fullID, ok := normalizeIDInMap(c.nibs, blocker, configPrefix)
			if (!ok && !skipped[blocker]) || fullID == b.ID {
				continue
			}
			newBlockedBy = append(newBlockedBy, blocker)
		}
		blockedRemoved := len(b.BlockedBy) - len(newBlockedBy)

		// Surviving document set (drop paths that no longer exist on disk).
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

		dir, err := c.saveToDiskDeferDirSync(clone)
		pending.Add(dir)
		if err != nil {
			return fixed, err
		}
		c.nibs[id] = clone
	}

	return fixed, nil
}
