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
	Milestones  []milestoneGroup  `json:"milestones"`
	Unscheduled *unscheduledGroup `json:"unscheduled,omitempty"`
}

// unscheduledGroup represents items not assigned to any milestone.
type unscheduledGroup struct {
	Epics []epicGroup `json:"epics,omitempty"`
	Other []*nib.Nib  `json:"other,omitempty"`
}

// milestoneGroup represents a milestone and its contents. Progress is the
// canonical child-completion rollup over the milestone's DIRECT children
// (progress.ByCount) — the same value `nibs get <milestone> -f progress`
// reports — computed over every real child, independent of the display filters.
type milestoneGroup struct {
	Milestone *nib.Nib        `json:"milestone"`
	Progress  progress.Rollup `json:"progress"`
	Epics     []epicGroup     `json:"epics,omitempty"`
	Other     []*nib.Nib      `json:"other,omitempty"`
}

// epicGroup represents an epic and its child items. Progress is the canonical
// child-completion rollup over the epic's direct children.
type epicGroup struct {
	Epic     *nib.Nib        `json:"epic"`
	Progress progress.Rollup `json:"progress"`
	Items    []*nib.Nib      `json:"items,omitempty"`
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
// which milestones exist, what belongs to each, what remains unscheduled —
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
		if len(group.Epics) > 0 || len(group.Other) > 0 {
			milestoneGroups = append(milestoneGroups, group)
		}
	}

	// The unscheduled remainder comes from the view, computed against every
	// DECLARED milestone rather than the status-filtered list above — work
	// under a status-hidden milestone is scheduled work the filter chose not
	// to show, not backlog. (The old two-level walk leaked it here; restoring
	// that would be a deliberate policy change, not a default.)
	rem := view.Unscheduled()

	var unscheduledEpics []epicGroup
	for _, eg := range rem.Epics {
		// Build the epic group if it still holds outstanding scope.
		epicItems := filterChildren(eg.Items, includeDone, cfg)
		if len(epicItems) > 0 {
			sortByTypeThenStatus(epicItems, cfg)
			unscheduledEpics = append(unscheduledEpics, epicGroup{
				Epic:     eg.Epic,
				Progress: progress.ByCount(childStatuses(eg.Items)),
				Items:    epicItems,
			})
		}
	}

	// Sort unscheduled epics by title
	sort.Slice(unscheduledEpics, func(i, j int) bool {
		return unscheduledEpics[i].Epic.Title < unscheduledEpics[j].Epic.Title
	})

	// Orphan items: root-level work, kept while it stays on the roadmap.
	var orphanItems []*nib.Nib
	for _, b := range rem.Other {
		if !staysOnRoadmap(b.Status, includeDone, cfg) {
			continue
		}
		orphanItems = append(orphanItems, b)
	}

	// Sort orphan items
	sortByTypeThenStatus(orphanItems, cfg)

	// Build unscheduled group if there's content
	var unscheduled *unscheduledGroup
	if len(unscheduledEpics) > 0 || len(orphanItems) > 0 {
		unscheduled = &unscheduledGroup{
			Epics: unscheduledEpics,
			Other: orphanItems,
		}
	}

	return &roadmapData{
		Milestones:  milestoneGroups,
		Unscheduled: unscheduled,
	}
}

// buildMilestoneGroup builds a milestone group with its epics and other items.
func buildMilestoneGroup(m *nib.Nib, view *membership.View, includeDone bool, cfg *config.Config) milestoneGroup {
	tree := view.Grouped(m.ID)
	group := milestoneGroup{
		Milestone: m,
		// % complete over the milestone's real direct members (epics + direct
		// items), computed over the full member set regardless of includeDone.
		Progress: progress.ByCount(childStatuses(view.DirectMembers(m.ID))),
	}

	// Build epic groups
	for _, eg := range tree.Epics {
		epicItems := filterChildren(eg.Items, includeDone, cfg)
		// Include epics that still hold outstanding scope. An epic that closed
		// over a deferred child keeps rendering it — closing the parent does not
		// resolve the child.
		if len(epicItems) > 0 {
			sortByTypeThenStatus(epicItems, cfg)
			group.Epics = append(group.Epics, epicGroup{
				Epic:     eg.Epic,
				Progress: progress.ByCount(childStatuses(eg.Items)),
				Items:    epicItems,
			})
		}
	}

	// Build "Other" list: the milestone's direct non-epic members
	// (With single parent enforcement, items can't be both under an epic and directly under the milestone)
	var other []*nib.Nib
	for _, child := range tree.Other {
		if staysOnRoadmap(child.Status, includeDone, cfg) {
			other = append(other, child)
		}
	}

	// Sort epics by their position in the milestone's queue: assignees carry
	// milestone_order, not the parent-scope order key (which they lack), and a
	// title sort here would shuffle the queue the user arranged. Unkeyed epics
	// fall to the title tiebreak.
	slices.SortStableFunc(group.Epics, func(a, b epicGroup) int {
		return nib.CompareByKey(a.Epic, b.Epic, func(n *nib.Nib) string { return n.MilestoneOrder })
	})

	// Sort other items
	sortByTypeThenStatus(other, cfg)
	group.Other = other

	return group
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

// sortByTypeThenStatus sorts nibs by type order, then status order, then by ID.
func sortByTypeThenStatus(nibs []*nib.Nib, cfg interface {
	StatusNames() []string
	TypeNames() []string
}) {
	statusOrder := make(map[string]int)
	for i, s := range cfg.StatusNames() {
		statusOrder[s] = i
	}
	typeOrder := make(map[string]int)
	for i, t := range cfg.TypeNames() {
		typeOrder[t] = i
	}

	sort.Slice(nibs, func(i, j int) bool {
		// First by type (EffectiveType so a type-less nib sorts as "task", not at
		// the zero-value/first slot).
		ti, tj := typeOrder[nibs[i].EffectiveType()], typeOrder[nibs[j].EffectiveType()]
		if ti != tj {
			return ti < tj
		}
		// Then by status
		si, sj := statusOrder[nibs[i].Status], statusOrder[nibs[j].Status]
		if si != sj {
			return si < sj
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
