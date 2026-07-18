package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/mdsection"
	"github.com/alphaleonis/nibs/internal/nib"
)

// possibleDuplicate is one closed nib that a freshly-created nib may duplicate.
// It carries only id/status/title and, for a scrapped match, a short one-line
// reason snippet — never the full body — so warnings never leak nib content.
type possibleDuplicate struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Title  string `json:"title"`
	// Reason is a one-line snippet of a scrapped nib's "## Reasons for Scrapping"
	// section (why the team decided against it). Empty for completed matches and
	// for scrapped nibs without that section.
	Reason string `json:"reason,omitempty"`
}

// normalizeTitle lowercases, replaces every non-alphanumeric rune with a space,
// and collapses runs of whitespace to a single space (trimming the ends). This
// is the canonical form both the title-equality and token-containment signals
// compare on.
func normalizeTitle(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// titlesMatch reports whether two titles are likely the same work item: their
// normalized forms are equal, or one's token set is fully contained in the
// other's (so "Add dark mode" matches "Add dark mode toggle"). Blank normalized
// titles never match — a title of only punctuation is not a duplicate signal.
func titlesMatch(a, b string) bool {
	na := normalizeTitle(a)
	nb := normalizeTitle(b)
	if na == "" || nb == "" {
		return false
	}
	if na == nb {
		return true
	}
	ta := strings.Fields(na)
	tb := strings.Fields(nb)
	return tokenSubset(ta, tb) || tokenSubset(tb, ta)
}

// tokenSubset reports whether every token in sub also appears in super. An empty
// sub is not a subset for dedup purposes (it would match everything).
func tokenSubset(sub, super []string) bool {
	if len(sub) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(super))
	for _, t := range super {
		set[t] = struct{}{}
	}
	for _, t := range sub {
		if _, ok := set[t]; !ok {
			return false
		}
	}
	return true
}

// scrapReasonSnippet extracts a one-line snippet from a nib body's
// "## Reasons for Scrapping" section: the first non-empty line, stripped of a
// leading list marker and truncated. Returns "" when the section is absent.
func scrapReasonSnippet(body string) string {
	content, found := mdsection.Find(body, "Reasons for Scrapping")
	if !found {
		return ""
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if len(line) >= 2 && (line[0] == '-' || line[0] == '*' || line[0] == '+') && line[1] == ' ' {
			line = strings.TrimSpace(line[2:])
		}
		if line == "" {
			continue
		}
		return truncateSnippet(line, 160)
	}
	return ""
}

// truncateSnippet caps a snippet at max runes, appending an ellipsis when cut.
func truncateSnippet(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimRight(string(r[:max]), " ") + "…"
}

// findPossibleDuplicates scans the full nib set for CLOSED nibs (completed or
// scrapped, per cfg.IsArchiveStatus) that likely duplicate a just-created nib.
// The new nib itself is excluded by id, and open nibs are never returned (the
// day-to-day list already surfaces those). A match is an equal/contained
// normalized title or an exact non-empty slug equality. Results are sorted by id
// for deterministic output.
func findPossibleDuplicates(candidates []*nib.Nib, cfg *config.Config, newID, newTitle, newSlug string) []possibleDuplicate {
	var out []possibleDuplicate
	for _, c := range candidates {
		if c == nil || c.ID == newID {
			continue
		}
		if !cfg.IsArchiveStatus(c.Status) {
			continue
		}
		slugMatch := newSlug != "" && c.Slug == newSlug
		if !slugMatch && !titlesMatch(newTitle, c.Title) {
			continue
		}
		pd := possibleDuplicate{ID: c.ID, Status: c.Status, Title: c.Title}
		if c.Status == "scrapped" {
			pd.Reason = scrapReasonSnippet(c.Body)
		}
		out = append(out, pd)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// printDuplicateWarning writes the human, warn-only duplicate notice to w (the
// command's stderr). It is a no-op when there are no matches, so callers can
// invoke it unconditionally.
func printDuplicateWarning(w io.Writer, dups []possibleDuplicate) {
	if len(dups) == 0 {
		return
	}
	noun := "duplicate"
	if len(dups) > 1 {
		noun = "duplicates"
	}
	// Writes to stderr; a write error here is not actionable and must not derail
	// the (already successful) create, so the results are explicitly ignored.
	_, _ = fmt.Fprintf(w, "warning: %d possible %s among closed nibs (create still succeeded):\n", len(dups), noun)
	for _, d := range dups {
		if d.Reason != "" {
			_, _ = fmt.Fprintf(w, "  %s [%s] %s — %s\n", d.ID, d.Status, d.Title, d.Reason)
		} else {
			_, _ = fmt.Fprintf(w, "  %s [%s] %s\n", d.ID, d.Status, d.Title)
		}
	}
}
