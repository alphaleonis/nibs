package nib

import (
	"strings"
	"testing"
)

func TestReplaceOnce(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		old     string
		new     string
		want    string
		wantErr string
	}{
		{
			name: "simple replacement",
			text: "hello world",
			old:  "world",
			new:  "there",
			want: "hello there",
		},
		{
			name: "replace checkbox unchecked to checked",
			text: "## Tasks\n- [ ] Task 1\n- [ ] Task 2",
			old:  "- [ ] Task 1",
			new:  "- [x] Task 1",
			want: "## Tasks\n- [x] Task 1\n- [ ] Task 2",
		},
		{
			name: "delete text with empty new",
			text: "hello world",
			old:  " world",
			new:  "",
			want: "hello",
		},
		{
			name: "replace at start",
			text: "hello world",
			old:  "hello",
			new:  "hi",
			want: "hi world",
		},
		{
			name: "replace at end",
			text: "hello world",
			old:  "world",
			new:  "universe",
			want: "hello universe",
		},
		{
			name: "replace entire string",
			text: "hello",
			old:  "hello",
			new:  "goodbye",
			want: "goodbye",
		},
		{
			name: "replace with longer text",
			text: "a",
			old:  "a",
			new:  "abc",
			want: "abc",
		},
		{
			name: "replace multiline",
			text: "line1\nline2\nline3",
			old:  "line2",
			new:  "replaced",
			want: "line1\nreplaced\nline3",
		},
		{
			name:    "empty old string",
			text:    "hello",
			old:     "",
			new:     "world",
			wantErr: "old text cannot be empty",
		},
		{
			name:    "text not found",
			text:    "hello world",
			old:     "foo",
			new:     "bar",
			wantErr: "text not found in body",
		},
		{
			name:    "text found multiple times",
			text:    "hello hello",
			old:     "hello",
			new:     "hi",
			wantErr: "text found 2 times in body (must be unique)",
		},
		{
			name:    "text found three times",
			text:    "aaa",
			old:     "a",
			new:     "b",
			wantErr: "text found 3 times in body (must be unique)",
		},
		{
			name:    "empty text with non-empty old",
			text:    "",
			old:     "hello",
			new:     "world",
			wantErr: "text not found in body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReplaceOnce(tt.text, tt.old, tt.new)
			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("ReplaceOnce() error = nil, wantErr %q", tt.wantErr)
					return
				}
				if err.Error() != tt.wantErr {
					t.Errorf("ReplaceOnce() error = %q, wantErr %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("ReplaceOnce() unexpected error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("ReplaceOnce() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyBodyMod(t *testing.T) {
	t.Run("no replacements and no append returns body unchanged", func(t *testing.T) {
		body := "original body"
		got, err := ApplyBodyMod(body, nil, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != body {
			t.Errorf("got %q, want %q", got, body)
		}
	})

	t.Run("single replacement", func(t *testing.T) {
		body := "- [ ] Task 1\n- [ ] Task 2"
		got, err := ApplyBodyMod(body, []BodyReplacement{{Old: "- [ ] Task 1", New: "- [x] Task 1"}}, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "- [x] Task 1\n- [ ] Task 2"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("multiple sequential replacements", func(t *testing.T) {
		body := "- [ ] Task 1\n- [ ] Task 2\n- [ ] Task 3"
		replacements := []BodyReplacement{
			{Old: "- [ ] Task 1", New: "- [x] Task 1"},
			{Old: "- [ ] Task 3", New: "- [x] Task 3"},
		}
		got, err := ApplyBodyMod(body, replacements, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "- [x] Task 1\n- [ ] Task 2\n- [x] Task 3"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("append only no replacements", func(t *testing.T) {
		body := "## Tasks\n- [ ] Task 1"
		got, err := ApplyBodyMod(body, nil, "## Notes\n\nSome notes")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "## Tasks\n- [ ] Task 1\n\n## Notes\n\nSome notes"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("replacements and append combined", func(t *testing.T) {
		body := "- [ ] Task 1\n- [ ] Task 2"
		replacements := []BodyReplacement{
			{Old: "- [ ] Task 1", New: "- [x] Task 1"},
			{Old: "- [ ] Task 2", New: "- [x] Task 2"},
		}
		got, err := ApplyBodyMod(body, replacements, "## Summary\n\nAll done!")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "- [x] Task 1\n- [x] Task 2\n\n## Summary\n\nAll done!"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("replacement failure returns original body", func(t *testing.T) {
		body := "- [ ] Task 1\n- [ ] Task 2"
		replacements := []BodyReplacement{
			{Old: "nonexistent text", New: "whatever"},
		}
		got, err := ApplyBodyMod(body, replacements, "appended")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "replacement 0 failed") {
			t.Errorf("error %q should mention replacement index", err.Error())
		}
		if got != body {
			t.Errorf("got %q, want original body %q", got, body)
		}
	})

	t.Run("later replacement fails after earlier ones succeed returns original body", func(t *testing.T) {
		body := "- [ ] Task 1\n- [ ] Task 2"
		replacements := []BodyReplacement{
			{Old: "- [ ] Task 1", New: "- [x] Task 1"},
			{Old: "nonexistent", New: "whatever"},
		}
		got, err := ApplyBodyMod(body, replacements, "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "replacement 1 failed") {
			t.Errorf("error %q should mention replacement index 1", err.Error())
		}
		// Must return the ORIGINAL body, not partially modified
		if got != body {
			t.Errorf("got %q, want original body %q", got, body)
		}
	})

	t.Run("empty body with replacement fails", func(t *testing.T) {
		got, err := ApplyBodyMod("", []BodyReplacement{{Old: "x", New: "y"}}, "")
		if err == nil {
			t.Fatal("expected error for replacement on empty body")
		}
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})

	t.Run("empty body with append only", func(t *testing.T) {
		got, err := ApplyBodyMod("", nil, "new content")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "new content" {
			t.Errorf("got %q, want %q", got, "new content")
		}
	})
}

func TestAppendWithSeparator(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		addition string
		want     string
	}{
		{
			name:     "append to non-empty text",
			text:     "hello",
			addition: "world",
			want:     "hello\n\nworld",
		},
		{
			name:     "append to empty text",
			text:     "",
			addition: "world",
			want:     "world",
		},
		{
			name:     "append empty to non-empty text (no-op)",
			text:     "hello",
			addition: "",
			want:     "hello",
		},
		{
			name:     "append empty to empty text (no-op)",
			text:     "",
			addition: "",
			want:     "",
		},
		{
			name:     "text with trailing newline",
			text:     "hello\n",
			addition: "world",
			want:     "hello\n\nworld",
		},
		{
			name:     "text with multiple trailing newlines",
			text:     "hello\n\n\n",
			addition: "world",
			want:     "hello\n\nworld",
		},
		{
			name:     "multiline text",
			text:     "line1\nline2",
			addition: "line3",
			want:     "line1\nline2\n\nline3",
		},
		{
			name:     "multiline addition",
			text:     "header",
			addition: "line1\nline2",
			want:     "header\n\nline1\nline2",
		},
		{
			name:     "typical usage - adding notes section",
			text:     "## Tasks\n- [ ] Task 1",
			addition: "## Notes\n\nSome notes here",
			want:     "## Tasks\n- [ ] Task 1\n\n## Notes\n\nSome notes here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AppendWithSeparator(tt.text, tt.addition)
			if got != tt.want {
				t.Errorf("AppendWithSeparator() = %q, want %q", got, tt.want)
			}
		})
	}
}
