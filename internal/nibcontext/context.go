package nibcontext

import (
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/estimate"
	"github.com/alphaleonis/nibs/internal/mdsection"
	"github.com/alphaleonis/nibs/internal/nib"
)

// NibRef is a lightweight reference to a nib for JSON output.
// It decouples the context JSON contract from the full nib.Nib shape.
// ID/Title/Status are always present; Type and Estimate may be absent
// for untyped/unestimated nibs (matching the nib data model).
type NibRef struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Type     string `json:"type,omitempty"`
	Estimate string `json:"estimate,omitempty"`
}

// ContainerSummary is a lightweight summary of a milestone or root container
// for the no-arg overview mode.
type ContainerSummary struct {
	NibRef
	ActivePhase *NibRef  `json:"active_phase,omitempty"`
	Progress    Progress `json:"progress"`
}

// Summary is the full context output for a nib or all active work.
type Summary struct {
	Root        *NibRef             `json:"root,omitempty"`
	ActivePhase *NibRef             `json:"active_phase,omitempty"`
	Progress    Progress            `json:"progress"`
	ActiveTasks []*NibRef           `json:"active_tasks"`
	NextTasks   []*NibRef           `json:"next_tasks"`
	Decisions   []string            `json:"decisions,omitempty"`
	Warnings    []string            `json:"warnings,omitempty"`
	Containers  []*ContainerSummary `json:"containers,omitempty"`
}

// Progress holds weighted progress metrics for a set of nibs.
type Progress struct {
	CompletedWeight int     `json:"completed_weight"`
	TotalWeight     int     `json:"total_weight"`
	Percentage      float64 `json:"percentage"`
}

// BuildSummary constructs a context summary for a specific nib and its descendants.
// If rootID is non-empty, scopes to that nib's descendants (works for any type).
// If empty, summarizes all active work with a warning.
//
// cfg supplies the closed-status definition (Config.IsClosedStatus) for the
// active-milestone filter below. The status literals here are not config-derived
// on purpose: the epic/task selectors match "in-progress"/"todo" — single
// statuses a group predicate cannot single out.
func BuildSummary(allNibs []*nib.Nib, rootID string, cfg *config.Config) Summary {
	byID := indexByID(allNibs)

	sum := Summary{
		ActiveTasks: []*NibRef{},
		NextTasks:   []*NibRef{},
	}

	if rootID == "" {
		// Overview mode: summarize active milestones and all leaf work.
		active := filterByStatusAndLeaf(allNibs, "in-progress")
		nib.SortByOrder(active)
		sum.ActiveTasks = toNibRefs(active)
		sum.Progress = CalcProgress(filterLeafWork(allNibs))

		// Build per-milestone container summaries for active milestones
		var milestones []*nib.Nib
		for _, n := range allNibs {
			// Classification check — exempt: empty type is never milestone/epic.
			if n.Type == "milestone" && !cfg.IsClosedStatus(n.Status) {
				milestones = append(milestones, n)
			}
		}
		nib.SortByOrder(milestones)

		for _, ms := range milestones {
			cs := &ContainerSummary{
				NibRef: *newNibRef(ms),
			}
			descendants := collectDescendants(allNibs, ms.ID, byID)
			cs.Progress = CalcProgress(descendants)

			// Active phase: first in-progress epic child
			var phaseCandidates []*nib.Nib
			for _, n := range descendants {
				// Classification check — exempt: empty type is never milestone/epic.
				if n.Type == "epic" && n.Status == "in-progress" && n.Parent == ms.ID {
					phaseCandidates = append(phaseCandidates, n)
				}
			}
			nib.SortByOrder(phaseCandidates)
			if len(phaseCandidates) > 0 {
				cs.ActivePhase = newNibRef(phaseCandidates[0])
			}

			sum.Containers = append(sum.Containers, cs)
		}

		return sum
	}

	root, ok := byID[rootID]
	if !ok {
		sum.Warnings = append(sum.Warnings, "nib not found: "+rootID)
		return sum
	}
	sum.Root = newNibRef(root)
	sum.Decisions = ExtractDecisions(root.Body)

	// Collect all descendants of the root nib
	descendants := collectDescendants(allNibs, rootID, byID)

	// Active phase: in-progress epic that is a direct child of the root.
	// Sort candidates by Order so the selection is deterministic.
	var phaseCandidates []*nib.Nib
	for _, n := range descendants {
		// Classification check — exempt: empty type is never milestone/epic.
		if n.Type == "epic" && n.Status == "in-progress" && n.Parent == rootID {
			phaseCandidates = append(phaseCandidates, n)
		}
	}
	nib.SortByOrder(phaseCandidates)
	if len(phaseCandidates) > 0 {
		sum.ActivePhase = newNibRef(phaseCandidates[0])
	}

	// Progress across all descendant leaf work
	sum.Progress = CalcProgress(descendants)

	// Active tasks: in-progress leaf work anywhere under root, sorted by Order
	activeTasks := filterByStatusAndLeaf(descendants, "in-progress")
	nib.SortByOrder(activeTasks)
	sum.ActiveTasks = toNibRefs(activeTasks)

	// Next tasks: todo leaf work under the active phase (if any), sorted by Order.
	// If there's no active phase, fall back to all todo leaf work under root.
	if len(phaseCandidates) > 0 {
		phaseDescendants := collectDescendants(allNibs, phaseCandidates[0].ID, byID)
		nextTasks := filterByStatusAndLeaf(phaseDescendants, "todo")
		nib.SortByOrder(nextTasks)
		sum.NextTasks = toNibRefs(nextTasks)
	} else {
		nextTasks := filterByStatusAndLeaf(descendants, "todo")
		nib.SortByOrder(nextTasks)
		sum.NextTasks = toNibRefs(nextTasks)
	}

	return sum
}

// CalcProgress computes weighted progress across a set of nibs.
// Only leaf work types (task, bug, feature) count — epics and milestones are excluded.
// It applies the same three-way rule as graph.ComputeProgress, weighted by
// estimate: "completed" is the numerator, "scrapped" work is no longer scope and
// leaves the denominator, and everything else counts toward the denominator
// without counting as done. Deferred nibs are in that last group — parked work
// is coming back, so it is outstanding scope. Draft nibs are there too: planned
// scope that hasn't been refined yet.
func CalcProgress(nibs []*nib.Nib) Progress {
	var completed, total int
	for _, n := range nibs {
		if !isLeafType(n.EffectiveType()) {
			continue
		}
		if n.Status == "scrapped" {
			continue
		}
		w := estimate.Weight(n.Estimate)
		total += w
		if n.Status == "completed" {
			completed += w
		}
	}
	var pct float64
	if total > 0 {
		pct = float64(completed) / float64(total) * 100
	}
	return Progress{
		CompletedWeight: completed,
		TotalWeight:     total,
		Percentage:      pct,
	}
}

// ExtractDecisions parses bullet points from a "Key Decisions" section in markdown.
func ExtractDecisions(body string) []string {
	content, found := mdsection.Find(body, "Key Decisions", mdsection.AnyLevel)
	if !found {
		return nil
	}

	var decisions []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			text := strings.TrimSpace(trimmed[2:])
			if text != "" {
				decisions = append(decisions, text)
			}
		}
	}
	return decisions
}

// newNibRef converts a full Nib to a lightweight NibRef.
func newNibRef(n *nib.Nib) *NibRef {
	return &NibRef{
		ID:       n.ID,
		Title:    n.Title,
		Status:   n.Status,
		Type:     n.EffectiveType(),
		Estimate: n.Estimate,
	}
}

// toNibRefs converts a slice of Nibs to NibRefs.
func toNibRefs(nibs []*nib.Nib) []*NibRef {
	refs := make([]*NibRef, len(nibs))
	for i, n := range nibs {
		refs[i] = newNibRef(n)
	}
	return refs
}

// indexByID builds a lookup map from nib ID to nib.
func indexByID(nibs []*nib.Nib) map[string]*nib.Nib {
	m := make(map[string]*nib.Nib, len(nibs))
	for _, n := range nibs {
		m[n.ID] = n
	}
	return m
}

// collectDescendants returns all nibs that are descendants of rootID
// (children, grandchildren, etc.) — not including rootID itself.
func collectDescendants(allNibs []*nib.Nib, rootID string, byID map[string]*nib.Nib) []*nib.Nib {
	// Build children map
	children := make(map[string][]string)
	for _, n := range allNibs {
		if n.Parent != "" {
			children[n.Parent] = append(children[n.Parent], n.ID)
		}
	}

	// BFS from rootID
	var result []*nib.Nib
	queue := children[rootID]
	visited := make(map[string]bool)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		visited[id] = true
		if n, ok := byID[id]; ok {
			result = append(result, n)
			queue = append(queue, children[id]...)
		}
	}
	return result
}

// isLeafType returns true for work types that count toward progress.
// Mirrors the leaf-type set in internal/nibtypes/hierarchy.go — keep in sync
// when new leaf types are added to DefaultTypes.
func isLeafType(typ string) bool {
	return typ == "task" || typ == "bug" || typ == "feature" || typ == "research"
}

// filterByStatusAndLeaf returns leaf-type nibs matching the given status.
func filterByStatusAndLeaf(nibs []*nib.Nib, status string) []*nib.Nib {
	result := []*nib.Nib{}
	for _, n := range nibs {
		if isLeafType(n.EffectiveType()) && n.Status == status {
			result = append(result, n)
		}
	}
	return result
}

// filterLeafWork returns only leaf-type nibs from the input.
func filterLeafWork(nibs []*nib.Nib) []*nib.Nib {
	var result []*nib.Nib
	for _, n := range nibs {
		if isLeafType(n.EffectiveType()) {
			result = append(result, n)
		}
	}
	return result
}
