package tui

import (
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

// defaultTypeForContext computes the smart default type for a new nib
// based on the type of the currently selected nib.
func defaultTypeForContext(selectedNibType string) string {
	switch selectedNibType {
	case "milestone":
		return "epic"
	case "epic":
		return "feature"
	case "feature", "bug", "task", "research":
		return "task"
	default:
		return "feature" // no selection or unknown type
	}
}

// inferParent determines the appropriate parent and afterID for a new nib
// based on the chosen type and the currently selected nib.
//
// Rules:
//   - If chosen type is a valid child of the selected nib's type → parent = selected nib
//   - If chosen type is the same level as selected nib → sibling (parent = selected's parent, afterID = selected's ID)
//   - If chosen type is higher level → no parent (root level)
//   - If no nib is selected → no parent (root level)
func inferParent(chosenType string, selectedNib *nib.Nib) (parentID string, afterID string) {
	if selectedNib == nil {
		return "", ""
	}

	// Check if chosen type can be a child of the selected nib's type. EffectiveType
	// so a type-less nib is treated as "task" (ValidChildTypes special-cases "" as
	// "no parent → all types", which would be wrong for an existing leaf nib).
	validChildren := nibtypes.ValidChildTypes(selectedNib.EffectiveType())
	for _, childType := range validChildren {
		if childType == chosenType {
			return selectedNib.ID, ""
		}
	}

	// Check if chosen type is the same level (same valid parent types) → sibling
	selectedParentTypes := nibtypes.ValidParentTypes(selectedNib.EffectiveType())
	chosenParentTypes := nibtypes.ValidParentTypes(chosenType)
	if sameParentTypes(selectedParentTypes, chosenParentTypes) {
		if selectedNib.Parent != "" {
			return selectedNib.Parent, selectedNib.ID
		}
		// Both at root level — no parent, no positioning
		return "", ""
	}

	// Higher level or incompatible → root level
	return "", ""
}

// sameParentTypes returns true if both slices contain the same elements.
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
