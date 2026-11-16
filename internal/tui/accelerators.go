package tui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/list"
)

// acceleratorPool is every key a picker row can take, in the order rows take
// them when their hint offers none.
var acceleratorPool = strings.Split("1234567890abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", "")

// takenKeys returns the keys an action of table in ctx takes, either as a
// whole binding or as the first key of a sequence.
func takenKeys(table []action, ctx keyContext) map[string]bool {
	taken := map[string]bool{}
	for _, a := range table {
		if !slices.Contains(a.Contexts, ctx) {
			continue
		}
		for _, binding := range a.Keys {
			taken[strings.Fields(binding)[0]] = true
		}
	}
	return taken
}

// pickerOpeners are the actions that open each picker whose rows write a
// field.
var pickerOpeners = map[keyContext][]actionName{
	ctxStatusPicker:   {actStatus},
	ctxTypePicker:     {actType, actCreate},
	ctxParentPicker:   {actParent},
	ctxBlockingPicker: {actBlocking},
	ctxPriorityPicker: {actPriority},
	ctxEstimatePicker: {actEstimate},
	ctxFilterPicker:   {actFilterPick},
}

// modifyRowFields are the pickers the modify picker's rows open, by row label.
var modifyRowFields = map[keyContext]string{
	ctxPriorityPicker:  "priority",
	ctxEstimatePicker:  "estimate",
	ctxMilestonePicker: "milestone",
	ctxAreaPicker:      "area",
}

// openerKeys returns the last key of each binding that opens ctx's picker under
// table, and the modify picker's key for the row that opens it. A row with one
// of them as its accelerator would be chosen by a double tap or a key repeat of
// the key that opened the picker.
func openerKeys(table []action, ctx keyContext) map[string]bool {
	keys := map[string]bool{}
	for _, name := range pickerOpeners[ctx] {
		addBindingEnds(keys, table, name)
	}
	if label, ok := modifyRowFields[ctx]; ok {
		keys[modifyRowKey(table, label)] = true
	}
	return keys
}

// addBindingEnds adds the last key of each of name's bindings under table to
// keys.
func addBindingEnds(keys map[string]bool, table []action, name actionName) {
	if a := findAction(table, name); a != nil {
		for _, binding := range a.Keys {
			fields := strings.Fields(binding)
			keys[fields[len(fields)-1]] = true
		}
	}
}

// modifyRowKey returns the modify picker's accelerator for the row labeled
// label under table, or "" when it has none.
func modifyRowKey(table []action, label string) string {
	hints := make([]string, len(modifyRows))
	for i, row := range modifyRows {
		hints[i] = row.label
	}
	keys := assignAccelerators(table, ctxModifyPicker, hints)
	if i := slices.Index(hints, label); i >= 0 {
		return keys[i]
	}
	return ""
}

// freeAccelerators returns acceleratorPool without the keys an action of table
// in ctx takes, see takenKeys, and without the keys that open ctx's picker, see
// openerKeys.
func freeAccelerators(table []action, ctx keyContext) []string {
	taken := takenKeys(table, ctx)
	for k := range openerKeys(table, ctx) {
		taken[k] = true
	}
	free := make([]string, 0, len(acceleratorPool))
	for _, k := range acceleratorPool {
		if !taken[k] {
			free = append(free, k)
		}
	}
	return free
}

// assignAccelerators gives each row a distinct key free in ctx under table: the
// first free letter of its hint, else the first free key left in the pool. A
// row gets "" once the free keys run out.
func assignAccelerators(table []action, ctx keyContext, hints []string) []string {
	return assignFrom(freeAccelerators(table, ctx), hints)
}

// assignFrom gives each row a distinct key of free: the first one among the
// letters of its hint, else the first one left. A row gets "" once the keys
// run out.
func assignFrom(free, hints []string) []string {
	used := map[string]bool{}
	keys := make([]string, len(hints))
	for i, hint := range hints {
		for _, r := range hint {
			k := string(r)
			if slices.Contains(free, k) && !used[k] {
				keys[i] = k
				break
			}
		}
		used[keys[i]] = true
	}
	next := 0
	for i := range keys {
		if keys[i] != "" {
			continue
		}
		for next < len(free) && used[free[next]] {
			next++
		}
		if next < len(free) {
			keys[i] = free[next]
			used[keys[i]] = true
		}
	}
	return keys
}

// fixedAccelerator is a picker row whose accelerator is set when the picker
// opens, so the row keeps it while the list scrolls or filters.
type fixedAccelerator interface {
	accelerator() string
}

// rowAccelerator returns the accelerator of l's visible row i. A row without a
// fixed one takes the key in pageKeys at its place on the current page, so the
// keys follow the page. It returns "" when the row has none.
func rowAccelerator(l list.Model, pageKeys []string, i int) string {
	items := l.VisibleItems()
	if i < 0 || i >= len(items) {
		return ""
	}
	if row, ok := items[i].(fixedAccelerator); ok {
		return row.accelerator()
	}
	start, end := l.Paginator.GetSliceBounds(len(items))
	if i < start || i >= end || i-start >= len(pageKeys) {
		return ""
	}
	return pageKeys[i-start]
}

// selectAccelerator moves l's cursor to the visible row whose accelerator is
// key. It reports false, leaving the cursor, when no row has it.
func selectAccelerator(l *list.Model, pageKeys []string, key string) bool {
	for i := range l.VisibleItems() {
		if rowAccelerator(*l, pageKeys, i) == key {
			l.Select(i)
			return true
		}
	}
	return false
}

// renderAccelerator draws a row's accelerator and the space after it, or two
// spaces for a row without one, so every row's text starts in one column.
func renderAccelerator(key string) string {
	if key == "" {
		return "  "
	}
	return helpKeyStyle.Render(key) + " "
}
