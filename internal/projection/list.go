package projection

import (
	"encoding/json"

	"github.com/alphaleonis/nibs/internal/nib"
)

// ProjectedList is nibs projected one by one through a single Selection, for list
// output: a {nibs,count,truncated} JSON envelope or a menu-ordered TSV grid.
type ProjectedList struct {
	nibs      []*Projected
	truncated bool
	// closed nibs the open-status default hid; set by the caller (SetHiddenClosed)
	hiddenClosed int
}

// ProjectList projects each nib with Project. A positive limit keeps only the
// first limit nibs, and Truncated reports whether any were dropped. The first
// projection error is returned.
func ProjectList(nibs []*nib.Nib, sel Selection, r Resolver, limit int) (*ProjectedList, error) {
	truncated := false
	if limit > 0 && len(nibs) > limit {
		nibs = nibs[:limit]
		truncated = true
	}
	projected := make([]*Projected, 0, len(nibs))
	for _, n := range nibs {
		p, err := Project(n, sel, r)
		if err != nil {
			return nil, err
		}
		projected = append(projected, p)
	}
	return &ProjectedList{nibs: projected, truncated: truncated}, nil
}

// Count returns the number of projected nibs, after any limit.
func (pl *ProjectedList) Count() int { return len(pl.nibs) }

// Truncated reports whether a limit dropped elements from the input.
func (pl *ProjectedList) Truncated() bool { return pl.truncated }

// SetHiddenClosed records how many closed nibs the open-status default hid. The
// JSON envelope omits hidden_closed when it is 0.
func (pl *ProjectedList) SetHiddenClosed(n int) { pl.hiddenClosed = n }

// HiddenClosed returns the suppressed closed-nib count (0 when none).
func (pl *ProjectedList) HiddenClosed() int { return pl.hiddenClosed }

// Nibs returns a copy of the projected elements in input order (each with its
// fields in canonical menu order).
func (pl *ProjectedList) Nibs() []*Projected {
	out := make([]*Projected, len(pl.nibs))
	copy(out, pl.nibs)
	return out
}

// Rows returns one row per projected nib, each cell rendered by TextValue.
func (pl *ProjectedList) Rows() [][]string {
	rows := make([][]string, len(pl.nibs))
	for i, p := range pl.nibs {
		fields := p.Fields()
		row := make([]string, len(fields))
		for j, f := range fields {
			row[j] = TextValue(f.Value)
		}
		rows[i] = row
	}
	return rows
}

// MarshalJSON serializes the list envelope in a fixed key order:
//
//	{"nibs":[ <projected>, … ], "count": <n>, "truncated": <bool>, "hidden_closed": <n>}
//
// An empty list writes "nibs":[], and hidden_closed is omitted when 0.
func (pl *ProjectedList) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Nibs         []*Projected `json:"nibs"`
		Count        int          `json:"count"`
		Truncated    bool         `json:"truncated"`
		HiddenClosed int          `json:"hidden_closed,omitempty"`
	}{
		Nibs:         pl.nibs,
		Count:        len(pl.nibs),
		Truncated:    pl.truncated,
		HiddenClosed: pl.hiddenClosed,
	})
}

// Count returns len(nibs), before any limit: the number -c prints.
func Count(nibs []*nib.Nib) int { return len(nibs) }
