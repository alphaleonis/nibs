package nibcore

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/alphaleonis/nibs/internal/fsutil"
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
// loads as written (see loadFromDisk's diagnostic warning) — but then every
// update of the nib through nibs is refused, and no CLI flag or mutation input
// exposes the axis fields for repair. This finding is what names the file the
// hand edit has to fix.
type InvalidAxis struct {
	NibID string `json:"nib_id"`
	// Path is relative to the nibs root with forward slashes, like nib.Path.
	Path string `json:"path"`
	// Reason is ValidateAxes' message, naming the axis the type refuses.
	Reason string `json:"reason"`
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

	// Key integrity. Derivable from the nibs alone (the modeled key set is a
	// compile-time fact of the nib package), so the pure map function carries it.
	NearMissKeys []NearMissKey `json:"near_miss_keys"`
}

// HasIssues returns true if any issues were found.
func (r *LinkCheckResult) HasIssues() bool {
	return r.TotalIssues() > 0
}

// TotalIssues returns the total count of all issues.
func (r *LinkCheckResult) TotalIssues() int {
	return len(r.BrokenLinks) + len(r.SelfLinks) + len(r.Cycles) + len(r.BrokenDocuments) + r.LoadIssues() + r.EnumIssues() + r.AxisIssues() + r.NearMissIssues()
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

// NearMissIssues returns the count of near-miss key findings alone, for the
// same render-them-apart reason as LoadIssues.
func (r *LinkCheckResult) NearMissIssues() int {
	return len(r.NearMissKeys)
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
		BrokenLinks:      []BrokenLink{},
		SelfLinks:        []SelfLink{},
		Cycles:           []Cycle{},
		BrokenDocuments:  []BrokenDocument{},
		UnparseableFiles: []UnparseableFile{},
		DuplicateIDs:     []DuplicateID{},
		InvalidEnums:     []InvalidEnum{},
		InvalidAxes:      []InvalidAxis{},
		NearMissKeys:     []NearMissKey{},
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

		// Check milestone link, resolved under the same rule as parent.
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
		// through nibs is refused. This finding is what names the file to fix.
		if err := nibtypes.ValidateAxes(b.EffectiveType(), b.Milestone, b.Area); err != nil {
			result.InvalidAxes = append(result.InvalidAxes, InvalidAxis{NibID: b.ID, Path: b.Path, Reason: err.Error()})
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
