package output

import (
	"fmt"
	"strings"
	"time"

	"github.com/alphaleonis/nibs/internal/nib"
)

// Column is a selectable nib field for tabular output.
type Column string

// Typed Column constants. Using these in the renderField switch turns a
// typo into a compile error (vs bare string literals which fail silently).
// Add a new column ↔ add a constant ↔ append to AvailableColumns ↔ extend
// renderField. The exhaustiveness test in columns_test.go verifies every
// entry in AvailableColumns has a renderField case.
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

// AvailableColumns lists the supported column names in canonical order
// (for --help text and error messages).
var AvailableColumns = []Column{
	ColumnID, ColumnSlug, ColumnTitle, ColumnStatus, ColumnType,
	ColumnPriority, ColumnEstimate, ColumnOrder, ColumnParent,
	ColumnTags, ColumnCreatedAt, ColumnUpdatedAt,
}

// availableSet is a fast-lookup set of valid column names, derived from
// AvailableColumns at init time.
var availableSet = func() map[Column]struct{} {
	m := make(map[Column]struct{}, len(AvailableColumns))
	for _, c := range AvailableColumns {
		m[c] = struct{}{}
	}
	return m
}()

// ParseColumns parses a comma-separated spec ("id,status,title") into a
// validated column list. Whitespace around names is trimmed; empty entries
// (including a fully empty spec) are rejected. Unknown column names error
// with the available set listed. Duplicate entries are rejected so agent
// consumers that split lines on tab can rely on a stable column index per
// name (e.g. "id,id,title" would otherwise place "title" at index 2 — the
// silent-bug class the comment in TestFormatColumns_EmptyFields wards off).
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

// FormatColumns renders one row per nib, fields tab-joined, rows joined by
// '\n' with a single trailing '\n'.
//
// Multi-value fields (tags) are joined internally with ',' (commas don't
// collide with the tab separator). Empty fields render as the empty string.
// time.Time fields render as RFC3339, or "" when nil.
func FormatColumns(nibs []*nib.Nib, columns []Column) string {
	var sb strings.Builder
	for _, n := range nibs {
		for i, c := range columns {
			if i > 0 {
				sb.WriteByte('\t')
			}
			sb.WriteString(renderField(n, c))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// renderField extracts the string representation of a single column from a
// nib. Time fields use time.RFC3339 — sort-friendly and matches existing
// JSON output (internal/nib encodes timestamps via the standard json
// package, also RFC3339).
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
		return n.Type
	case ColumnPriority:
		return n.Priority
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

// AvailableColumnsString returns AvailableColumns as a comma-separated string,
// suitable for embedding in CLI --help text and error messages. Single source
// of truth so the help text and error envelopes can never drift apart.
func AvailableColumnsString() string {
	names := make([]string, 0, len(AvailableColumns))
	for _, c := range AvailableColumns {
		names = append(names, string(c))
	}
	return strings.Join(names, ", ")
}

// availableNames is the internal alias used in error messages. Kept as a
// thin wrapper so the historical name appears at error-message sites.
func availableNames() string { return AvailableColumnsString() }
