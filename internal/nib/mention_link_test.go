package nib

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// Covers the link destination scan budget in ExtractMentionSpans: a run of
// unclosed links parses in linear time, and ordinary links stay links.

// Each body is about 240 KB, which took 2–7 s to parse without the budget.
func TestExtractMentionSpansUnclosedLinksAreFast(t *testing.T) {
	const size = 240000
	for name, unit := range map[string]string{
		"links":         "[a](",
		"images":        "![a](",
		"angle":         "[a](<",
		"nested parens": "[a]((",
		"destinations":  "[a](x",
	} {
		t.Run(name, func(t *testing.T) {
			body := strings.Repeat(unit, size/len(unit))
			start := time.Now()
			ExtractMentionSpans(body)
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Fatalf("parse took %v; want well under 1s", elapsed)
			}
		})
	}
}

// The unclosed links before the paragraph spend the whole budget, so the
// real link after them is read as text.
func TestExtractMentionSpansReadsLinksPastTheScanBudgetAsText(t *testing.T) {
	body := strings.Repeat("[a](", 64) + "\n\n[x](#tnib-a) and [[tnib-b]]\n"
	got := ExtractMentionSpans(body)
	want := []MentionSpan{
		spanAt(body, "#tnib-a", "tnib-a", MentionHash),
		spanAt(body, "[[tnib-b]]", "tnib-b", MentionBracket),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("spans = %+v, want %+v", got, want)
	}
}

func TestExtractMentionSpansKeepsLinksWithinTheScanBudget(t *testing.T) {
	for name, links := range map[string]string{
		"adjacent links":   strings.Repeat("[x](#tnib-a)", 2000),
		"spaced links":     strings.Repeat("[x](#tnib-a) ", 2000),
		"angle links":      strings.Repeat("[x](<#tnib-a b>)", 2000),
		"titled links":     strings.Repeat(`[x](#tnib-a "t")`, 2000),
		"nested images":    strings.Repeat("[![x](#tnib-a)](#tnib-a)", 1000),
		"long destination": "![x](data:" + strings.Repeat("A", 200000) + "/#tnib-a)",
		"wrapped links":    strings.Repeat("[x](\n#tnib-a)\n", 2000),
	} {
		t.Run(name, func(t *testing.T) {
			body := links + "\n\n#tnib-b\n"
			want := []MentionSpan{spanAt(body, "#tnib-b", "tnib-b", MentionHash)}
			if got := ExtractMentionSpans(body); !reflect.DeepEqual(got, want) {
				t.Errorf("spans = %+v, want %+v", got, want)
			}
		})
	}
}
