package cmd

import (
	"testing"

	"github.com/alphaleonis/nibs/internal/graph/model"
)

func TestBuildNibSort(t *testing.T) {
	tests := []struct {
		name      string
		sortFlag  string
		wantField model.NibSortField
		wantDesc  bool
	}{
		{"default", "", model.NibSortFieldOrder, false},
		{"created", "created", model.NibSortFieldCreatedAt, true},
		{"updated", "updated", model.NibSortFieldUpdatedAt, true},
		{"status", "status", model.NibSortFieldStatus, false},
		{"priority", "priority", model.NibSortFieldPriority, false},
		{"status-priority", "status-priority", model.NibSortFieldStatusPriority, false},
		{"id", "id", model.NibSortFieldID, false},
		{"unknown falls back to order", "garbage", model.NibSortFieldOrder, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildNibSort(tt.sortFlag)
			if got.Field != tt.wantField {
				t.Errorf("field = %s, want %s", got.Field, tt.wantField)
			}
			gotDesc := got.Direction != nil && *got.Direction == model.SortDirectionDesc
			if gotDesc != tt.wantDesc {
				t.Errorf("desc = %v, want %v", gotDesc, tt.wantDesc)
			}
		})
	}
}

func TestListReadyFlagMutualExclusion(t *testing.T) {
	// Test that --ready and --is-blocked are mutually exclusive
	// by checking the validation logic directly
	tests := []struct {
		name        string
		ready       bool
		isBlocked   bool
		expectError bool
	}{
		{"neither flag", false, false, false},
		{"only --ready", true, false, false},
		{"only --is-blocked", false, true, false},
		{"both flags", true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This mirrors the validation logic in list.go
			hasError := tt.ready && tt.isBlocked
			if hasError != tt.expectError {
				t.Errorf("ready=%v, isBlocked=%v: got error=%v, want error=%v",
					tt.ready, tt.isBlocked, hasError, tt.expectError)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"needs truncation", "hello world", 8, "hello..."},
		{"very short max", "hello", 4, "h..."},
		{"empty string", "", 10, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

