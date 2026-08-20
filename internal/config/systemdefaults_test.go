package config

import "testing"

// TestSystemDefaultsAgreeWithDefault pins the two answers to "what does an
// unset key mean" against each other.
//
// They disagreed, and only for `default_type`: Default() carries "task" while
// applySystemDefaults filled an unset value from DefaultTypes[0], which is
// "milestone" — that list is ordered by hierarchy depth, not by which entry is a
// sensible default. So a store config that omitted the key created milestones.
//
// It stayed hidden because `nibs init` writes `default_type: task` into every
// config it creates, so the fallback only fires for a config written or trimmed
// by hand. Latent rather than absent.
//
// All three system defaults are compared, not just the one that broke: the two
// that agree today agree by coincidence of two literals matching, which is the
// same hazard one step behind.
func TestSystemDefaultsAgreeWithDefault(t *testing.T) {
	var applied Config
	applySystemDefaults(&applied)
	want := Default()

	for _, tc := range []struct {
		key  string
		got  any
		want any
	}{
		{"default_type", applied.Nibs.DefaultType, want.Nibs.DefaultType},
		{"default_status", applied.Nibs.DefaultStatus, want.Nibs.DefaultStatus},
		{"id_length", applied.Nibs.IDLength, want.Nibs.IDLength},
	} {
		if tc.got != tc.want {
			t.Errorf("an unset %s resolves to %v, but Default() carries %v — a config that omits the key behaves differently from one nibs init wrote",
				tc.key, tc.got, tc.want)
		}
	}
}
