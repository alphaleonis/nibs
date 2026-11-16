package tui

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
)

// hintLabel is the text a hint shows for a.
func hintLabel(a *action, st actionState) string {
	if a.Label != nil {
		return a.Label(st)
	}
	return a.Desc
}

// hintRow renders the footer's hint line from table. The row is not clipped.
func hintRow(table []action, st actionState) string {
	return strings.Join(hintItems(table, st), "  ")
}

// hintRowWithin renders the hint line in at most width cells, dropping the
// hints after the last one that fits whole.
func hintRowWithin(table []action, st actionState, width int) string {
	row := ""
	for _, h := range hintItems(table, st) {
		next := h
		if row != "" {
			next = row + "  " + h
		}
		if lipgloss.Width(next) > width {
			break
		}
		row = next
	}
	return row
}

// hintItems renders each hint of the hint line. After a pending prefix they are
// what can follow the prefix; otherwise they are the actions active in st.ctx
// that are ready and whose Hint holds, each with its first key. Hints follow
// actionGroups order, then table order.
func hintItems(table []action, st actionState) []string {
	var hints []string
	add := func(key, text string) {
		hints = append(hints, helpKeyStyle.Render(key)+" "+helpStyle.Render(text))
	}
	for _, a := range actionsByGroup(table, st.ctx) {
		if st.prefix != "" {
			for _, binding := range a.Keys {
				if rest, ok := strings.CutPrefix(binding, st.prefix+" "); ok {
					add(rest, hintLabel(a, st))
				}
			}
			continue
		}
		if a.Hint != nil && len(a.Keys) > 0 && a.Hint(st) && isReady(a, st) {
			add(a.Keys[0], hintLabel(a, st))
		}
	}
	if st.prefix != "" && len(hints) > 0 {
		hints = slices.Insert(hints, 0, helpKeyStyle.Render(st.prefix)+helpStyle.Render(" then"))
	}
	return hints
}

// isReady reports whether a can run in st.
func isReady(a *action, st actionState) bool {
	if a.Ready == nil {
		return true
	}
	ok, _ := a.Ready(st)
	return ok
}

// actionsByGroup returns the actions in table active in ctx, in actionGroups
// order, then table order.
func actionsByGroup(table []action, ctx keyContext) []*action {
	var out []*action
	for _, group := range actionGroups {
		for i := range table {
			if a := &table[i]; a.Group == group && slices.Contains(a.Contexts, ctx) {
				out = append(out, a)
			}
		}
	}
	return out
}
