package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
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
	// Reason is a one-line snippet of why the nib left the board, taken from the
	// most recent entry `close` wrote into its "## Summary". It is filled for
	// every closed status, not just scrapped: this warning exists to stop
	// someone recreating work already considered, and "we set this aside" is as
	// relevant as "we rejected this". Empty when the nib carries no such record.
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

// closeReasonSnippet extracts a one-line snippet of why a nib left the board.
//
// It reads the LAST entry `close` wrote into "## Summary", because a nib can be
// closed more than once to revise its reason and the newest entry is the one
// that still holds. Entries look like "**Scrapped 2026-08-02** — superseded by
// nibs-abcd"; the stamp is dropped and the text kept. A body whose Summary is
// hand-written prose rather than close's entries yields "" from this pass.
//
// It then falls back to a "## Reasons for Scrapping" section, the convention
// that predated the `close --as` ritual. There is no data migration in this
// project — existing nib files stay valid — so nibs scrapped the old way must
// still explain themselves here.
func closeReasonSnippet(body string) string {
	if reason := latestCloseEntry(body); reason != "" {
		return reason
	}
	return legacyScrapReason(body)
}

// latestCloseEntry returns the text of the last "**Reason YYYY-MM-DD** — …"
// entry in the body's Summary section, or "" when there is none.
func latestCloseEntry(body string) string {
	content, found := mdsection.Find(body, closeSummaryHeading, mdsection.AnyLevel)
	if !found {
		return ""
	}
	var latest string
	for _, line := range strings.Split(content, "\n") {
		if text, ok := parseCloseEntry(strings.TrimSpace(line)); ok {
			latest = text
		}
	}
	if latest == "" {
		return ""
	}
	return truncateSnippet(latest, 160)
}

// parseCloseEntry splits a "**Reason YYYY-MM-DD** — text" entry into its text,
// reporting whether the line had that shape at all.
func parseCloseEntry(line string) (string, bool) {
	if !strings.HasPrefix(line, "**") {
		return "", false
	}
	end := strings.Index(line[2:], "**")
	if end < 0 {
		return "", false
	}
	stamp := line[2 : 2+end]
	// The stamp is "<Reason> <date>"; require the date so ordinary bold text at
	// the start of a hand-written summary is not mistaken for a close entry.
	sp := strings.LastIndex(stamp, " ")
	if sp < 0 {
		return "", false
	}
	if _, err := time.Parse(closeEntryDateFormat, stamp[sp+1:]); err != nil {
		return "", false
	}
	text := strings.TrimSpace(line[2+end+2:])
	text = strings.TrimPrefix(text, "—")
	text = strings.TrimPrefix(text, "-")
	return strings.TrimSpace(text), true
}

// legacyScrapReason reads the pre-`close --as` convention: the first non-empty
// line of a "## Reasons for Scrapping" section, stripped of a list marker.
func legacyScrapReason(body string) string {
	content, found := mdsection.Find(body, "Reasons for Scrapping", mdsection.AnyLevel)
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

// findPossibleDuplicates scans the full nib set for CLOSED nibs — whatever
// cfg.IsClosedStatus covers, so a deferred nib counts as much as a completed or
// scrapped one — that likely duplicate a just-created nib. The new nib itself is
// excluded by id, and open nibs are not returned (the day-to-day list already
// surfaces those). A match is an equal/contained
// normalized title or an exact non-empty slug equality. Results are sorted by id
// for deterministic output.
func findPossibleDuplicates(candidates []*nib.Nib, cfg *config.Config, newID, newTitle, newSlug string) []possibleDuplicate {
	var out []possibleDuplicate
	for _, c := range candidates {
		if c == nil || c.ID == newID {
			continue
		}
		if !cfg.IsClosedStatus(c.Status) {
			continue
		}
		slugMatch := newSlug != "" && c.Slug == newSlug
		if !slugMatch && !titlesMatch(newTitle, c.Title) {
			continue
		}
		pd := possibleDuplicate{ID: c.ID, Status: c.Status, Title: c.Title}
		pd.Reason = closeReasonSnippet(c.Body)
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
