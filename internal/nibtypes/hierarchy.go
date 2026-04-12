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

// ValidateParentType checks whether childType can have parentType as a parent.
// Returns nil if valid, or an error describing the constraint violation.
func ValidateParentType(childType, parentType string) error {
	allowed := ValidParentTypes(childType)
	if allowed == nil {
		return fmt.Errorf("%s cannot have a parent", childType)
	}
	for _, a := range allowed {
		if a == parentType {
			return nil
		}
	}
	return fmt.Errorf("%s can only have a %s parent, not %s", childType, JoinWithOr(allowed), parentType)
}

// ValidParentTypes returns the valid parent types for a given nib type.
// Returns nil if the nib type cannot have a parent.
func ValidParentTypes(nibType string) []string {
	switch nibType {
	case "milestone":
		return nil // milestones cannot have parents
	case "epic":
		return []string{"milestone"}
	case "feature", "bug":
		return []string{"milestone", "epic"}
	case "task", "research":
		return []string{"milestone", "epic", "feature", "bug"}
	default:
		return []string{"milestone", "epic", "feature", "bug"} // default for unknown types
	}
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
