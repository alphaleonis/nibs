package progress

import (
	"reflect"
	"testing"
)

// TestByCount pins the canonical progress rule directly, independent of
// the store: done = completed only, total excludes scrapped children and no
// others, percent rounds, and the two closed-but-not-completed statuses are
// disclosed by their own counters. The three closed statuses get three
// treatments — completed counts as done, scrapped leaves the denominator,
// deferred stays in it undone — so each is covered separately below.
func TestByCount(t *testing.T) {
	cases := []struct {
		name     string
		statuses []string
		want     Rollup
	}{
		{"no children", nil, Rollup{}},
		{
			"mixed with scrapped excluded",
			[]string{"completed", "scrapped", "todo"},
			Rollup{Total: 2, Done: 1, Percent: 50, Scrapped: 1},
		},
		{
			"all completed",
			[]string{"completed", "completed"},
			Rollup{Total: 2, Done: 2, Percent: 100},
		},
		{
			"rounds to nearest",
			[]string{"completed", "todo", "todo"}, // 1/3 = 33.33 -> 33
			Rollup{Total: 3, Done: 1, Percent: 33},
		},
		{
			"only scrapped -> zero denominator",
			[]string{"scrapped", "scrapped"},
			Rollup{Total: 0, Done: 0, Percent: 0, Scrapped: 2},
		},
		{
			// Deferred is closed, but the work is coming back — it is set aside
			// scope, so it counts toward total and not toward done, exactly like
			// the open statuses. Only scrapped leaves the denominator.
			"draft and deferred both count toward total but not done",
			[]string{"draft", "deferred", "in-progress", "completed"},
			Rollup{Total: 4, Done: 1, Percent: 25, Deferred: 1},
		},
		{
			// A deferred child holds the parent below 100%: there is still work
			// under it, and the roadmap still renders that child. Reporting 100%
			// here would contradict a view that lists the deferred item.
			"a deferred child holds the parent below 100%",
			[]string{"completed", "completed", "completed", "deferred"},
			Rollup{Total: 4, Done: 3, Percent: 75, Deferred: 1},
		},
		{
			"only deferred -> all scope outstanding, 0%",
			[]string{"deferred", "deferred"},
			Rollup{Total: 2, Done: 0, Percent: 0, Deferred: 2},
		},
		{
			// Unknown statuses (a hand-edited nib with no `status:`) are
			// outstanding scope, so they cannot inflate the percentage.
			"unknown status counts toward total but not done",
			[]string{"completed", "", "bogus"},
			Rollup{Total: 3, Done: 1, Percent: 33},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ByCount(tc.statuses); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ByCount(%v) = %#v, want %#v", tc.statuses, got, tc.want)
			}
		})
	}
}
