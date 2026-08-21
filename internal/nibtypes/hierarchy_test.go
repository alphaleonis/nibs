package nibtypes

import (
	"strings"
	"testing"
)

func TestValidParentTypes(t *testing.T) {
	tests := []struct {
		name    string
		nibType string
		want    map[string]bool
		wantNil bool
	}{
		{
			name:    "milestone cannot have parents",
			nibType: "milestone",
			wantNil: true,
		},
		{
			name:    "epic cannot have parents",
			nibType: "epic",
			wantNil: true,
		},
		{
			name:    "feature can only have epic parent",
			nibType: "feature",
			want:    map[string]bool{"epic": true},
		},
		{
			name:    "bug can only have epic parent",
			nibType: "bug",
			want:    map[string]bool{"epic": true},
		},
		{
			name:    "task can have epic, feature, or bug parent",
			nibType: "task",
			want:    map[string]bool{"epic": true, "feature": true, "bug": true},
		},
		{
			name:    "research can have epic, feature, or bug parent",
			nibType: "research",
			want:    map[string]bool{"epic": true, "feature": true, "bug": true},
		},
		{
			name:    "unknown type defaults to epic, feature, bug",
			nibType: "unknown",
			want:    map[string]bool{"epic": true, "feature": true, "bug": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidParentTypes(tt.nibType)
			if tt.wantNil {
				if got != nil {
					t.Errorf("ValidParentTypes(%q) = %v, want nil", tt.nibType, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ValidParentTypes(%q) = nil, want %v", tt.nibType, tt.want)
			}
			gotMap := make(map[string]bool)
			for _, v := range got {
				gotMap[v] = true
			}
			for w := range tt.want {
				if !gotMap[w] {
					t.Errorf("ValidParentTypes(%q) missing %q", tt.nibType, w)
				}
			}
			if len(got) != len(tt.want) {
				t.Errorf("ValidParentTypes(%q) = %v (len %d), want len %d", tt.nibType, got, len(got), len(tt.want))
			}
		})
	}
}

// TestNoTypeParentsToMilestone pins the axis-model rule that milestones sit
// outside the parent graph entirely: no type may name one as a parent.
func TestNoTypeParentsToMilestone(t *testing.T) {
	for _, childType := range allTypeNames() {
		for _, p := range ValidParentTypes(childType) {
			if p == "milestone" {
				t.Errorf("ValidParentTypes(%q) lists milestone as a legal parent", childType)
			}
		}
	}
	if got := ValidChildTypes("milestone"); len(got) != 0 {
		t.Errorf("ValidChildTypes(\"milestone\") = %v, want none", got)
	}
}

func TestValidChildTypes(t *testing.T) {
	tests := []struct {
		name       string
		parentType string
		want       map[string]bool
	}{
		{
			name:       "milestone has no children",
			parentType: "milestone",
			want:       map[string]bool{},
		},
		{
			name:       "epic children include feature, task, bug, research",
			parentType: "epic",
			want:       map[string]bool{"feature": true, "task": true, "bug": true, "research": true},
		},
		{
			name:       "feature children include task, research",
			parentType: "feature",
			want:       map[string]bool{"task": true, "research": true},
		},
		{
			name:       "bug children include task, research",
			parentType: "bug",
			want:       map[string]bool{"task": true, "research": true},
		},
		{
			name:       "no parent means all types valid",
			parentType: "",
			want:       map[string]bool{"milestone": true, "epic": true, "feature": true, "task": true, "bug": true, "research": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidChildTypes(tt.parentType)
			gotMap := make(map[string]bool)
			for _, v := range got {
				gotMap[v] = true
			}
			for w := range tt.want {
				if !gotMap[w] {
					t.Errorf("ValidChildTypes(%q) missing %q", tt.parentType, w)
				}
			}
			if len(got) != len(tt.want) {
				t.Errorf("ValidChildTypes(%q) = %v (len %d), want len %d", tt.parentType, got, len(got), len(tt.want))
			}
		})
	}
}

func TestValidateParentType(t *testing.T) {
	// Build the valid combinations from ValidParentTypes
	allTypes := []string{"milestone", "epic", "feature", "task", "bug", "research"}

	tests := []struct {
		childType   string
		parentType  string
		wantErr     bool
		errContains string // substring expected in error message
	}{
		// Valid combinations
		{"task", "epic", false, ""},
		{"task", "feature", false, ""},
		{"task", "bug", false, ""},
		{"bug", "epic", false, ""},
		{"research", "epic", false, ""},
		{"research", "feature", false, ""},
		{"research", "bug", false, ""},
		{"feature", "epic", false, ""},

		// Invalid: milestone is not a parent for any type
		{"epic", "milestone", true, "cannot have a parent"},
		{"feature", "milestone", true, "epic"},
		{"bug", "milestone", true, "epic"},
		{"task", "milestone", true, "epic, feature, or bug"},
		{"research", "milestone", true, "epic, feature, or bug"},

		// Invalid: wrong parent type
		{"task", "task", true, "epic, feature, or bug"},
		{"task", "research", true, "epic, feature, or bug"},
		{"epic", "task", true, "cannot have a parent"},
		{"epic", "epic", true, "cannot have a parent"},
		{"epic", "feature", true, "cannot have a parent"},
		{"feature", "task", true, "epic"},
		{"feature", "feature", true, "epic"},
		{"feature", "bug", true, "epic"},
		{"bug", "feature", true, "epic"},
		{"bug", "bug", true, "epic"},
		{"bug", "task", true, "epic"},

		// Invalid: milestone can never have a parent
		{"milestone", "epic", true, "cannot have a parent"},
		{"milestone", "milestone", true, "cannot have a parent"},
		{"milestone", "task", true, "cannot have a parent"},
	}

	for _, tt := range tests {
		name := tt.childType + "_child_" + tt.parentType + "_parent"
		t.Run(name, func(t *testing.T) {
			err := ValidateParentType(tt.childType, tt.parentType)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidateParentType(%q, %q) = nil, want error", tt.childType, tt.parentType)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateParentType(%q, %q) = %v, want nil", tt.childType, tt.parentType, err)
				}
			}
		})
	}

	// Verify exhaustive coverage: every type pair not in the explicit table
	// is cross-checked against ValidParentTypes for correctness.
	tested := make(map[string]bool)
	for _, tt := range tests {
		tested[tt.childType+":"+tt.parentType] = true
	}
	for _, child := range allTypes {
		for _, parent := range allTypes {
			key := child + ":" + parent
			if !tested[key] {
				err := ValidateParentType(child, parent)
				allowed := ValidParentTypes(child)
				isValid := false
				for _, a := range allowed {
					if a == parent {
						isValid = true
						break
					}
				}
				if isValid && err != nil {
					t.Errorf("ValidateParentType(%q, %q) = error %v, want nil", child, parent, err)
				}
				if !isValid && err == nil {
					t.Errorf("ValidateParentType(%q, %q) = nil, want error", child, parent)
				}
			}
		}
	}
}

func TestValidParentTypesForChildren(t *testing.T) {
	tests := []struct {
		name       string
		childTypes []string
		want       map[string]bool
	}{
		{
			name:       "no children means all types valid",
			childTypes: nil,
			want:       map[string]bool{"milestone": true, "epic": true, "feature": true, "task": true, "bug": true, "research": true},
		},
		{
			name:       "task children constrain to epic, feature, bug",
			childTypes: []string{"task"},
			want:       map[string]bool{"epic": true, "feature": true, "bug": true},
		},
		{
			name:       "feature children constrain to epic only",
			childTypes: []string{"feature"},
			want:       map[string]bool{"epic": true},
		},
		{
			name:       "epic children leave no valid parent type",
			childTypes: []string{"epic"},
			want:       map[string]bool{},
		},
		{
			name:       "milestone children leave no valid parent type",
			childTypes: []string{"milestone"},
			want:       map[string]bool{},
		},
		{
			name:       "mixed task and feature intersects to epic",
			childTypes: []string{"task", "feature"},
			want:       map[string]bool{"epic": true},
		},
		{
			name:       "mixed epic and feature intersects to nothing",
			childTypes: []string{"epic", "feature"},
			want:       map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidParentTypesForChildren(tt.childTypes)
			gotMap := make(map[string]bool)
			for _, v := range got {
				gotMap[v] = true
			}
			for w := range tt.want {
				if !gotMap[w] {
					t.Errorf("ValidParentTypesForChildren(%v) missing %q", tt.childTypes, w)
				}
			}
			if len(got) != len(tt.want) {
				t.Errorf("ValidParentTypesForChildren(%v) = %v (len %d), want len %d", tt.childTypes, got, len(got), len(tt.want))
			}
		})
	}
}

func TestValidateAxes(t *testing.T) {
	tests := []struct {
		name        string
		nibType     string
		milestone   string
		area        string
		wantErr     bool
		errContains string
	}{
		{name: "milestone with neither axis is fine", nibType: "milestone"},
		{name: "milestone with milestone assignment refused", nibType: "milestone", milestone: "nibs-m1", wantErr: true, errContains: "cannot be assigned to a milestone"},
		{name: "milestone with area refused", nibType: "milestone", area: "web/ui", wantErr: true, errContains: "cannot have an area"},
		{name: "milestone with both axes refused", nibType: "milestone", milestone: "nibs-m1", area: "web/ui", wantErr: true},
		{name: "epic takes both axes", nibType: "epic", milestone: "nibs-m1", area: "web/ui"},
		{name: "task takes both axes", nibType: "task", milestone: "nibs-m1", area: "web/ui"},
		{name: "unknown type takes both axes", nibType: "unknown", milestone: "nibs-m1", area: "web/ui"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAxes(tt.nibType, tt.milestone, tt.area)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidateAxes(%q, %q, %q) = nil, want error", tt.nibType, tt.milestone, tt.area)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateAxes(%q, %q, %q) = %v, want nil", tt.nibType, tt.milestone, tt.area, err)
			}
		})
	}
}
