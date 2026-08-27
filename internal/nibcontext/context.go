package nibcontext

import (
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/mdsection"
	"github.com/alphaleonis/nibs/internal/membership"
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
	ActivePhase *NibRef `json:"active_phase,omitempty"`
}

// Summary is the full context output for a nib or all active work.
type Summary struct {
	Root        *NibRef             `json:"root,omitempty"`
	ActivePhase *NibRef             `json:"active_phase,omitempty"`
	ActiveTasks []*NibRef           `json:"active_tasks"`
	NextTasks   []*NibRef           `json:"next_tasks"`
	Decisions   []string            `json:"decisions,omitempty"`
	Warnings    []string            `json:"warnings,omitempty"`
	Containers  []*ContainerSummary `json:"containers,omitempty"`
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
	return BuildSummaryWithView(allNibs, membership.Compute(allNibs), rootID, cfg)
}

// BuildSummaryWithView is BuildSummary for a caller that already computed the
// membership view over the same slice, so the summary and the rollups the
// caller derives from that same view share one Compute rather than taking one
// per layer. The view MUST be built over allNibs; two different slices here
// would let the summary and its rollups disagree about the store.
func BuildSummaryWithView(allNibs []*nib.Nib, view *membership.View, rootID string, cfg *config.Config) Summary {
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

		// Build per-milestone container summaries for active milestones
		var milestones []*nib.Nib
		for _, n := range view.Milestones() {
			if !cfg.IsClosedStatus(n.Status) {
				milestones = append(milestones, n)
			}
		}
		nib.SortByOrder(milestones)

		for _, ms := range milestones {
			cs := &ContainerSummary{
				NibRef: *newNibRef(ms),
			}

			// Active phase: first in-progress epic member, in queue order
			var phaseCandidates []*nib.Nib
			for _, n := range view.DirectMembers(ms.ID) {
				// Classification check — exempt: empty type is never milestone/epic.
				if n.Type == "epic" && n.Status == "in-progress" {
					phaseCandidates = append(phaseCandidates, n)
				}
			}
			nib.SortByMilestoneOrder(phaseCandidates)
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
	descendants := view.Members(rootID)

	// Active phase: in-progress epic that is a direct member of the root.
	// Sort candidates by the root's member-order key so the selection is
	// deterministic (see sortDirectMembers).
	var phaseCandidates []*nib.Nib
	for _, n := range view.DirectMembers(rootID) {
		// Classification check — exempt: empty type is never milestone/epic.
		if n.Type == "epic" && n.Status == "in-progress" {
			phaseCandidates = append(phaseCandidates, n)
		}
	}
	sortDirectMembers(root, phaseCandidates)
	if len(phaseCandidates) > 0 {
		sum.ActivePhase = newNibRef(phaseCandidates[0])
	}

	// Active tasks: in-progress leaf work anywhere under root, sorted by Order
	activeTasks := filterByStatusAndLeaf(descendants, "in-progress")
	nib.SortByOrder(activeTasks)
	sum.ActiveTasks = toNibRefs(activeTasks)

	// Next tasks: todo leaf work under the active phase (if any), sorted by Order.
	// If there's no active phase, fall back to all todo leaf work under root.
	if len(phaseCandidates) > 0 {
		phaseDescendants := view.Members(phaseCandidates[0].ID)
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

// sortDirectMembers orders a container's direct members by the key that
// positions them in THAT container: a milestone's members sit in its queue
// (milestone_order), every other container's in its decomposition (order).
// Members lacking the key fall to the shared title tiebreak.
func sortDirectMembers(container *nib.Nib, members []*nib.Nib) {
	if container.EffectiveType() == "milestone" {
		nib.SortByMilestoneOrder(members)
		return
	}
	nib.SortByOrder(members)
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

// isLeafType returns true for the work types the active/next task lists draw
// from — containers organize work, they are not work themselves.
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
