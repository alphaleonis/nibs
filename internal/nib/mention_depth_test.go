package nib

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// Covers the container-depth bound in ExtractMentionSpans: a deeply nested line
// parses in linear time and keeps its mentions at their original offsets.

// Each body is about 240 KB, which took 15–30 s to parse without the bound.
func TestExtractMentionSpansDeepNestingIsFast(t *testing.T) {
	for name, body := range map[string]string{
		"bullets":         strings.Repeat("- ", 120000) + "x",
		"quotes":          strings.Repeat(">", 240000) + " x",
		"spaced quotes":   strings.Repeat("> ", 120000) + "x",
		"ordered":         strings.Repeat("1. ", 80000) + "x",
		"quoted bullets":  strings.Repeat("> - ", 60000) + "x",
		"tabbed bullets":  strings.Repeat("-\t", 120000) + "x",
		"thematic breaks": strings.Repeat("- ", 120000),
	} {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			ExtractMentionSpans(body)
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Fatalf("parse took %v; want well under 5s", elapsed)
			}
		})
	}
}

func TestExtractMentionSpansPastTheDepthBound(t *testing.T) {
	for _, tc := range []struct {
		name, prefix string
	}{
		{"bullets", strings.Repeat("- ", maxContainerDepth+8)},
		{"quotes", strings.Repeat(">", maxContainerDepth+8)},
		{"ordered", strings.Repeat("12) ", maxContainerDepth+8)},
		{"one past the bound", strings.Repeat("- ", maxContainerDepth+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.prefix + "#tnib-a and [[tnib-b]]\nnext #tnib-c\n"
			got := ExtractMentionSpans(body)
			want := []MentionSpan{
				spanAt(body, "#tnib-a", "tnib-a", MentionHash),
				spanAt(body, "[[tnib-b]]", "tnib-b", MentionBracket),
				spanAt(body, "#tnib-c", "tnib-c", MentionHash),
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("spans = %+v, want %+v", got, want)
			}
		})
	}
}

// A long spaced thematic break stays a break, so the indented line below it is
// code rather than a lazy continuation of a nested paragraph.
func TestExtractMentionSpansLongThematicBreakKeepsItsMeaning(t *testing.T) {
	for _, marker := range []string{"- ", "* "} {
		body := strings.Repeat(marker, maxContainerDepth+8) + "\n    #tnib-a\n"
		if got := ExtractMentionSpans(body); len(got) != 0 {
			t.Errorf("%q break: spans = %+v, want none", marker, got)
		}
	}
}

func spanAt(body, spelled, token string, form MentionForm) MentionSpan {
	start := strings.Index(body, spelled)
	return MentionSpan{Token: token, Start: start, Stop: start + len(spelled), Form: form}
}
