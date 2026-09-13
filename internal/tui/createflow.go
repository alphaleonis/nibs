package tui

import (
	"slices"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibtypes"
)

// openCreateTypePickerMsg requests opening the type picker for the create flow
type openCreateTypePickerMsg struct {
	defaultType string
}

// createTypeSelectedMsg is sent when a type is selected during the create flow
type createTypeSelectedMsg struct {
	nibType string
}

// defaultTypeForContext suggests a type for a new nib from the hierarchy rules:
//
//   - a selection that can take children: feature if legal, else task (no
//     selection counts, since ValidChildTypes("") is every type)
//   - a childless task-level selection (task, research, an unknown type): task
//   - otherwise (a milestone): the selected type
func defaultTypeForContext(selectedNibType string) string {
	children := nibtypes.ValidChildTypes(selectedNibType)
	for _, candidate := range []string{"feature", "task"} {
		if slices.Contains(children, candidate) {
			return candidate
		}
	}
	if sameParentTypes(nibtypes.ValidParentTypes(selectedNibType), nibtypes.ValidParentTypes("task")) {
		return "task"
	}
	return selectedNibType
}

// inferParent returns the parent and afterID for a new nib of chosenType:
//
//   - a valid child of the selected nib's type: under the selected nib
//   - the same level (identical valid parent types): after the selected nib,
//     under its parent; at root with no position when it has no parent
//   - otherwise, or with nothing selected: at root
func inferParent(chosenType string, selectedNib *nib.Nib) (parentID string, afterID string) {
	if selectedNib == nil {
		return "", ""
	}

	// EffectiveType: ValidChildTypes("") means every type, which is wrong for an
	// existing type-less nib.
	validChildren := nibtypes.ValidChildTypes(selectedNib.EffectiveType())
	for _, childType := range validChildren {
		if childType == chosenType {
			return selectedNib.ID, ""
		}
	}

	selectedParentTypes := nibtypes.ValidParentTypes(selectedNib.EffectiveType())
	chosenParentTypes := nibtypes.ValidParentTypes(chosenType)
	if sameParentTypes(selectedParentTypes, chosenParentTypes) {
		if selectedNib.Parent != "" {
			return selectedNib.Parent, selectedNib.ID
		}
		return "", ""
	}

	return "", ""
}

// sameParentTypes reports whether a and b hold the same types, ignoring order.
// It assumes neither holds duplicates.
func sameParentTypes(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[s] = true
	}
	for _, s := range b {
		if !set[s] {
			return false
		}
	}
	return true
}
