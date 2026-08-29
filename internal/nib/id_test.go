package nib

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestIsIDChar(t *testing.T) {
	// Every byte in the generator alphabet must be accepted: this is the
	// equivalence pin that keeps derived charset gates (e.g. nibcore's
	// isIDFragment) aligned with idAlphabet as the single source of truth.
	for i := 0; i < len(idAlphabet); i++ {
		c := idAlphabet[i]
		if !IsIDChar(c) {
			t.Errorf("IsIDChar(%q) = false, want true (byte is in idAlphabet)", c)
		}
	}

	rejects := []struct {
		name string
		c    byte
	}{
		{"uppercase A", 'A'},
		{"uppercase Z", 'Z'},
		{"dash", '-'},
		{"space", ' '},
		{"dot", '.'},
		{"underscore", '_'},
		{"slash", '/'},
		{"digit boundary below zero", '/'}, // '/' is '0'-1
		{"alpha boundary below a", '`'},    // '`' is 'a'-1
		{"alpha boundary above z", '{'},    // '{' is 'z'+1
		{"high-bit non-ASCII", 0xE9},       // 'é' in latin-1
		{"NUL", 0x00},
	}
	for _, tt := range rejects {
		t.Run(tt.name, func(t *testing.T) {
			if IsIDChar(tt.c) {
				t.Errorf("IsIDChar(%q / 0x%02x) = true, want false", tt.c, tt.c)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"basic spaces", "Hello World", "hello-world"},
		{"underscores", "hello_world", "hello-world"},
		{"special chars", "Hello! World?", "hello-world"},
		{"multiple spaces", "hello   world", "hello-world"},
		{"multiple dashes", "hello---world", "hello-world"},
		{"leading trailing dashes", "--hello--", "hello"},
		{"empty string", "", ""},
		{"numbers", "Test 123", "test-123"},
		{"mixed special chars", "Hello, World! How's it going?", "hello-world-hows-it-going"},
		{"only special chars", "!@#$%^&*()", ""},
		{"unicode letters", "Café Résumé", "café-résumé"},
		{"already lowercase", "already-slugified", "already-slugified"},
		{"all caps", "ALL CAPS", "all-caps"},
		{"spaces and underscores mixed", "hello world_test", "hello-world-test"},
		{
			"truncation at 50 chars",
			"this is a very long title that should be truncated to fifty characters",
			"this-is-a-very-long-title-that-should-be-truncated",
		},
		{
			"truncation removes trailing dash",
			"this is a very long title that should be truncated-at dash",
			"this-is-a-very-long-title-that-should-be-truncated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Slugify(tt.input)
			if got != tt.expected {
				t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseFilename(t *testing.T) {
	tests := []struct {
		name         string
		filename     string
		prefix       string
		expectedID   string
		expectedSlug string
	}{
		// New format with double-dash (prefix-agnostic: the double-dash check runs
		// first, so the same result holds with or without a configured prefix).
		{"new format basic", "abc--my-slug.md", "", "abc", "my-slug"},
		{"new format with prefix", "nibs-z5r9--add-unit-tests.md", "", "nibs-z5r9", "add-unit-tests"},
		{"new format long slug", "xyz--this-is-a-longer-slug.md", "", "xyz", "this-is-a-longer-slug"},

		// Dot format (also prefix-agnostic: the dot check runs before the prefix branch).
		{"dot format basic", "abc.my-slug.md", "", "abc", "my-slug"},
		{"dot format with prefix", "nibs-z5r9.add-unit-tests.md", "", "nibs-z5r9", "add-unit-tests"},

		// Legacy format with single dash (prefix-less: guards the SplitN fallback).
		{"legacy format basic", "abc-my-slug.md", "", "abc", "my-slug"},
		{"legacy format multi-part slug", "abc-my-multi-part-slug.md", "", "abc", "my-multi-part-slug"},

		// ID only
		{"id only with md", "abc.md", "", "abc", ""},
		{"id only no extension", "abc", "", "abc", ""},

		// Prefix-less legacy interpretation: with no configured prefix, a slugless
		// prefixed-looking id is still split on its first dash (unchanged behavior).
		{"id only prefixed, no configured prefix", "nibs-z5r9.md", "", "nibs", "z5r9"},

		// Prefix-aware slugless id (nibs-mccz): with the prefix configured, a slugless
		// prefixed id keeps its full id and yields an empty slug — the split the legacy
		// fallback got wrong.
		{"slugless prefixed id", "nibs-x9z2.md", "nibs-", "nibs-x9z2", ""},
		{"slugless prefixed id, tnib", "tnib-a1b2.md", "tnib-", "tnib-a1b2", ""},

		// Prefixed id with a double-dash slug (double-dash branch still wins first).
		{"prefixed id double-dash slug", "nibs-x9z2--my-slug.md", "nibs-", "nibs-x9z2", "my-slug"},

		// Prefixed id with a dot slug: the dot branch runs BEFORE the prefix branch,
		// so it wins even with the prefix configured — guards the order-dependent
		// precedence in ParseFilename (a reorder would misparse this as slugless).
		{"prefixed id dot slug", "nibs-x9z2.my-slug.md", "nibs-", "nibs-x9z2", "my-slug"},

		// Prefixed id with a legacy single-dash slug: the dash after the prefix
		// separates a slug, so id keeps the prefix and slug is the remainder.
		{"prefixed id single-dash slug", "nibs-x9z2-my-slug.md", "nibs-", "nibs-x9z2", "my-slug"},

		// Prefix configured but name doesn't match it: falls back to legacy split.
		{"prefix set but no match", "other-thing.md", "nibs-", "other", "thing"},

		// Edge cases
		{"empty string", "", "", "", ""},
		{"just md extension", ".md", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotSlug := ParseFilename(tt.filename, tt.prefix)
			if gotID != tt.expectedID || gotSlug != tt.expectedSlug {
				t.Errorf("ParseFilename(%q, %q) = (%q, %q), want (%q, %q)",
					tt.filename, tt.prefix, gotID, gotSlug, tt.expectedID, tt.expectedSlug)
			}
		})
	}
}

func TestBuildFilename(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		slug     string
		expected string
	}{
		{"with slug", "abc", "my-slug", "abc--my-slug.md"},
		{"empty slug", "abc", "", "abc.md"},
		{"with prefix id", "nibs-z5r9", "add-tests", "nibs-z5r9--add-tests.md"},
		{"long slug", "xyz", "this-is-a-longer-slug", "xyz--this-is-a-longer-slug.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildFilename(tt.id, tt.slug)
			if got != tt.expected {
				t.Errorf("BuildFilename(%q, %q) = %q, want %q",
					tt.id, tt.slug, got, tt.expected)
			}
		})
	}
}

func TestNewID(t *testing.T) {
	t.Run("length without prefix", func(t *testing.T) {
		id := NewID("", 4)
		if len(id) != 4 {
			t.Errorf("NewID(\"\", 4) length = %d, want 4", len(id))
		}
	})

	t.Run("length with prefix", func(t *testing.T) {
		id := NewID("nibs-", 4)
		if len(id) != 9 { // "nibs-" (5) + 4
			t.Errorf("NewID(\"nibs-\", 4) length = %d, want 9", len(id))
		}
	})

	t.Run("prefix preserved", func(t *testing.T) {
		prefix := "myapp-"
		id := NewID(prefix, 4)
		if !strings.HasPrefix(id, prefix) {
			t.Errorf("NewID(%q, 4) = %q, should start with prefix", prefix, id)
		}
	})

	t.Run("uses valid alphabet", func(t *testing.T) {
		id := NewID("", 100) // generate long ID to test alphabet
		for _, r := range id {
			if !strings.ContainsRune(idAlphabet, r) {
				t.Errorf("NewID contains invalid character %q, should only use %q", r, idAlphabet)
			}
		}
	})

	t.Run("generates unique IDs", func(t *testing.T) {
		seen := make(map[string]bool)
		for i := 0; i < 100; i++ {
			id := NewID("", 8)
			if seen[id] {
				t.Errorf("NewID generated duplicate: %q", id)
			}
			seen[id] = true
		}
	})
}

func TestParseFilenameAndBuildFilenameRoundtrip(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		slug   string
		prefix string
	}{
		{"basic", "abc", "my-slug", ""},
		{"with prefix", "nibs-z5r9", "add-tests", "nibs-"},
		{"no slug", "xyz", "", ""},
		// Slugless prefixed id: BuildFilename emits {id}.md and ParseFilename must
		// recover the full prefixed id with an empty slug (nibs-mccz).
		{"slugless prefixed id", "nibs-z5r9", "", "nibs-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filename := BuildFilename(tt.id, tt.slug)
			gotID, gotSlug := ParseFilename(filename, tt.prefix)
			if gotID != tt.id || gotSlug != tt.slug {
				t.Errorf("Roundtrip failed: BuildFilename(%q, %q) = %q, ParseFilename(_, %q) = (%q, %q)",
					tt.id, tt.slug, filename, tt.prefix, gotID, gotSlug)
			}
		})
	}
}

func TestValidateIDForFilename(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"plain short id", "z5r9", false},
		{"prefixed id", "nibs-z5r9", false},
		{"uppercase prefix", "PX-tkn1", false},
		{"underscores and dots inside a segment", "__no_milestone__.v2-z5r9", false},
		{"leading dots but not a whole segment", "..z5r9", false},
		{"forward slash", "a/b-z5r9", true},
		{"leading forward slash", "/__no_milestone__z5r9", true},
		{"parent traversal", "../../z5r9", true},
		{"backslash", `a\b-z5r9`, true},
		{"windows traversal", `..\..\z5r9`, true},
		{"bare dot", ".", true},
		{"bare parent", "..", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIDForFilename(tt.id)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidateIDForFilename(%q) = nil, want an error", tt.id)
				}
				if !errors.Is(err, ErrIDNotFilename) {
					t.Errorf("ValidateIDForFilename(%q) error = %v, want one wrapping ErrIDNotFilename", tt.id, err)
				}
				if !strings.Contains(err.Error(), fmt.Sprintf("%q", tt.id)) {
					t.Errorf("ValidateIDForFilename(%q) error = %q, should quote the offending id", tt.id, err)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateIDForFilename(%q) = %v, want nil", tt.id, err)
			}
		})
	}
}
