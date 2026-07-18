package projection

import (
	"encoding/json"

	"github.com/alphaleonis/nibs/internal/nib"
)

// ProjectedList is the projection of a slice of nibs through one Selection: the
// list-output analogue of Projected. It is the shared shape every list-producing
// view (list, rel, recipes) emits — a {nibs,count,truncated} JSON envelope for
// --json and a menu-ordered string grid for the default TSV output.
//
// It is built by applying the single-nib Project to each element with the same
// Selection + Resolver, so a list stays byte-for-byte consistent with a single
// get of the same field set. The single-read {nib} contract (one id) is a
// different, wrapper-free shape and is unaffected by this type.
type ProjectedList struct {
	nibs      []*Projected
	truncated bool
	// hiddenClosed is the number of completed/scrapped nibs the open-by-default
	// status filter silently removed (matching every other active filter). It is
	// disclosed in the JSON envelope as "hidden_closed" and omitted when 0. It is
	// set by the caller (SetHiddenClosed) after projection because it depends on a
	// widened, pre-limit re-query the projection layer does not perform.
	hiddenClosed int
}

// ProjectList projects each nib through the same Selection + Resolver via the
// single-nib Project, applying an optional limit. A limit <= 0 means unlimited;
// when a positive limit is smaller than len(nibs) only the first limit elements
// are projected and Truncated reports true. The input slice is never mutated (a
// local copy of the slice header is resliced). A per-element projection error
// (e.g. a computed field with a nil Resolver) is returned rather than swallowed.
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

// Count returns the number of projected nibs (post-limit) — the envelope's
// "count". This is the size of the rendered list, not the pre-limit input size;
// for the bare -c count use the Count package function.
func (pl *ProjectedList) Count() int { return len(pl.nibs) }

// Truncated reports whether a limit dropped elements from the input.
func (pl *ProjectedList) Truncated() bool { return pl.truncated }

// SetHiddenClosed records how many completed/scrapped nibs the open-by-default
// filter suppressed, for disclosure in the JSON envelope. A value <= 0 means
// "not applicable" and is omitted from the envelope.
func (pl *ProjectedList) SetHiddenClosed(n int) { pl.hiddenClosed = n }

// HiddenClosed returns the suppressed completed/scrapped count (0 when none).
func (pl *ProjectedList) HiddenClosed() int { return pl.hiddenClosed }

// Nibs returns a copy of the projected elements in input order (each with its
// fields in canonical menu order).
func (pl *ProjectedList) Nibs() []*Projected {
	out := make([]*Projected, len(pl.nibs))
	copy(out, pl.nibs)
	return out
}

// Rows returns the TSV grid: one []string per projected nib, each cell the
// menu-ordered leaf rendered via TextValue (multi-value fields comma-joined,
// missing values ""). Every row has the same columns in the same order because
// all elements share one Selection. The byte-level assembly (header + tabs +
// newlines) is output.FormatListTSV's job; this stays transport-agnostic.
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
// Each element reuses Projected.MarshalJSON (a flat, menu-ordered object). An
// empty list marshals to "nibs":[] (never null) so a consumer can index it
// unconditionally. hidden_closed is omitted when 0 (not applicable) so its
// presence signals that the open default suppressed completed/scrapped rows.
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

// Count returns the number of nibs, the value the bare -c/count path emits. It
// counts the raw input independent of any limit or projection — unlike
// ProjectedList.Count, which is the post-limit size of a rendered list.
func Count(nibs []*nib.Nib) int { return len(nibs) }
