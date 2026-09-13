package nibtypes

import (
	"fmt"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
)

// allTypeNames returns the canonical list of type names from config.DefaultTypes.
func allTypeNames() []string {
	names := make([]string, len(config.DefaultTypes))
	for i, t := range config.DefaultTypes {
		names[i] = t.Name
	}
	return names
}

// JoinWithOr joins strings with commas and "or" for the last element (Oxford comma style).
func JoinWithOr(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " or " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", or " + items[len(items)-1]
	}
}

// HierarchyError is an illegal parent-type relationship. Allowed lists the parent
// types the child may take, and is empty when it may take none.
type HierarchyError struct {
	ChildType  string
	ParentType string
	Allowed    []string
}

func (e *HierarchyError) Error() string {
	if len(e.Allowed) == 0 {
		return fmt.Sprintf("%s cannot have a parent", e.ChildType)
	}
	return fmt.Sprintf("%s can only have a parent of type %s, not %s", e.ChildType, JoinWithOr(e.Allowed), e.ParentType)
}

// ValidateParentType returns nil when childType may take parentType as a parent,
// and a *HierarchyError otherwise.
func ValidateParentType(childType, parentType string) error {
	allowed := ValidParentTypes(childType)
	if allowed == nil {
		return &HierarchyError{ChildType: childType, ParentType: parentType}
	}
	for _, a := range allowed {
		if a == parentType {
			return nil
		}
	}
	return &HierarchyError{ChildType: childType, ParentType: parentType, Allowed: allowed}
}

// ValidParentTypes returns the parent types nibType may take, or nil for none.
// Milestones neither take a parent nor serve as one; work reaches a milestone
// through `milestone:`. Epics are roots.
func ValidParentTypes(nibType string) []string {
	switch nibType {
	case "milestone":
		return nil
	case "epic":
		return nil
	case "feature", "bug":
		return []string{"epic"}
	case "task", "research":
		return []string{"epic", "feature", "bug"}
	default:
		return []string{"epic", "feature", "bug"} // default for unknown types
	}
}

// CanHaveParent reports whether nibType may take a parent. An epic cannot either,
// so false does not mean milestone.
func CanHaveParent(nibType string) bool {
	return ValidParentTypes(nibType) != nil
}

// The assignment axes, named by their front-matter keys. AxisError.Axis holds one.
const (
	AxisMilestone = "milestone"
	AxisArea      = "area"
)

// AxisError is an assignment axis a nib's type may not carry. Classify it with
// errors.As, not by its message.
type AxisError struct {
	NibType string // the type refusing the axis
	Axis    string // AxisMilestone or AxisArea
}

func (e *AxisError) Error() string {
	if e.Axis == AxisMilestone {
		return fmt.Sprintf("a %s cannot be assigned to a milestone", e.NibType)
	}
	return fmt.Sprintf("a %s cannot have an area", e.NibType)
}

// RefusedAxes returns every axis nibType refuses among those the nib carries,
// milestone before area. Use it where every refusal must be reported;
// ValidateAxes stops at the first.
func RefusedAxes(nibType, milestone, area string) []string {
	if nibType != "milestone" {
		return nil
	}
	var axes []string
	if milestone != "" {
		axes = append(axes, AxisMilestone)
	}
	if area != "" {
		axes = append(axes, AxisArea)
	}
	return axes
}

// ValidateAxes returns an *AxisError for the first axis RefusedAxes reports. Only
// a milestone refuses any: it takes neither a milestone nor an area.
func ValidateAxes(nibType, milestone, area string) error {
	axes := RefusedAxes(nibType, milestone, area)
	if len(axes) == 0 {
		return nil
	}
	return &AxisError{NibType: nibType, Axis: axes[0]}
}

// ValidChildTypes returns the nib types that can be children of the given parent type.
// If parentType is empty (no parent), all types are valid.
func ValidChildTypes(parentType string) []string {
	allTypes := allTypeNames()
	if parentType == "" {
		return allTypes
	}
	var valid []string
	for _, childType := range allTypes {
		for _, allowedParent := range ValidParentTypes(childType) {
			if allowedParent == parentType {
				valid = append(valid, childType)
				break
			}
		}
	}
	return valid
}

// ValidParentTypesForChildren returns the nib types that can be a parent of all the given child types.
// If childTypes is empty, all types are valid.
func ValidParentTypesForChildren(childTypes []string) []string {
	allTypes := allTypeNames()
	if len(childTypes) == 0 {
		return allTypes
	}
	var result []string
	for _, candidate := range allTypes {
		validForAll := true
		for _, childType := range childTypes {
			allowed := ValidParentTypes(childType)
			found := false
			for _, a := range allowed {
				if a == candidate {
					found = true
					break
				}
			}
			if !found {
				validForAll = false
				break
			}
		}
		if validForAll {
			result = append(result, candidate)
		}
	}
	return result
}
