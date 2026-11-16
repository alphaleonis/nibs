package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/progress"
	"github.com/alphaleonis/nibs/internal/ui"
)

// columnName is a list column's name in tui.columns. The thirteen the web has
// keep the web's names.
type columnName string

const (
	colID        columnName = "id"
	colParent    columnName = "parent"
	colType      columnName = "type"
	colTitle     columnName = "title"
	colStatus    columnName = "status"
	colPriority  columnName = "priority"
	colEstimate  columnName = "estimate"
	colChildren  columnName = "children"
	colTags      columnName = "tags"
	colMilestone columnName = "milestone"
	colArea      columnName = "area"
	colBlocking  columnName = "blocking"
	colBlockedBy columnName = "blockedBy"
	colCreated   columnName = "created"
	colModified  columnName = "modified"
)

// columnOrder is every column in the order a row draws them. The title comes
// first, after the row's lead, and the ID last.
var columnOrder = []columnName{
	colTitle, colParent, colType, colStatus, colPriority, colEstimate, colChildren, colTags,
	colMilestone, colArea, colBlocking, colBlockedBy, colCreated, colModified, colID,
}

// columnDescriptions say what each column shows, for the column picker.
var columnDescriptions = map[columnName]string{
	colTitle:     "always shown",
	colParent:    "the parent's title",
	colType:      "the type's name",
	colStatus:    "the status's name",
	colPriority:  "the priority's name",
	colEstimate:  "the estimate",
	colChildren:  "done / total children",
	colTags:      "the tags",
	colMilestone: "the milestone's title",
	colArea:      "the area",
	colBlocking:  "nibs it holds back",
	colBlockedBy: "nibs that hold it back",
	colCreated:   "time since it was created",
	colModified:  "time since it last changed",
	colID:        "the ID",
}

// treeView names the tree view in tui.columns.
const treeView = "tree"

// columnViews are the views tui.columns may name.
var columnViews = []string{treeView}

// defaultColumns are the columns a view shows while tui.columns does not name
// it.
var defaultColumns = map[string][]columnName{
	treeView: {colTags, colID},
}

// Widths of the columns that do not take the width of a vocabulary or of the
// IDs.
const (
	tagsColumnWidth     = 20
	nibRefColumnWidth   = 20 // the parent's or the milestone's title
	areaColumnWidth     = 16
	linkCountWidth      = 4 // a mark and up to three digits
	ageColumnWidth      = 8 // "just now", "11mo ago" and "Jul 2024" are the widest
	childrenColumnWidth = 7 // "999/999"
)

// columnWidth returns the cells name takes in a row. The type, status,
// priority and estimate columns are as wide as the widest name of their
// vocabulary in cfg, and the ID column is idWidth wide.
func columnWidth(cfg *config.Config, name columnName, idWidth int) int {
	switch name {
	case colID:
		return idWidth
	case colType:
		return widest(cfg.TypeNames())
	case colStatus:
		return widest(cfg.StatusNames())
	case colPriority:
		return widest(cfg.PriorityNames())
	case colEstimate:
		return widest(cfg.EstimateNames())
	case colTags:
		return tagsColumnWidth
	case colParent, colMilestone:
		return nibRefColumnWidth
	case colArea:
		return areaColumnWidth
	case colBlocking, colBlockedBy:
		return linkCountWidth
	case colCreated, colModified:
		return ageColumnWidth
	case colChildren:
		return childrenColumnWidth
	case colTitle:
	}
	return 0
}

func widest(names []string) int {
	w := 0
	for _, n := range names {
		w = max(w, lipgloss.Width(n))
	}
	return w
}

// rowFacts are what a row's columns show that the nib alone does not hold.
type rowFacts struct {
	parent    string          // the parent's title, or the parent's ID when no nib has it
	milestone string          // the milestone's title, or the stored ID when it names no milestone
	children  progress.Rollup // of the direct children
	blocking  int             // nibs this one blocks
	blockedBy int             // nibs that block this one
}

// rowFactsOf returns the facts of each nib in nibs, which must be the whole
// store. The link counts follow the GraphQL blockedByIds and blockingIds
// fields: a blocker whose status releases its dependents counts in neither,
// and blocking also leaves out the dependents whose status does.
func rowFactsOf(nibs []*nib.Nib, cfg *config.Config) map[string]rowFacts {
	byID := make(map[string]*nib.Nib, len(nibs))
	childStatuses := map[string][]string{}
	for _, n := range nibs {
		byID[n.ID] = n
		if n.Parent != "" {
			childStatuses[n.Parent] = append(childStatuses[n.Parent], n.Status)
		}
	}
	facts := make(map[string]rowFacts, len(nibs))
	blocking := map[string]int{}
	for _, n := range nibs {
		f := rowFacts{parent: n.Parent, milestone: n.Milestone, children: progress.ByCount(childStatuses[n.ID])}
		if p := byID[n.Parent]; p != nil {
			f.parent = p.Title
		}
		if ms := byID[n.Milestone]; ms != nil && ms.EffectiveType() == "milestone" {
			f.milestone = ms.Title
		}
		for _, id := range n.BlockedBy {
			blocker := byID[id]
			if blocker == nil || cfg.StatusReleasesDependents(blocker.Status) {
				continue
			}
			f.blockedBy++
			if !cfg.StatusReleasesDependents(n.Status) {
				blocking[id]++
			}
		}
		facts[n.ID] = f
	}
	for id, count := range blocking {
		f := facts[id]
		f.blocking = count
		facts[id] = f
	}
	return facts
}

// timeNow returns the time the created and modified columns count from. Tests
// replace it.
var timeNow = time.Now

// relativeAge returns how long ago t was, in the buckets the web's
// formatRelative uses, or "" when t is nil.
func relativeAge(t *time.Time, now time.Time) string {
	if t == nil {
		return ""
	}
	const day = 24 * time.Hour
	d := now.Sub(*t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < day:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	case d < 30*day:
		return fmt.Sprintf("%dd ago", int(d/day))
	case d < 365*day:
		return fmt.Sprintf("%dmo ago", int(d/(30*day)))
	}
	return t.UTC().Format("Jan 2006")
}

// cellText returns what column name shows for item and the style it is drawn
// in. The tags and title columns are drawn elsewhere.
func (d itemDelegate) cellText(name columnName, item nibItem) (string, lipgloss.Style) {
	n, f := item.nib, item.facts
	plain := lipgloss.NewStyle()
	colored := func(c string) lipgloss.Style { return plain.Foreground(ui.ResolveColor(c)) }
	switch name {
	case colID:
		return d.display.id(n.ID), plain
	case colParent:
		return f.parent, plain
	case colType:
		var c string
		if t := d.cfg.GetType(n.EffectiveType()); t != nil {
			c = t.Color
		}
		return n.EffectiveType(), colored(c)
	case colStatus:
		var c string
		if s := d.cfg.GetStatus(n.Status); s != nil {
			c = s.Color
		}
		return n.Status, colored(c)
	case colPriority:
		var c string
		if p := d.cfg.GetPriority(n.EffectivePriority()); p != nil {
			c = p.Color
		}
		return n.EffectivePriority(), colored(c)
	case colEstimate:
		return strings.ToUpper(n.Estimate), plain
	case colChildren:
		if f.children.Total == 0 {
			return "", plain
		}
		return fmt.Sprintf("%d/%d", f.children.Done, f.children.Total), plain
	case colMilestone:
		return f.milestone, plain
	case colArea:
		return n.Area, plain
	case colBlocking:
		if f.blocking == 0 {
			return "", plain
		}
		return ui.GlyphBlocking(d.display.ascii()) + fmt.Sprint(f.blocking), blockingStyle
	case colBlockedBy:
		if f.blockedBy == 0 {
			return "", plain
		}
		return ui.GlyphBlocked(d.display.ascii()) + fmt.Sprint(f.blockedBy), blockedStyle
	case colCreated:
		return relativeAge(n.CreatedAt, timeNow()), ui.Muted
	case colModified:
		return relativeAge(n.UpdatedAt, timeNow()), ui.Muted
	case colTitle, colTags:
	}
	return "", plain
}
