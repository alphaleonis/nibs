package output

import (
	"fmt"
	"strings"
	"time"

	"github.com/alphaleonis/nibs/internal/nib"
)

// Column is a selectable nib field for tabular output.
type Column string

// To add a column, add a constant, append it to AvailableColumns and extend
// renderField; a test fails for a column renderField does not handle.
const (
	ColumnID        Column = "id"
	ColumnSlug      Column = "slug"
	ColumnTitle     Column = "title"
	ColumnStatus    Column = "status"
	ColumnType      Column = "type"
	ColumnPriority  Column = "priority"
	ColumnEstimate  Column = "estimate"
	ColumnOrder     Column = "order"
	ColumnParent    Column = "parent"
	ColumnTags      Column = "tags"
	ColumnCreatedAt Column = "created_at"
	ColumnUpdatedAt Column = "updated_at"
)

// AvailableColumns lists the supported columns in canonical order.
var AvailableColumns = []Column{
	ColumnID, ColumnSlug, ColumnTitle, ColumnStatus, ColumnType,
	ColumnPriority, ColumnEstimate, ColumnOrder, ColumnParent,
	ColumnTags, ColumnCreatedAt, ColumnUpdatedAt,
}

var availableSet = func() map[Column]struct{} {
	m := make(map[Column]struct{}, len(AvailableColumns))
	for _, c := range AvailableColumns {
		m[c] = struct{}{}
	}
	return m
}()

// ParseColumns parses a comma-separated column list, trimming spaces. It rejects
// an empty spec, empty entries, unknown names and duplicates, so each name has one
// column index.
func ParseColumns(spec string) ([]Column, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, fmt.Errorf("--columns is empty; available columns: %s", availableNames())
	}
	parts := strings.Split(spec, ",")
	out := make([]Column, 0, len(parts))
	seen := make(map[Column]struct{}, len(parts))
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name == "" {
			return nil, fmt.Errorf("--columns has empty entry; available columns: %s", availableNames())
		}
		c := Column(name)
		if _, ok := availableSet[c]; !ok {
			return nil, fmt.Errorf("unknown column %q; available columns: %s", name, availableNames())
		}
		if _, dup := seen[c]; dup {
			return nil, fmt.Errorf("--columns has duplicate entry %q; available columns: %s", name, availableNames())
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out, nil
}

// FormatColumns renders one row per nib through FormatTSV; tags are comma-joined.
func FormatColumns(nibs []*nib.Nib, columns []Column) string {
	rows := make([][]string, len(nibs))
	for i, n := range nibs {
		row := make([]string, len(columns))
		for j, c := range columns {
			row[j] = renderField(n, c)
		}
		rows[i] = row
	}
	return FormatTSV(rows)
}

// renderField returns column c of n; timestamps use time.RFC3339.
func renderField(n *nib.Nib, c Column) string {
	switch c {
	case ColumnID:
		return n.ID
	case ColumnSlug:
		return n.Slug
	case ColumnTitle:
		return n.Title
	case ColumnStatus:
		return n.Status
	case ColumnType:
		return n.EffectiveType()
	case ColumnPriority:
		return n.EffectivePriority()
	case ColumnEstimate:
		return n.Estimate
	case ColumnOrder:
		return n.Order
	case ColumnParent:
		return n.Parent
	case ColumnTags:
		return strings.Join(n.Tags, ",")
	case ColumnCreatedAt:
		if n.CreatedAt == nil {
			return ""
		}
		return n.CreatedAt.Format(time.RFC3339)
	case ColumnUpdatedAt:
		if n.UpdatedAt == nil {
			return ""
		}
		return n.UpdatedAt.Format(time.RFC3339)
	}
	return ""
}

// AvailableColumnsString returns AvailableColumns joined with ", ".
func AvailableColumnsString() string {
	names := make([]string, 0, len(AvailableColumns))
	for _, c := range AvailableColumns {
		names = append(names, string(c))
	}
	return strings.Join(names, ", ")
}

func availableNames() string { return AvailableColumnsString() }
