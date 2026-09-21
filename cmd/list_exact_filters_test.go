package cmd

import (
	"io"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
)

// listIDs runs `nibs list <args…> --all -q` and returns the ids it listed,
// sorted. --all is deliberate on both axes: neither assignment says anything
// about status, and the open default would otherwise decide half of what these
// rows assert.
func listIDs(t *testing.T, nibsPath string, args ...string) []string {
	t.Helper()
	ids := listIDsOrdered(t, nibsPath, args...)
	sort.Strings(ids)
	return ids
}

// listIDsOrdered is listIDs without the sort, for the assertions whose subject
// is the ORDER the rows come back in rather than the set.
func listIDsOrdered(t *testing.T, nibsPath string, args ...string) []string {
	t.Helper()
	resetListFlags()
	resetRootPersistentFlags()
	out, err := runRootWith(t, append([]string{"--nibs-path", nibsPath, "list"}, append(args, "--all", "-q")...)...)
	if err != nil {
		t.Fatalf("list %v: %v", args, err)
	}
	return strings.Fields(out)
}

// keepOnly returns the members of ids that are in want, in ids' order.
func keepOnly(ids []string, want []string) []string {
	var kept []string
	for _, id := range ids {
		if slices.Contains(want, id) {
			kept = append(kept, id)
		}
	}
	return kept
}

// TestListMilestoneExactIsTheQueueAndMilestoneIsThePlan is the flag surface of
// the two milestone readings, asserted against each other on one store so the
// difference is the subject rather than two independent lists.
//
// --milestone-exact is the queue: exactly the nibs whose own `milestone:`
// resolves to tnib-m001. --milestone is the derived plan, which also holds the
// subtree that rides along with each assignee — tnib-f001 and tnib-t001 sit
// under the assigned epic tnib-e001 and carry no assignment of their own.
func TestListMilestoneExactIsTheQueueAndMilestoneIsThePlan(t *testing.T) {
	nibsPath := setupQueueCLITest(t)

	exact := listIDs(t, nibsPath, "--milestone-exact", "tnib-m001")
	want := []string{"tnib-e001", "tnib-e002", "tnib-e003", "tnib-e004", "tnib-t041"}
	sort.Strings(want)
	if !slices.Equal(exact, want) {
		t.Errorf("--milestone-exact = %v, want the queue %v", exact, want)
	}

	plan := listIDs(t, nibsPath, "--milestone", "tnib-m001")
	for _, id := range want {
		if !slices.Contains(plan, id) {
			t.Errorf("--milestone omits %s, which is directly assigned", id)
		}
	}
	// The inherited members: children of the assigned epic tnib-e001, neither of
	// which carries an assignment. Without them the derived reading collapsed
	// back onto the direct one.
	for _, id := range []string{"tnib-f001", "tnib-t001"} {
		if !slices.Contains(plan, id) {
			t.Errorf("--milestone omits %s, which is under the assigned epic tnib-e001", id)
		}
	}
	// tnib-f020 is the unassigned root whose child tnib-t041 is assigned:
	// membership runs down the chain, never up.
	if slices.Contains(plan, "tnib-f020") {
		t.Error("--milestone lists tnib-f020, the unassigned PARENT of an assigned task")
	}
	if len(plan) <= len(exact) {
		t.Errorf("--milestone listed %d nibs and --milestone-exact %d; the plan must be the wider set", len(plan), len(exact))
	}

	// The short form resolves the same way it does for every other id filter.
	if got := listIDs(t, nibsPath, "--milestone-exact", "m001"); !slices.Equal(got, want) {
		t.Errorf("--milestone-exact m001 = %v, want %v", got, want)
	}
}

// TestListAreaExactSelectsOnlyTheNamedPath is the flag surface of the other
// new reading, against `--area` on the same store: the exact flag holds the
// nibs stored at that path and the closed one also holds what is declared
// beneath it.
func TestListAreaExactSelectsOnlyTheNamedPath(t *testing.T) {
	nibsPath := setupAreaCLITest(t)

	tests := []struct {
		path       string
		wantExact  []string
		wantWithin []string
	}{
		{"web", []string{"tnib-b005"}, []string{"tnib-b005", "tnib-f008"}},
		{"web/dashboard", []string{"tnib-f008"}, []string{"tnib-f008"}},
		{"api", []string{"tnib-b011"}, []string{"tnib-b011", "tnib-f011"}},
		{"api/webhooks", []string{"tnib-f011"}, []string{"tnib-f011"}},
		{"infra", []string{"tnib-t039"}, []string{"tnib-t039"}},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			want := slices.Clone(tt.wantExact)
			sort.Strings(want)
			if got := listIDs(t, nibsPath, "--area-exact", tt.path); !slices.Equal(got, want) {
				t.Errorf("--area-exact %s = %v, want %v", tt.path, got, want)
			}
			within := slices.Clone(tt.wantWithin)
			sort.Strings(within)
			if got := listIDs(t, nibsPath, "--area", tt.path); !slices.Equal(got, within) {
				t.Errorf("--area %s = %v, want %v", tt.path, got, within)
			}
		})
	}
}

// TestListExactFilterFlagRefusals pins the new flags to the refusals their
// siblings already give: an empty value is a validation error (exit 2) rather
// than a listing widened to the whole store, an undeclared area path is
// refused naming the declared set, and --milestone-exact contradicts --backlog
// exactly as --milestone does.
func TestListExactFilterFlagRefusals(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*testing.T) string
		args     []string
		wantExit int
		wantMsg  string
	}{
		{"empty milestone-exact", setupQueueCLITest, []string{"list", "--milestone-exact", ""}, output.ExitValidation, "--milestone-exact was given an empty value"},
		{"empty area-exact", setupAreaCLITest, []string{"list", "--area-exact", ""}, output.ExitValidation, "--area-exact was given an empty value"},
		{"undeclared area-exact", setupAreaCLITest, []string{"list", "--area-exact", "nosuch"}, output.ExitValidation, "must be one of"},
		{"milestone-exact with backlog", setupQueueCLITest, []string{"list", "--milestone-exact", "tnib-m001", "--backlog"}, output.ExitValidation, "--milestone-exact and --backlog are mutually exclusive"},
		{"unknown milestone-exact id", setupQueueCLITest, []string{"list", "--milestone-exact", "nope"}, output.ExitNotFound, "nope"},
		{"non-milestone milestone-exact id", setupQueueCLITest, []string{"list", "--milestone-exact", "tnib-e001"}, output.ExitValidation, `"tnib-e001" has type epic, not milestone`},
		{"non-milestone milestone id", setupQueueCLITest, []string{"list", "--milestone", "tnib-e001"}, output.ExitValidation, `"tnib-e001" has type epic, not milestone`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := tt.setup(t)
			resetListFlags()
			_, err := runRootWith(t, append([]string{"--nibs-path", nibsPath}, tt.args...)...)
			if err == nil {
				t.Fatal("list succeeded, want refusal")
			}
			if code := reportExitError(io.Discard, err); code != tt.wantExit {
				t.Errorf("exit = %d, want %d", code, tt.wantExit)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

// TestListMilestoneOrderDefaults pins WHICH of the two milestone flags
// substitutes queue order, which the set comparisons above discard.
//
// --milestone-exact selects the queue, every member of which carries a
// milestone_order key, so it comes back in that key's order. --milestone
// selects the derived plan, most of which carries no queue key at all, so it
// keeps the order key — sorting it by milestone_order would scatter each
// inherited subtree away from the assignee it hangs under.
//
// The plan is compared against a plain `nibs list` narrowed to the same ids,
// which is what "the order key" means at this surface, and then against the
// same listing under an explicit --sort milestone-order, so the row cannot pass
// by the two orders happening to coincide.
func TestListMilestoneOrderDefaults(t *testing.T) {
	nibsPath := setupQueueCLITest(t)

	queue := listIDsOrdered(t, nibsPath, "--milestone-exact", "tnib-m001")
	wantQueue := []string{"tnib-e001", "tnib-e002", "tnib-e003", "tnib-e004", "tnib-t041"}
	if !slices.Equal(queue, wantQueue) {
		t.Errorf("--milestone-exact = %v, want the queue in queue order %v", queue, wantQueue)
	}

	plan := listIDsOrdered(t, nibsPath, "--milestone", "tnib-m001")
	wantPlan := keepOnly(listIDsOrdered(t, nibsPath), plan)
	if !slices.Equal(plan, wantPlan) {
		t.Errorf("--milestone = %v, want the order key %v", plan, wantPlan)
	}

	queueOrdered := listIDsOrdered(t, nibsPath, "--milestone", "tnib-m001", "--sort", "milestone-order")
	if slices.Equal(plan, queueOrdered) {
		t.Fatal("the plan came back in its queue order: either the default moved onto --milestone or the fixture stopped telling the two orders apart, and the row above proves nothing either way")
	}
}

// TestListMilestoneRefusalsNameTheFlagNotTheField pins the spelling a refusal
// hands back. Every other id-valued flag's GraphQL field is a recognizable
// transform of it (--parent/parentId), but the two milestone readings are
// crossed: --milestone writes inMilestone and --milestone-exact writes
// milestone. Echoing the schema spelling therefore names a REAL adjacent flag
// on the same command with the opposite reading, and this project tells agents
// to stop on an error and act on what it names — so a mistyped id becomes a
// silently ten-times-wider query rather than a repair.
func TestListMilestoneRefusalsNameTheFlagNotTheField(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		want  string
		avoid string
	}{
		{"unknown id under the derived flag",
			[]string{"list", "--milestone", "nope"}, "--milestone filter:", "inMilestone filter:"},
		{"unknown id under the exact flag",
			[]string{"list", "--milestone-exact", "nope"}, "--milestone-exact filter:", "milestone filter:"},
		{"a non-milestone target under the exact flag",
			[]string{"list", "--milestone-exact", "tnib-e001"}, "--milestone-exact filter:", "milestone filter:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := setupQueueCLITest(t)
			resetListFlags()
			_, err := runRootWith(t, append([]string{"--nibs-path", nibsPath}, tt.args...)...)
			if err == nil {
				t.Fatal("list succeeded, want refusal")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to name the flag %q", err.Error(), tt.want)
			}
			// The field spelling this flag does NOT write must not appear at
			// all: it is the other flag's name, so a caller reading it is sent
			// to the wrong half of their own command.
			if strings.Contains(err.Error(), tt.avoid) {
				t.Errorf("error = %q names %q, which is the OTHER milestone reading", err.Error(), tt.avoid)
			}
		})
	}
}

// TestListRefusesTwoMilestoneTargetsAtOnce is the flag surface of the id×id
// refusal. `--milestone X --milestone-exact Y` with X≠Y selects nothing by
// construction — everything the exact half keeps derives to its own target —
// so it is refused rather than answered with an empty listing, which would be a
// factual claim about the store for a question that was never answerable.
//
// The same milestone twice is the queue and stays answerable, in either
// spelling: the rule is the resolved targets, not the presence of both flags.
func TestListRefusesTwoMilestoneTargetsAtOnce(t *testing.T) {
	nibsPath := setupQueueCLITest(t)

	resetListFlags()
	_, err := runRootWith(t, "--nibs-path", nibsPath, "list",
		"--milestone", "tnib-m001", "--milestone-exact", "tnib-m002", "--all")
	if err == nil {
		t.Fatal("two different milestones were answered, want a refusal")
	}
	if code := reportExitError(io.Discard, err); code != output.ExitValidation {
		t.Errorf("exit = %d, want %d", code, output.ExitValidation)
	}
	for _, want := range []string{"--milestone", "--milestone-exact", "tnib-m001", "tnib-m002"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err.Error(), want)
		}
	}

	want := []string{"tnib-e001", "tnib-e002", "tnib-e003", "tnib-e004", "tnib-t041"}
	for _, spelling := range [][]string{
		{"--milestone", "tnib-m001", "--milestone-exact", "tnib-m001"},
		{"--milestone", "m001", "--milestone-exact", "tnib-m001"},
	} {
		if got := listIDs(t, nibsPath, spelling...); !slices.Equal(got, want) {
			t.Errorf("list %v = %v, want the queue %v", spelling, got, want)
		}
	}
}
