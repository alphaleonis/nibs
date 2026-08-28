package cmd

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/template"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/progress"
	"github.com/spf13/cobra"
)

//go:embed roadmap.tmpl
var roadmapTemplateContent string

var (
	roadmapJSON        bool
	roadmapIncludeDone bool
	roadmapStatus      []string
	roadmapNoStatus    []string
	roadmapNoLinks     bool
	roadmapLinkPrefix  string
)

// roadmapData holds the structured roadmap for JSON output.
type roadmapData struct {
	Milestones []milestoneGroup `json:"milestones"`
	Backlog    *backlogGroup    `json:"backlog,omitempty"`
}

// backlogGroup is the work no milestone holds, rendered under the `## Backlog`
// heading whenever there are milestone sections to separate it from. Its queue is ONE ordered list because a backlog epic and a backlog
// root item are siblings in the same root `order` scope: two arrays would put
// every epic ahead of every item, which is an arrangement nobody made.
type backlogGroup struct {
	Queue []queueEntry `json:"queue,omitempty"`
}

// milestoneGroup represents a milestone and its queue. Progress is the
// canonical child-completion rollup over the milestone's DIRECT members
// (progress.ByCount) — the same value `nibs get <milestone> -f progress`
// reports — computed over every real member, independent of the display
// filters.
type milestoneGroup struct {
	Milestone *nib.Nib        `json:"milestone"`
	Progress  progress.Rollup `json:"progress"`
	// Queue is the milestone's direct members in milestone_order: ONE ordered
	// list, epics and loose work standing wherever their keys put them. The
	// JSON carries that single list rather than an epics/other split for the
	// same reason the Markdown reads as one queue — two arrays do not express
	// an interleaving on their own. A consumer could re-merge them on each
	// nib's milestone_order, which does ship in the payload, but only by
	// re-implementing this renderer's sort rule (keyed before unkeyed, then the
	// title-then-ID tiebreak), which is not a published contract — and an
	// unkeyed member ships no key at all.
	Queue []queueEntry `json:"queue,omitempty"`
}

// queueEntry is one position in an ordered list of members — a milestone's
// queue, or the backlog. Nib is the member standing there and is ALWAYS
// present; a member that expands into a decomposition also carries that
// decomposition and the canonical child-completion rollup over it.
//
// One flat shape rather than a union of "epic entry" and "item entry": the
// list is a list of members, some of which expand, so an entry has a member
// either way. A union would make a consumer branch on which key is present,
// and would nest the member a level deeper here than the same object sits
// elsewhere in the same payload. cmd/next.go and cmd/check.go are the local
// precedent for the principle this follows — branch on a field's value, not on
// whether the field is there — reached by an always-present field and a `kind`
// tag respectively; neither is a union this shape has to imitate.
//
// Build entries through queueItem and queueContainer. Progress and Items are
// set together or not at all, and the Markdown branch that reads Progress
// trusts that pairing.
type queueEntry struct {
	Nib      *nib.Nib         `json:"nib"`
	Progress *progress.Rollup `json:"progress,omitempty"`
	Items    []*nib.Nib       `json:"items,omitempty"`
}

// queueItem builds the entry for a member that stands alone at its position.
func queueItem(b *nib.Nib) queueEntry {
	return queueEntry{Nib: b}
}

// queueContainer builds the entry for a member that expands into its
// decomposition. items is non-empty at every call site: a container holding no
// outstanding scope leaves the list rather than standing in it empty, so the
// rendered block never opens on nothing.
func queueContainer(b *nib.Nib, rollup progress.Rollup, items []*nib.Nib) queueEntry {
	return queueEntry{Nib: b, Progress: &rollup, Items: items}
}

var roadmapCmd = &cobra.Command{
	Use:   "roadmap",
	Short: "Generate a Markdown roadmap from milestones and epics",
	Args:  codedNoArgs(&roadmapJSON), // renders the whole store; takes no positional args
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		// Query all nibs via GraphQL resolver
		resolver := app.newResolver()
		allNibs, err := resolver.Query().Nibs(context.Background(), nil, nil)
		if err != nil {
			return fmt.Errorf("querying nibs: %w", err)
		}

		// Build the roadmap
		data := buildRoadmap(allNibs, roadmapIncludeDone, roadmapStatus, roadmapNoStatus, app.Config())

		// JSON output
		if roadmapJSON {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(data)
		}

		// Markdown output
		links := !roadmapNoLinks
		linkPrefix := roadmapLinkPrefix
		if links && linkPrefix == "" {
			// Default: relative path from cwd to .nibs directory
			linkPrefix = defaultLinkPrefix(app.Core)
		}
		md := renderRoadmapMarkdown(data, links, linkPrefix)
		fmt.Print(md)
		return nil
	},
}

// buildRoadmap constructs the roadmap data structure from nibs. Membership —
// which milestones exist, what belongs to each, what is left in the backlog —
// comes from internal/membership; everything here is display policy: status
// filters, the includeDone rule, sorting, and the "earns its place by holding
// outstanding scope" tests.
func buildRoadmap(allNibs []*nib.Nib, includeDone bool, statusFilter, noStatusFilter []string, cfg *config.Config) *roadmapData {
	view := membership.Compute(allNibs)

	// Find milestones, applying status filters
	var milestones []*nib.Nib
	for _, b := range view.Milestones() {
		if len(statusFilter) > 0 && !containsStatus(statusFilter, b.Status) {
			continue
		}
		if len(noStatusFilter) > 0 && containsStatus(noStatusFilter, b.Status) {
			continue
		}
		milestones = append(milestones, b)
	}

	// Sort milestones by status order, then by created date
	sortByStatusThenCreated(milestones, cfg)

	// Build milestone groups
	var milestoneGroups []milestoneGroup
	for _, m := range milestones {
		group := buildMilestoneGroup(m, view, includeDone, cfg)
		// A milestone earns its place by holding outstanding scope — the same
		// rule as epics, one level up.
		if len(group.Queue) > 0 {
			milestoneGroups = append(milestoneGroups, group)
		}
	}

	// The backlog comes from the view, computed against every
	// DECLARED milestone rather than the status-filtered list above — work
	// under a status-hidden milestone is scheduled work the filter chose not
	// to show, not backlog. (The old two-level walk leaked it here; restoring
	// that would be a deliberate policy change, not a default.)
	rem := view.Backlog()

	// The backlog is the tree, filtered (decision 2.5). Its epics and its
	// root-level items are SIBLINGS in the same root `order` scope, so they
	// collect into one list and sort once by that scope's key — the tree's own
	// arrangement, rather than one the renderer invented by ranking epics
	// ahead of everything else.
	var backlog []queueEntry
	for _, eg := range rem.Epics {
		if entry, ok := roadmapEntry(eg.Epic, eg.Items, includeDone, cfg); ok {
			backlog = append(backlog, entry)
		}
	}
	for _, b := range rem.Other {
		// A backlog root carries its decomposition for the same reason a queue
		// entry does: it is the only place that work appears at all.
		if entry, ok := roadmapEntry(b, view.DirectMembers(b.ID), includeDone, cfg); ok {
			backlog = append(backlog, entry)
		}
	}
	slices.SortStableFunc(backlog, func(a, b queueEntry) int {
		return nib.CompareByKey(a.Nib, b.Nib, func(n *nib.Nib) string { return n.Order })
	})

	// Build the backlog group if there's content
	var backlogGrp *backlogGroup
	if len(backlog) > 0 {
		backlogGrp = &backlogGroup{Queue: backlog}
	}

	return &roadmapData{
		Milestones: milestoneGroups,
		Backlog:    backlogGrp,
	}
}

// buildMilestoneGroup builds a milestone's section: the progress rollup over
// its assigned set, and its queue.
func buildMilestoneGroup(m *nib.Nib, view *membership.View, includeDone bool, cfg *config.Config) milestoneGroup {
	members := view.DirectMembers(m.ID)
	group := milestoneGroup{
		Milestone: m,
		// % complete over the milestone's real direct members (epics + direct
		// items), computed over the full member set regardless of includeDone.
		Progress: progress.ByCount(childStatuses(members)),
	}

	// The queue is the whole direct-member set in ONE order, and milestone_order
	// is the only key that expresses it: an assignee's `order` is a live
	// position in a DIFFERENT scope — its structural sibling group, or the root
	// group — so ordering the queue by it would render an arrangement nobody
	// made. Sorting the epics apart from the rest would do the same. Unkeyed
	// members fall to the title tiebreak.
	nib.SortByMilestoneOrder(members)

	for _, member := range members {
		if entry, ok := roadmapEntry(member, view.DirectMembers(member.ID), includeDone, cfg); ok {
			group.Queue = append(group.Queue, entry)
		}
	}

	return group
}

// roadmapEntry decides how one member of a milestone's queue or of the backlog
// appears, and whether it appears at all. Both callers ask the same question,
// so they ask it in one place.
//
// A member that still holds outstanding scope EXPANDS, carrying that scope with
// it — whatever its type. Nothing here asks whether the member is an epic:
// `nibs catalog hierarchy` declares three container types (epic, bug, feature),
// and a type test naming one of them dropped the other two's children out of
// every view — not into the backlog, which reaches only roots and unassigned
// epics, and not into the rollup, which counts the container as one unit. The
// question that matters is whether a member has scope under it, and the data
// answers that without a type table to keep in step.
//
// A member with nothing outstanding under it stands alone, subject to its own
// status. That keeps two cases straight: a container closed over a deferred
// child keeps rendering it, because closing a parent does not resolve the
// child; and an open container whose children are all closed still appears,
// because it is open work the milestone's own progress is already counting.
//
// The decomposition takes the PARENT-scope order key. It is a different axis
// from the queue its container stands in, and the rollup is computed over the
// unfiltered decomposition so the display filters cannot move the arithmetic.
func roadmapEntry(member *nib.Nib, decomposition []*nib.Nib, includeDone bool, cfg *config.Config) (queueEntry, bool) {
	items := filterChildren(decomposition, includeDone, cfg)
	if len(items) > 0 {
		nib.SortByOrder(items)
		return queueContainer(member, progress.ByCount(childStatuses(decomposition)), items), true
	}
	if !staysOnRoadmap(member.Status, includeDone, cfg) {
		return queueEntry{}, false
	}
	return queueItem(member), true
}

// childStatuses projects a child slice to its status strings, the input
// progress.ByCount needs to build a canonical progress rollup.
func childStatuses(children []*nib.Nib) []string {
	statuses := make([]string, len(children))
	for i, c := range children {
		statuses[i] = c.Status
	}
	return statuses
}

// staysOnRoadmap reports whether a nib is still outstanding scope and belongs on
// the board. Work whose status released its dependents is finished with — it
// either happened (completed) or it never will (scrapped), and nothing waits on
// it — so the default view drops it. Everything else stays, including a deferred
// nib: it is closed, but the work is coming back, so it still blocks its
// dependents and it is still scope someone has to deal with. Roadmap has no
// hidden-closed disclosure, so a deferred nib that dropped out would be
// invisible rather than merely collapsed.
//
// This is the visibility half of the seam with progress.ByCount: without
// includeDone, the nibs this keeps are exactly the ones that rollup counts in
// Total but not in Done.
func staysOnRoadmap(status string, includeDone bool, cfg *config.Config) bool {
	return includeDone || !cfg.StatusReleasesDependents(status)
}

// filterChildren drops the children that are no longer outstanding scope, unless
// includeDone asks for the full set. See staysOnRoadmap.
func filterChildren(children []*nib.Nib, includeDone bool, cfg *config.Config) []*nib.Nib {
	var filtered []*nib.Nib
	for _, b := range children {
		if staysOnRoadmap(b.Status, includeDone, cfg) {
			filtered = append(filtered, b)
		}
	}
	return filtered
}

// containsStatus checks if a status is in the list.
func containsStatus(statuses []string, status string) bool {
	return slices.Contains(statuses, status)
}

// sortByStatusThenCreated sorts nibs by status order, then by created date.
func sortByStatusThenCreated(nibs []*nib.Nib, cfg interface{ StatusNames() []string }) {
	statusOrder := make(map[string]int)
	for i, s := range cfg.StatusNames() {
		statusOrder[s] = i
	}

	sort.Slice(nibs, func(i, j int) bool {
		oi, oj := statusOrder[nibs[i].Status], statusOrder[nibs[j].Status]
		if oi != oj {
			return oi < oj
		}
		// Then by created date (oldest first for milestones)
		if nibs[i].CreatedAt != nil && nibs[j].CreatedAt != nil {
			return nibs[i].CreatedAt.Before(*nibs[j].CreatedAt)
		}
		return nibs[i].ID < nibs[j].ID
	})
}

// renderRoadmapMarkdown renders the roadmap as Markdown using the template.
func renderRoadmapMarkdown(data *roadmapData, links bool, linkPrefix string) string {
	// Create template with closures that capture link settings
	tmpl := template.Must(
		template.New("roadmap").Funcs(template.FuncMap{
			"firstParagraph": firstParagraph,
			"typeBadge":      typeBadge,
			"nibRef": func(b *nib.Nib) string {
				return renderNibRef(b, links, linkPrefix)
			},
		}).Parse(roadmapTemplateContent),
	)

	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		panic(err)
	}
	return sb.String()
}

// renderNibRef renders a nib ID, optionally as a markdown link.
func renderNibRef(b *nib.Nib, asLink bool, linkPrefix string) string {
	if !asLink {
		return "(" + b.ID + ")"
	}
	if linkPrefix == "" {
		return fmt.Sprintf("([%s](%s))", b.ID, b.Path)
	}
	// Ensure prefix ends with / for clean concatenation
	if !strings.HasSuffix(linkPrefix, "/") {
		linkPrefix += "/"
	}
	return fmt.Sprintf("([%s](%s%s))", b.ID, linkPrefix, b.Path)
}

// badgeHexes maps a TypeConfig.Color name to the shields.io hex the roadmap
// renders it as. Colors deliberately stay out of the shared vocabulary — each
// surface owns its palette — so the hexes live with this renderer; a config
// color without an entry falls back to gray.
var badgeHexes = map[string]string{
	"red":    "d73a4a",
	"green":  "0e8a16",
	"blue":   "1d76db",
	"purple": "5319e7",
}

// typeBadge returns a shields.io badge markdown for the nib type, colored via
// the type's config color so the type→color pairing is declared once. Uses
// EffectiveType so a type-less nib still renders its "task" badge rather than
// an empty one.
func typeBadge(b *nib.Nib) string {
	typeName := b.EffectiveType()
	color := "gray"
	if tc := config.Default().GetType(typeName); tc != nil {
		if hex, ok := badgeHexes[tc.Color]; ok {
			color = hex
		}
	}
	return fmt.Sprintf("![%s](https://img.shields.io/badge/%s-%s?style=flat-square)", typeName, typeName, color)
}

// defaultLinkPrefix returns the relative path from cwd to the .nibs directory.
func defaultLinkPrefix(core *nibcore.Core) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(cwd, core.Root())
	if err != nil {
		return ""
	}
	// Convert to forward slashes for URL compatibility
	return filepath.ToSlash(rel)
}

// firstParagraph extracts the first paragraph from a body text.
func firstParagraph(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}

	// Find the first blank line (paragraph separator)
	lines := strings.Split(body, "\n")
	var para []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			break
		}
		// Skip markdown headers
		if strings.HasPrefix(line, "#") {
			continue
		}
		para = append(para, strings.TrimSpace(line))
	}

	result := strings.Join(para, " ")
	// Truncate if too long
	if len(result) > 200 {
		result = result[:197] + "..."
	}
	return result
}

func init() {
	roadmapCmd.Flags().BoolVar(&roadmapJSON, "json", false, "Output as JSON")
	roadmapCmd.Flags().BoolVar(&roadmapIncludeDone, "include-done", false, "Include completed items")
	roadmapCmd.Flags().StringArrayVar(&roadmapStatus, "status", nil, "Filter milestones by status (can be repeated)")
	roadmapCmd.Flags().StringArrayVar(&roadmapNoStatus, "no-status", nil, "Exclude milestones by status (can be repeated)")
	roadmapCmd.Flags().BoolVar(&roadmapNoLinks, "no-links", false, "Don't render nib IDs as markdown links")
	roadmapCmd.Flags().StringVar(&roadmapLinkPrefix, "link-prefix", "", "URL prefix for links")
	rootCmd.AddCommand(roadmapCmd)
}
