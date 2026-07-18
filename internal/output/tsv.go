package output

import (
	"fmt"
	"strings"
)

// FormatTSV renders a pre-stringified grid as tab-separated values: cells in a
// row are joined by '\t', and each row is terminated by '\n' (so N rows yield N
// newlines; an empty grid yields ""). It is the byte-level TSV primitive shared
// by the column projection (FormatColumns) and the list projection's default
// output (FormatListTSV) — the single place the tab/newline convention lives so
// the two renderers cannot drift apart.
func FormatTSV(rows [][]string) string {
	var sb strings.Builder
	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				sb.WriteByte('\t')
			}
			sb.WriteString(cell)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// FormatListTSV renders a projected list grid as the default list output: an
// optional "# <n> nibs" comment header (n = number of rows, "# 0 nibs" when the
// grid is empty) followed by the tab-separated rows (see FormatTSV). Passing
// includeHeader=false drops the comment line — the --no-header form.
//
// When hiddenClosed > 0 the header is annotated to disclose that the open
// default silently suppressed that many completed/scrapped rows, e.g.
// "# 8 nibs (43 hidden: completed/scrapped — --all to include)". hiddenLabel
// names the suppressed statuses ("completed/scrapped"). The caller passes
// hiddenClosed = 0 whenever the annotation does not apply (an explicit status
// selection, --all, --ready, or nothing hidden), which suppresses the note.
//
// The grid is supplied by the caller (produced by
// projection.ProjectedList.Rows) so this stays free of any projection or
// transport dependency: it is pure string assembly, reused by list/rel/recipe
// views.
func FormatListTSV(rows [][]string, includeHeader bool, hiddenClosed int, hiddenLabel string) string {
	body := FormatTSV(rows)
	if !includeHeader {
		return body
	}
	header := fmt.Sprintf("# %d nibs", len(rows))
	if hiddenClosed > 0 {
		header += fmt.Sprintf(" (%d hidden: %s — --all to include)", hiddenClosed, hiddenLabel)
	}
	return header + "\n" + body
}
