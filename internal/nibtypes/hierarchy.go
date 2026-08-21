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

// HierarchyError describes an illegal parent-type relationship. It carries the
// child and attempted-parent types plus the set of parent types that WOULD be
// legal for the child, so callers (e.g. the CLI's HIERARCHY error) can surface
// the allowed set structurally instead of re-deriving it or scraping the
// message. Allowed is empty when the child type cannot have a parent at all.
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

// ValidateParentType checks whether childType can have parentType as a parent.
// Returns nil if valid, or a *HierarchyError describing the constraint
// violation (and the allowed parent types).
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

// ValidParentTypes returns the valid parent types for a given nib type.
// Returns nil if the nib type cannot have a parent.
//
// Milestones sit outside the parent graph entirely: they are waypoints, not
// containers, so they neither take a parent nor appear as one — work reaches a
// milestone through the `milestone:` assignment axis instead. Epics top the
// work tree.
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

// CanHaveParent reports whether the nib type may take a parent at all. Both
// milestone and epic are root-only — the milestone as a waypoint outside the
// parent graph, the epic as the top of the work tree — so "no valid parents"
// must not be read as "is a milestone".
func CanHaveParent(nibType string) bool {
	return ValidParentTypes(nibType) != nil
}

// ValidateAxes checks the assignment axes against the nib's type. A milestone
// is a waypoint, not work: it takes neither a milestone assignment nor an
// area. Every other type (unknown ones included) takes both.
func ValidateAxes(nibType, milestone, area string) error {
	if nibType != "milestone" {
		return nil
	}
	if milestone != "" {
		return fmt.Errorf("a milestone cannot be assigned to a milestone")
	}
	if area != "" {
		return fmt.Errorf("a milestone cannot have an area")
	}
	return nil
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
	// A candidate type is valid only if it appears in ValidParentTypes for every child type
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
