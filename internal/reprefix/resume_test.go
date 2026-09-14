package reprefix

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// existsIn reports a path as existing when it is one of paths: the store as a
// failed run left it on disk.
func existsIn(paths ...string) TargetExistsFunc {
	return func(rel string) bool { return slices.Contains(paths, rel) }
}

func TestBuildPlan_Resumes(t *testing.T) {
	tests := []struct {
		name     string
		snapshot []NibSnapshot
		onDisk   []string
		want     []FilePlan
	}{
		{
			name: "half renamed",
			snapshot: []NibSnapshot{
				{ID: "new-aaa", Path: "new-aaa--root.md"},
				{ID: "tnib-bbb", Path: "archive/tnib-bbb--child.md", Parent: "tnib-aaa"},
			},
			onDisk: []string{"new-aaa--root.md", "archive/tnib-bbb--child.md"},
			want: []FilePlan{
				{OldPath: "new-aaa--root.md", NewPath: "new-aaa--root.md", OldID: "tnib-aaa", NewID: "new-aaa"},
				{OldPath: "archive/tnib-bbb--child.md", NewPath: "archive/new-bbb--child.md", OldID: "tnib-bbb", NewID: "new-bbb", OldParent: "tnib-aaa", NewParent: "new-aaa"},
			},
		},
		{
			name: "fully renamed, one file already rewritten",
			snapshot: []NibSnapshot{
				{ID: "new-aaa", Path: "new-aaa--root.md"},
				{ID: "new-bbb", Path: "new-bbb--child.md", Parent: "new-aaa"},
				{ID: "new-ccc", Path: "new-ccc.md", Parent: "tnib-aaa", Milestone: "tnib-aaa", BlockedBy: []string{"tnib-bbb"}, Blocking: []string{"new-bbb"}},
			},
			onDisk: []string{"new-aaa--root.md", "new-bbb--child.md", "new-ccc.md"},
			want: []FilePlan{
				{OldPath: "new-aaa--root.md", NewPath: "new-aaa--root.md", OldID: "tnib-aaa", NewID: "new-aaa"},
				{OldPath: "new-bbb--child.md", NewPath: "new-bbb--child.md", OldID: "tnib-bbb", NewID: "new-bbb", OldParent: "new-aaa", NewParent: "new-aaa"},
				{
					OldPath: "new-ccc.md", NewPath: "new-ccc.md", OldID: "tnib-ccc", NewID: "new-ccc",
					OldParent: "tnib-aaa", NewParent: "new-aaa",
					OldMilestone: "tnib-aaa", NewMilestone: "new-aaa",
					OldBlockedBy: []string{"tnib-bbb"}, NewBlockedBy: []string{"new-bbb"},
					OldBlocking: []string{"new-bbb"}, NewBlocking: []string{"new-bbb"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := BuildPlan(tt.snapshot, "tnib-", "new-", existsIn(tt.onDisk...))
			if err != nil {
				t.Fatalf("BuildPlan: %v", err)
			}
			if len(plan.Collisions) != 0 {
				t.Errorf("Collisions = %v, want none", plan.Collisions)
			}
			if len(plan.Files) != len(tt.want) {
				t.Fatalf("got %d file plans, want %d", len(plan.Files), len(tt.want))
			}
			for i, want := range tt.want {
				got := plan.Files[i]
				if got.OldPath != want.OldPath || got.NewPath != want.NewPath || got.OldID != want.OldID || got.NewID != want.NewID ||
					got.OldParent != want.OldParent || got.NewParent != want.NewParent ||
					got.OldMilestone != want.OldMilestone || got.NewMilestone != want.NewMilestone ||
					!slices.Equal(got.OldBlockedBy, want.OldBlockedBy) || !slices.Equal(got.NewBlockedBy, want.NewBlockedBy) ||
					!slices.Equal(got.OldBlocking, want.OldBlocking) || !slices.Equal(got.NewBlocking, want.NewBlocking) {
					t.Errorf("Files[%d] = %+v\nwant %+v", i, got, want)
				}
			}
		})
	}
}

// A renamed row's own path is on disk and is not a collision, but another row
// renaming onto it still is.
func TestBuildPlan_ResumedPathStillCollidesWithAnotherRow(t *testing.T) {
	for _, order := range []string{"renamed first", "renamed last"} {
		t.Run(order, func(t *testing.T) {
			snapshot := []NibSnapshot{
				{ID: "new-aaa", Path: "new-aaa--x.md"},
				{ID: "tnib-aaa", Path: "tnib-aaa--x.md"},
			}
			if order == "renamed last" {
				slices.Reverse(snapshot)
			}
			plan, err := BuildPlan(snapshot, "tnib-", "new-", existsIn("new-aaa--x.md", "tnib-aaa--x.md"))
			if err != nil {
				t.Fatalf("BuildPlan: %v", err)
			}
			if !slices.Equal(plan.Collisions, []string{"new-aaa--x.md"}) {
				t.Errorf("Collisions = %v, want [new-aaa--x.md]", plan.Collisions)
			}
		})
	}
}

func TestBuildPlan_ResumeStillRefusesAForeignPrefix(t *testing.T) {
	snapshot := []NibSnapshot{
		{ID: "new-aaa", Path: "new-aaa.md"},
		{ID: "zz-bbb", Path: "zz-bbb.md"},
	}
	_, err := BuildPlan(snapshot, "tnib-", "new-", stubExists)
	if err == nil || !strings.Contains(err.Error(), "zz-bbb") {
		t.Fatalf("BuildPlan err = %v, want a refusal naming zz-bbb", err)
	}
}

func TestBuildPlan_RefusesOverlappingPrefixResume(t *testing.T) {
	tests := []struct {
		name           string
		oldPrefix      string
		newPrefix      string
		resumed        []NibSnapshot
		fresh          []NibSnapshot
		freshNewPrefix []string
	}{
		{
			name:      "new prefix extends the old",
			oldPrefix: "a-", newPrefix: "a-b-",
			resumed: []NibSnapshot{{ID: "a-b-xyz", Path: "a-b-xyz.md"}, {ID: "a-qrs", Path: "a-qrs.md"}},
			fresh:   []NibSnapshot{{ID: "a-xyz", Path: "a-xyz.md"}, {ID: "a-qrs", Path: "a-qrs.md"}},
		},
		{
			name:      "old prefix extends the new",
			oldPrefix: "a-b-", newPrefix: "a-",
			resumed: []NibSnapshot{{ID: "a-xyz", Path: "a-xyz.md"}, {ID: "a-b-qrs", Path: "a-b-qrs.md"}},
			fresh:   []NibSnapshot{{ID: "a-b-xyz", Path: "a-b-xyz.md"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildPlan(tt.resumed, tt.oldPrefix, tt.newPrefix, stubExists)
			var overlap *OverlappingPrefixResumeError
			if !errors.As(err, &overlap) {
				t.Fatalf("BuildPlan err = %v, want an OverlappingPrefixResumeError", err)
			}
			plan, err := BuildPlan(tt.fresh, tt.oldPrefix, tt.newPrefix, stubExists)
			if err != nil {
				t.Fatalf("an untouched store no longer plans: %v", err)
			}
			for _, fp := range plan.Files {
				if fp.OldPath == fp.NewPath {
					t.Errorf("%s planned no rename in an untouched store", fp.OldPath)
				}
			}
		})
	}
}

// TestExecute_ResumesAHalfRenamedStore runs a plan whose first row a failed run
// already renamed and rewrote and whose second it only renamed.
func TestExecute_ResumesAHalfRenamedStore(t *testing.T) {
	root := t.TempDir()
	newTestNib(t, root, "new-aaa--root.md", "new-aaa", "", nil, "see #new-bbb")
	newTestNib(t, root, "new-bbb--child.md", "tnib-bbb", "tnib-aaa", nil, "see #tnib-aaa")
	newTestNib(t, root, "tnib-ccc--leaf.md", "tnib-ccc", "tnib-bbb", nil, "")

	snapshot := []NibSnapshot{
		{ID: "new-aaa", Path: "new-aaa--root.md"},
		{ID: "new-bbb", Path: "new-bbb--child.md", Parent: "tnib-aaa"},
		{ID: "tnib-ccc", Path: "tnib-ccc--leaf.md", Parent: "tnib-bbb"},
	}
	plan, err := BuildPlan(snapshot, "tnib-", "new-", existsIn("new-aaa--root.md", "new-bbb--child.md", "tnib-ccc--leaf.md"))
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if err := Execute(plan, root); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for path, want := range map[string]struct{ id, parent, body string }{
		"new-aaa--root.md":  {"new-aaa", "", "see #new-bbb"},
		"new-bbb--child.md": {"new-bbb", "new-aaa", "see #new-aaa"},
		"new-ccc--leaf.md":  {"new-ccc", "new-bbb", ""},
	} {
		// Parse derives no id, so the rendered id line is read from the bytes.
		raw, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "# "+want.id+"\n") || strings.Contains(string(raw), "# tnib-") {
			t.Errorf("%s does not render id %q alone:\n%s", path, want.id, raw)
		}
		b := readNib(t, root, path)
		if b.Parent != want.parent || strings.TrimSpace(b.Body) != want.body {
			t.Errorf("%s: parent=%q body=%q, want %+v", path, b.Parent, b.Body, want)
		}
	}
}
