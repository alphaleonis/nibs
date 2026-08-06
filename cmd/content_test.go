package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alphaleonis/nibs/internal/input"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
)

func TestApplyTags(t *testing.T) {
	tests := []struct {
		name     string
		initial  []string
		toAdd    []string
		wantTags []string
		wantErr  bool
	}{
		{
			name:     "add single tag",
			initial:  nil,
			toAdd:    []string{"bug"},
			wantTags: []string{"bug"},
		},
		{
			name:     "add multiple tags",
			initial:  nil,
			toAdd:    []string{"bug", "urgent"},
			wantTags: []string{"bug", "urgent"},
		},
		{
			name:     "add to existing tags",
			initial:  []string{"existing"},
			toAdd:    []string{"new"},
			wantTags: []string{"existing", "new"},
		},
		{
			name:     "empty tags list",
			initial:  []string{"existing"},
			toAdd:    []string{},
			wantTags: []string{"existing"},
		},
		{
			name:    "invalid tag with spaces",
			initial: nil,
			toAdd:   []string{"invalid tag"},
			wantErr: true,
		},
		{
			name:     "uppercase tag gets normalized",
			initial:  nil,
			toAdd:    []string{"InvalidTag"},
			wantTags: []string{"invalidtag"}, // normalized to lowercase
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &nib.Nib{Tags: tt.initial}
			err := applyTags(b, tt.toAdd)

			if tt.wantErr {
				if err == nil {
					t.Errorf("applyTags() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("applyTags() unexpected error: %v", err)
				return
			}

			if len(b.Tags) != len(tt.wantTags) {
				t.Errorf("applyTags() tags count = %d, want %d", len(b.Tags), len(tt.wantTags))
				return
			}

			for i, want := range tt.wantTags {
				if b.Tags[i] != want {
					t.Errorf("applyTags() tags[%d] = %q, want %q", i, b.Tags[i], want)
				}
			}
		})
	}
}

func TestFormatCycle(t *testing.T) {
	tests := []struct {
		path []string
		want string
	}{
		{[]string{"a", "b", "c", "a"}, "a → b → c → a"},
		{[]string{"x", "y"}, "x → y"},
		{[]string{"single"}, "single"},
		{[]string{}, ""},
	}

	for _, tt := range tests {
		got := formatCycle(tt.path)
		if got != tt.want {
			t.Errorf("formatCycle(%v) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestResolveBodyFlag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.md")
	content := "# Title\n\nSome **prose**.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	t.Run("@file reads the file verbatim", func(t *testing.T) {
		got, err := resolveBodyFlag("@"+path, "")
		if err != nil {
			t.Fatalf("resolveBodyFlag(@file) unexpected error: %v", err)
		}
		if got != content {
			t.Errorf("resolveBodyFlag(@file) = %q, want %q", got, content)
		}
	})

	t.Run("--body-file reads the file verbatim", func(t *testing.T) {
		got, err := resolveBodyFlag("", path)
		if err != nil {
			t.Fatalf("resolveBodyFlag(--body-file) unexpected error: %v", err)
		}
		if got != content {
			t.Errorf("resolveBodyFlag(--body-file) = %q, want %q", got, content)
		}
	})

	t.Run("inline value is rejected", func(t *testing.T) {
		_, err := resolveBodyFlag("just some inline body", "")
		if !errors.Is(err, input.ErrInlineProse) {
			t.Errorf("resolveBodyFlag(inline) error = %v, want ErrInlineProse", err)
		}
	})

	t.Run("missing @file is an IO error mapped to exit 5", func(t *testing.T) {
		_, err := resolveBodyFlag("@"+filepath.Join(dir, "nope.md"), "")
		if err == nil {
			t.Fatal("resolveBodyFlag(@missing) expected error, got nil")
		}
		mapped := inputError(true, err)
		var ce *output.CodedError
		if !errors.As(mapped, &ce) {
			t.Fatalf("inputError() = %T, want *output.CodedError", mapped)
		}
		if output.ExitCode(ce.Code) != output.ExitIO {
			t.Errorf("inputError() exit = %d, want %d (IO)", output.ExitCode(ce.Code), output.ExitIO)
		}
	})

	t.Run("inline value maps to a validation exit code", func(t *testing.T) {
		_, err := resolveBodyFlag("inline", "")
		mapped := inputError(true, err)
		var ce *output.CodedError
		if !errors.As(mapped, &ce) {
			t.Fatalf("inputError() = %T, want *output.CodedError", mapped)
		}
		if output.ExitCode(ce.Code) != output.ExitValidation {
			t.Errorf("inputError() exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
		}
	})
}

func TestResolveAppendFlag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "append.md")
	// Trailing newlines should be trimmed so appended sections don't accrue
	// blank lines.
	if err := os.WriteFile(path, []byte("appended line\n\n"), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	got, err := resolveAppendFlag("@" + path)
	if err != nil {
		t.Fatalf("resolveAppendFlag(@file) unexpected error: %v", err)
	}
	if got != "appended line" {
		t.Errorf("resolveAppendFlag(@file) = %q, want %q", got, "appended line")
	}

	if _, err := resolveAppendFlag("inline append text"); !errors.Is(err, input.ErrInlineProse) {
		t.Errorf("resolveAppendFlag(inline) error = %v, want ErrInlineProse", err)
	}
}
