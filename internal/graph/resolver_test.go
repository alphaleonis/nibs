package graph

import (
	"strings"
	"testing"
)

func TestCheckMutualExclusion(t *testing.T) {
	strPtr := func(s string) *string { return &s }
	var nilStrPtr *string
	var nilSlice []string

	tests := []struct {
		name      string
		fieldName string
		replace   any
		deltas    []any
		wantErr   bool
		wantMsg   string // substring expected in error message
	}{
		{
			name:      "error when replace and delta both set",
			fieldName: "body",
			replace:   strPtr("x"),
			deltas:    []any{strPtr("y")},
			wantErr:   true,
			wantMsg:   "body",
		},
		{
			name:      "no error when only deltas set",
			fieldName: "tags",
			replace:   nil,
			deltas:    []any{strPtr("add"), strPtr("remove")},
			wantErr:   false,
		},
		{
			name:      "no error when only replace set",
			fieldName: "tags",
			replace:   strPtr("replace"),
			deltas:    []any{nil, nil},
			wantErr:   false,
		},
		{
			name:      "no error when nothing set",
			fieldName: "docs",
			replace:   nil,
			deltas:    []any{nil, nil},
			wantErr:   false,
		},
		{
			name:      "error when replace set and one of multiple deltas set",
			fieldName: "tags",
			replace:   strPtr("x"),
			deltas:    []any{nil, strPtr("remove")},
			wantErr:   true,
			wantMsg:   "tags",
		},
		{
			name:      "typed nil pointer is treated as unset",
			fieldName: "body",
			replace:   strPtr("x"),
			deltas:    []any{nilStrPtr},
			wantErr:   false,
		},
		{
			name:      "non-nil slice counts as set",
			fieldName: "tags",
			replace:   []string{"a"},
			deltas:    []any{[]string{"b"}},
			wantErr:   true,
			wantMsg:   "tags",
		},
		{
			name:      "typed nil slice is treated as unset",
			fieldName: "tags",
			replace:   strPtr("replace"),
			deltas:    []any{nilSlice},
			wantErr:   false,
		},
		{
			name:      "empty non-nil slice counts as set",
			fieldName: "tags",
			replace:   strPtr("x"),
			deltas:    []any{[]string{}},
			wantErr:   true,
			wantMsg:   "tags",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkMutualExclusion(tc.fieldName, tc.replace, tc.deltas...)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
					t.Errorf("error should contain %q, got: %v", tc.wantMsg, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}
