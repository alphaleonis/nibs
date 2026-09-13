package output

import (
	"fmt"
	"strings"
)

// FormatTSV joins each row's cells with '\t' and ends every row with '\n'; an
// empty grid yields "".
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

// FormatListTSV renders list output: an optional "# <n> nibs" header, noting
// hiddenClosed rows of the hiddenLabel statuses when hiddenClosed > 0, then the
// rows. Pass 0 when no open-status default hid anything.
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
