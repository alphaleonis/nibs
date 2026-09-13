package nibcontext

import (
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/mdsection"
	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
)

// NibRef is the context JSON's reference to a nib. Type is always the effective
// type; Estimate is omitted when unset.
type NibRef struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Type     string `json:"type,omitempty"`
	Estimate string `json:"estimate,omitempty"`
}

// ContainerSummary is an open milestone and its active phase, if any, for
// overview mode.
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

// BuildSummary summarizes the membership of rootID, or, when rootID is "", all
// in-progress leaf work and the open milestones. "in-progress" and "todo" are
// matched literally; cfg decides only which milestones are closed.
func BuildSummary(allNibs []*nib.Nib, rootID string, cfg *config.Config) Summary {
	return BuildSummaryWithView(allNibs, membership.Compute(allNibs), rootID, cfg)
}

// BuildSummaryWithView is BuildSummary over a view the caller already computed.
// Build view from allNibs.
func BuildSummaryWithView(allNibs []*nib.Nib, view *membership.View, rootID string, cfg *config.Config) Summary {
	byID := indexByID(allNibs)

	sum := Summary{
		ActiveTasks: []*NibRef{},
		NextTasks:   []*NibRef{},
	}

	if rootID == "" {
		active := filterByStatusAndLeaf(allNibs, "in-progress")
		nib.SortByOrder(active)
		sum.ActiveTasks = toNibRefs(active)

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

			var phaseCandidates []*nib.Nib
			for _, n := range view.DirectMembers(ms.ID) {
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

	descendants := view.Members(rootID)

	var phaseCandidates []*nib.Nib
	for _, n := range view.DirectMembers(rootID) {
		if n.Type == "epic" && n.Status == "in-progress" {
			phaseCandidates = append(phaseCandidates, n)
		}
	}
	sortDirectMembers(root, phaseCandidates)
	if len(phaseCandidates) > 0 {
		sum.ActivePhase = newNibRef(phaseCandidates[0])
	}

	activeTasks := filterByStatusAndLeaf(descendants, "in-progress")
	nib.SortByOrder(activeTasks)
	sum.ActiveTasks = toNibRefs(activeTasks)

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

// sortDirectMembers sorts a container's direct members by the key that positions
// them in it: milestone_order for a milestone, order otherwise.
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

func newNibRef(n *nib.Nib) *NibRef {
	return &NibRef{
		ID:       n.ID,
		Title:    n.Title,
		Status:   n.Status,
		Type:     n.EffectiveType(),
		Estimate: n.Estimate,
	}
}

func toNibRefs(nibs []*nib.Nib) []*NibRef {
	refs := make([]*NibRef, len(nibs))
	for i, n := range nibs {
		refs[i] = newNibRef(n)
	}
	return refs
}

func indexByID(nibs []*nib.Nib) map[string]*nib.Nib {
	m := make(map[string]*nib.Nib, len(nibs))
	for _, n := range nibs {
		m[n.ID] = n
	}
	return m
}

// isLeafType reports whether typ is a type the active and next task lists draw
// from. Update it when config.DefaultTypes gains a work type.
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
