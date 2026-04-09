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
			name:    "epic can only have milestone parent",
			nibType: "epic",
			want:    map[string]bool{"milestone": true},
		},
		{
			name:    "feature can have milestone or epic parent",
			nibType: "feature",
			want:    map[string]bool{"milestone": true, "epic": true},
		},
		{
			name:    "task can have milestone, epic, or feature parent",
			nibType: "task",
			want:    map[string]bool{"milestone": true, "epic": true, "feature": true},
		},
		{
			name:    "bug can have milestone, epic, or feature parent",
			nibType: "bug",
			want:    map[string]bool{"milestone": true, "epic": true, "feature": true},
		},
		{
			name:    "research can have milestone, epic, or feature parent",
			nibType: "research",
			want:    map[string]bool{"milestone": true, "epic": true, "feature": true},
		},
		{
			name:    "unknown type defaults to milestone, epic, feature",
			nibType: "unknown",
			want:    map[string]bool{"milestone": true, "epic": true, "feature": true},
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

func TestValidChildTypes(t *testing.T) {
	tests := []struct {
		name       string
		parentType string
		want       map[string]bool
	}{
		{
			name:       "milestone children include epic, feature, task, bug, research",
			parentType: "milestone",
			want:       map[string]bool{"epic": true, "feature": true, "task": true, "bug": true, "research": true},
		},
		{
			name:       "epic children include feature, task, bug, research",
			parentType: "epic",
			want:       map[string]bool{"feature": true, "task": true, "bug": true, "research": true},
		},
		{
			name:       "feature children include task, bug, research",
			parentType: "feature",
			want:       map[string]bool{"task": true, "bug": true, "research": true},
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
		childType  string
		parentType string
		wantErr    bool
		errContains string // substring expected in error message
	}{
		// Valid combinations
		{"task", "epic", false, ""},
		{"task", "feature", false, ""},
		{"task", "milestone", false, ""},
		{"bug", "epic", false, ""},
		{"bug", "feature", false, ""},
		{"bug", "milestone", false, ""},
		{"research", "epic", false, ""},
		{"research", "feature", false, ""},
		{"research", "milestone", false, ""},
		{"feature", "epic", false, ""},
		{"feature", "milestone", false, ""},
		{"epic", "milestone", false, ""},

		// Invalid: wrong parent type
		{"task", "task", true, "milestone, epic, or feature"},
		{"task", "bug", true, "milestone, epic, or feature"},
		{"epic", "task", true, "milestone"},
		{"epic", "epic", true, "milestone"},
		{"epic", "feature", true, "milestone"},
		{"feature", "task", true, "milestone or epic"},
		{"feature", "feature", true, "milestone or epic"},

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
			name:       "task children constrain to milestone, epic, feature",
			childTypes: []string{"task"},
			want:       map[string]bool{"milestone": true, "epic": true, "feature": true},
		},
		{
			name:       "feature children constrain to milestone, epic",
			childTypes: []string{"feature"},
			want:       map[string]bool{"milestone": true, "epic": true},
		},
		{
			name:       "epic children constrain to milestone only",
			childTypes: []string{"epic"},
			want:       map[string]bool{"milestone": true},
		},
		{
			name:       "mixed task and feature intersects to milestone, epic",
			childTypes: []string{"task", "feature"},
			want:       map[string]bool{"milestone": true, "epic": true},
		},
		{
			name:       "mixed epic and feature intersects to milestone only",
			childTypes: []string{"epic", "feature"},
			want:       map[string]bool{"milestone": true},
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
