package reprefix

import (
	"strings"
	"testing"
)

func TestValidatePrefix_Invalid(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
	}{
		{"empty", ""},
		{"single letter no dash", "a"},
		{"dash only", "-"},
		{"uppercase", "NIBS-"},
		{"no trailing dash", "nibs"},
		{"underscore", "nibs_"},
		{"slash", "nibs/"},
		{"too long", "abcdefghijklmnop-"},
		{"leading dash", "-nibs-"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidatePrefix(tc.prefix); err == nil {
				t.Errorf("ValidatePrefix(%q) returned nil, want error", tc.prefix)
			}
		})
	}
}

func TestValidatePrefix_Valid(t *testing.T) {
	// Boundary cases: "a-" is exactly 2 chars (min); "abcdefghijklmno-" is
	// exactly 16 chars (max: 15 letters + trailing dash).
	valid := []string{"a-", "nibs-", "tnib-", "myproj-", "a1-", "abcdefghijklmno-"}
	for _, p := range valid {
		t.Run(p, func(t *testing.T) {
			if err := ValidatePrefix(p); err != nil {
				t.Errorf("ValidatePrefix(%q) returned unexpected error: %v", p, err)
			}
		})
	}
}

// stubExists returns a TargetExistsFunc that always returns false.
func stubExists(string) bool { return false }

func TestFilePlan_HasReferenceUpdates(t *testing.T) {
	cases := []struct {
		name string
		fp   FilePlan
		want bool
	}{
		{
			name: "no refs at all",
			fp:   FilePlan{OldID: "tnib-a", NewID: "new-a"},
			want: false,
		},
		{
			name: "parent rewritten",
			fp: FilePlan{
				OldID:     "tnib-b",
				NewID:     "new-b",
				OldParent: "tnib-a",
				NewParent: "new-a",
			},
			want: true,
		},
		{
			name: "blocked_by rewritten",
			fp: FilePlan{
				OldID:        "tnib-c",
				NewID:        "new-c",
				OldBlockedBy: []string{"tnib-a"},
				NewBlockedBy: []string{"new-a"},
			},
			want: true,
		},
		{
			// Exercises the len(OldBlockedBy) != len(NewBlockedBy) branch,
			// which is defensive against hand-constructed FilePlan values.
			name: "blocked_by length changes",
			fp: FilePlan{
				OldID:        "tnib-d",
				NewID:        "new-d",
				OldBlockedBy: []string{"tnib-a", "tnib-b"},
				NewBlockedBy: []string{"new-a"},
			},
			want: true,
		},
		{
			name: "milestone rewritten",
			fp: FilePlan{
				OldID:        "tnib-e",
				NewID:        "new-e",
				OldMilestone: "tnib-m",
				NewMilestone: "new-m",
			},
			want: true,
		},
		{
			name: "legacy blocking rewritten",
			fp: FilePlan{
				OldID:       "tnib-f",
				NewID:       "new-f",
				OldBlocking: []string{"tnib-a"},
				NewBlocking: []string{"new-a"},
			},
			want: true,
		},
		{
			// Mirrors the blocked_by length case: defensive against
			// hand-constructed FilePlan values.
			name: "legacy blocking length changes",
			fp: FilePlan{
				OldID:       "tnib-g",
				NewID:       "new-g",
				OldBlocking: []string{"tnib-a", "tnib-b"},
				NewBlocking: []string{"new-a"},
			},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fp.HasReferenceUpdates(); got != tc.want {
				t.Errorf("HasReferenceUpdates() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildPlan_CollisionDetectionViaStub(t *testing.T) {
	snapshot := []NibSnapshot{
		{ID: "tnib-aaa", Path: "tnib-aaa--one.md"},
		{ID: "tnib-bbb", Path: "tnib-bbb--two.md"},
		{ID: "tnib-ccc", Path: "tnib-ccc--three.md"},
	}
	// Simulate that the second nib's target path already exists.
	existsFn := func(relPath string) bool {
		return relPath == "new-bbb--two.md"
	}

	plan, err := BuildPlan(snapshot, "tnib-", "new-", existsFn)
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}

	if len(plan.Collisions) != 1 || plan.Collisions[0] != "new-bbb--two.md" {
		t.Errorf("Collisions: got %v, want [new-bbb--two.md]", plan.Collisions)
	}
	// Plan must remain fully populated so --dry-run can show the full picture.
	if len(plan.Files) != 3 {
		t.Errorf("expected 3 file plans even on collision, got %d", len(plan.Files))
	}
}

func TestBuildPlan_SlugAndNoSlugFilenames(t *testing.T) {
	snapshot := []NibSnapshot{
		{ID: "tnib-abc123", Path: "tnib-abc123--slug.md"},
		{ID: "tnib-def456", Path: "tnib-def456.md"},
	}
	plan, err := BuildPlan(snapshot, "tnib-", "new-", stubExists)
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}
	if got, want := plan.Files[0].NewPath, "new-abc123--slug.md"; got != want {
		t.Errorf("slug filename: got %q, want %q", got, want)
	}
	if got, want := plan.Files[1].NewPath, "new-def456.md"; got != want {
		t.Errorf("no-slug filename: got %q, want %q", got, want)
	}
}

func TestBuildPlan_ArchivePathsPreserved(t *testing.T) {
	snapshot := []NibSnapshot{
		{ID: "tnib-aaa", Path: "tnib-aaa--active.md"},
		{ID: "tnib-zzz", Path: "archive/tnib-zzz--done.md"},
	}
	plan, err := BuildPlan(snapshot, "tnib-", "new-", stubExists)
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}
	if plan.Files[0].NewPath != "new-aaa--active.md" {
		t.Errorf("active path: got %q, want %q", plan.Files[0].NewPath, "new-aaa--active.md")
	}
	if plan.Files[1].NewPath != "archive/new-zzz--done.md" {
		t.Errorf("archive path: got %q, want %q", plan.Files[1].NewPath, "archive/new-zzz--done.md")
	}
}

func TestBuildPlan_InconsistentSnapshot(t *testing.T) {
	snapshot := []NibSnapshot{
		{ID: "nibs-aaa", Path: "nibs-aaa.md"},
		{ID: "foo-bbb", Path: "foo-bbb.md"},
	}
	_, err := BuildPlan(snapshot, "nibs-", "new-", stubExists)
	if err == nil {
		t.Fatal("BuildPlan with inconsistent snapshot returned nil, want error")
	}
	if !strings.Contains(err.Error(), "foo-bbb") {
		t.Errorf("error should name the offending id, got: %v", err)
	}
}

func TestBuildPlan_InvalidNewPrefix(t *testing.T) {
	snapshot := []NibSnapshot{{ID: "nibs-aaa", Path: "nibs-aaa.md"}}
	_, err := BuildPlan(snapshot, "nibs-", "BAD_", stubExists)
	if err == nil {
		t.Fatal("BuildPlan with invalid new prefix returned nil, want error")
	}
	if !strings.Contains(err.Error(), "BAD_") {
		t.Errorf("error should mention the invalid prefix, got: %v", err)
	}
}

func TestBuildPlan_EmptyOldPrefixRejected(t *testing.T) {
	// Empty old prefix is the one case we MUST reject: strings.CutPrefix
	// against "" always succeeds, so every ID would get double-prefixed.
	snapshot := []NibSnapshot{{ID: "nibs-aaa", Path: "nibs-aaa.md"}}
	_, err := BuildPlan(snapshot, "", "new-", stubExists)
	if err == nil {
		t.Fatal("BuildPlan(oldPrefix=\"\") returned nil, want error")
	}
	if !strings.Contains(err.Error(), "old prefix") {
		t.Errorf("error should identify the old prefix as the culprit, got: %v", err)
	}
}

func TestBuildPlan_GrandfatheredOldPrefix(t *testing.T) {
	// Old prefixes may not match the strict validation rules applied to
	// NEW prefixes — projects initialized via `nibs init` end up with
	// whatever `{dirname}-` produces, including uppercase, long names, etc.
	// The command's whole purpose is to let users migrate away from such
	// prefixes, so it must accept them as input. The structural correctness
	// check (every nib ID starts with oldPrefix) remains in force.
	cases := []struct {
		name      string
		oldPrefix string
		nibID     string
	}{
		{"uppercase", "NIBS-", "NIBS-aaa"},
		{"long (17 chars)", "boardGameTracker-", "boardGameTracker-aaa"},
		{"no trailing dash", "nibs", "nibsaaa"},
		{"underscore", "my_proj-", "my_proj-aaa"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := []NibSnapshot{{ID: tc.nibID, Path: tc.nibID + ".md"}}
			plan, err := BuildPlan(snapshot, tc.oldPrefix, "new-", stubExists)
			if err != nil {
				t.Fatalf("BuildPlan(oldPrefix=%q) returned error: %v", tc.oldPrefix, err)
			}
			if len(plan.Files) != 1 {
				t.Fatalf("expected 1 file plan, got %d", len(plan.Files))
			}
			if plan.Files[0].OldID != tc.nibID {
				t.Errorf("OldID = %q, want %q", plan.Files[0].OldID, tc.nibID)
			}
		})
	}
}

func TestBuildPlan_EmptySnapshot(t *testing.T) {
	plan, err := BuildPlan(nil, "tnib-", "new-", stubExists)
	if err != nil {
		t.Fatalf("BuildPlan(nil) returned error: %v", err)
	}
	if plan == nil {
		t.Fatal("BuildPlan(nil) returned nil plan")
		return
	}
	if len(plan.Files) != 0 {
		t.Errorf("expected zero file plans, got %d", len(plan.Files))
	}
	if len(plan.Collisions) != 0 {
		t.Errorf("expected zero collisions, got %v", plan.Collisions)
	}
}

func TestBuildPlan_NilTargetExistsRejected(t *testing.T) {
	snapshot := []NibSnapshot{{ID: "tnib-aaa", Path: "tnib-aaa.md"}}
	_, err := BuildPlan(snapshot, "tnib-", "new-", nil)
	if err == nil {
		t.Fatal("BuildPlan with nil targetExists returned nil, want error")
	}
	if !strings.Contains(err.Error(), "targetExists") {
		t.Errorf("error should mention targetExists, got: %v", err)
	}
}

func TestBuildPlan_IntraPlanCollision(t *testing.T) {
	// Two nibs whose NewPath collapse to the same target path.
	// This is contrived — IDs should be unique under a single prefix — but
	// the planner is the last safety net before the executor touches disk.
	// Here both IDs share the same suffix after the prefix is stripped by
	// constructing paths that collide post-rewrite.
	snapshot := []NibSnapshot{
		{ID: "tnib-aaa", Path: "tnib-aaa.md"},
		{ID: "tnib-aaa", Path: "tnib-aaa.md"}, // duplicate row
	}
	plan, err := BuildPlan(snapshot, "tnib-", "new-", stubExists)
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}
	if len(plan.Collisions) != 1 || plan.Collisions[0] != "new-aaa.md" {
		t.Errorf("expected intra-plan collision on new-aaa.md, got %v", plan.Collisions)
	}
}

func TestBuildPlan_IntraPlanCollisionNotDoubleCountedWithOnDisk(t *testing.T) {
	// If a path collides both with an on-disk file AND intra-plan, it
	// should appear exactly once in Collisions.
	snapshot := []NibSnapshot{
		{ID: "tnib-aaa", Path: "tnib-aaa.md"},
		{ID: "tnib-aaa", Path: "tnib-aaa.md"},
	}
	existsFn := func(relPath string) bool {
		return relPath == "new-aaa.md"
	}
	plan, err := BuildPlan(snapshot, "tnib-", "new-", existsFn)
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}
	if len(plan.Collisions) != 1 {
		t.Errorf("expected 1 collision entry, got %d: %v", len(plan.Collisions), plan.Collisions)
	}
}

func TestBuildPlan_BasenameIDMismatch(t *testing.T) {
	// The ID has the right prefix, but the path basename does not start
	// with the ID. Without the invariant check, rewritePath would silently
	// no-op and produce a corrupt plan.
	snapshot := []NibSnapshot{
		{ID: "tnib-abc", Path: "archive/other-file.md"},
	}
	_, err := BuildPlan(snapshot, "tnib-", "new-", stubExists)
	if err == nil {
		t.Fatal("BuildPlan with mismatched basename returned nil, want error")
	}
	if !strings.Contains(err.Error(), "tnib-abc") {
		t.Errorf("error should name the offending id, got: %v", err)
	}
}

func TestBuildPlan_OldBlockedByNotAliased(t *testing.T) {
	blockers := []string{"tnib-aaa", "tnib-bbb"}
	snapshot := []NibSnapshot{
		{ID: "tnib-aaa", Path: "tnib-aaa.md"},
		{ID: "tnib-bbb", Path: "tnib-bbb.md"},
		{ID: "tnib-ccc", Path: "tnib-ccc.md", BlockedBy: blockers},
	}
	plan, err := BuildPlan(snapshot, "tnib-", "new-", stubExists)
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}
	// Mutate the caller's slice after BuildPlan returns.
	blockers[0] = "MUTATED"
	if plan.Files[2].OldBlockedBy[0] != "tnib-aaa" {
		t.Errorf("plan.Files[2].OldBlockedBy was aliased to caller slice: got %q, want %q",
			plan.Files[2].OldBlockedBy[0], "tnib-aaa")
	}
}

func TestBuildPlan_SamePrefixIsError(t *testing.T) {
	snapshot := []NibSnapshot{{ID: "nibs-aaa", Path: "nibs-aaa.md"}}
	_, err := BuildPlan(snapshot, "nibs-", "nibs-", stubExists)
	if err == nil {
		t.Fatal("BuildPlan with identical prefixes returned nil, want error")
	}
	if !strings.Contains(err.Error(), "nibs-") {
		t.Errorf("error message should mention the equal prefix, got: %v", err)
	}
}

func TestBuildPlan_TracerBullet_ThreeNibHierarchy(t *testing.T) {
	snapshot := []NibSnapshot{
		{
			ID:   "tnib-aaa",
			Path: "tnib-aaa--root.md",
		},
		{
			ID:     "tnib-bbb",
			Path:   "tnib-bbb--child.md",
			Parent: "tnib-aaa",
		},
		{
			ID:        "tnib-ccc",
			Path:      "tnib-ccc--blocked.md",
			Parent:    "tnib-aaa",
			BlockedBy: []string{"tnib-bbb"},
		},
	}

	plan, err := BuildPlan(snapshot, "tnib-", "new-", stubExists)
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}
	if plan == nil {
		t.Fatal("BuildPlan returned nil plan")
		return
	}
	if len(plan.Collisions) != 0 {
		t.Errorf("expected no collisions, got %v", plan.Collisions)
	}
	if len(plan.Files) != 3 {
		t.Fatalf("expected 3 file plans, got %d", len(plan.Files))
	}

	// Input order must be preserved.
	want := []FilePlan{
		{
			OldPath: "tnib-aaa--root.md",
			NewPath: "new-aaa--root.md",
			OldID:   "tnib-aaa",
			NewID:   "new-aaa",
		},
		{
			OldPath:   "tnib-bbb--child.md",
			NewPath:   "new-bbb--child.md",
			OldID:     "tnib-bbb",
			NewID:     "new-bbb",
			OldParent: "tnib-aaa",
			NewParent: "new-aaa",
		},
		{
			OldPath:      "tnib-ccc--blocked.md",
			NewPath:      "new-ccc--blocked.md",
			OldID:        "tnib-ccc",
			NewID:        "new-ccc",
			OldParent:    "tnib-aaa",
			NewParent:    "new-aaa",
			OldBlockedBy: []string{"tnib-bbb"},
			NewBlockedBy: []string{"new-bbb"},
		},
	}

	for i, w := range want {
		got := plan.Files[i]
		if got.OldPath != w.OldPath || got.NewPath != w.NewPath {
			t.Errorf("Files[%d] path: got (old=%q new=%q) want (old=%q new=%q)",
				i, got.OldPath, got.NewPath, w.OldPath, w.NewPath)
		}
		if got.OldID != w.OldID || got.NewID != w.NewID {
			t.Errorf("Files[%d] id: got (old=%q new=%q) want (old=%q new=%q)",
				i, got.OldID, got.NewID, w.OldID, w.NewID)
		}
		if got.OldParent != w.OldParent || got.NewParent != w.NewParent {
			t.Errorf("Files[%d] parent: got (old=%q new=%q) want (old=%q new=%q)",
				i, got.OldParent, got.NewParent, w.OldParent, w.NewParent)
		}
		if !stringSlicesEqual(got.OldBlockedBy, w.OldBlockedBy) ||
			!stringSlicesEqual(got.NewBlockedBy, w.NewBlockedBy) {
			t.Errorf("Files[%d] blockedBy: got (old=%v new=%v) want (old=%v new=%v)",
				i, got.OldBlockedBy, got.NewBlockedBy, w.OldBlockedBy, w.NewBlockedBy)
		}
	}
}

func TestBuildPlan_RewritesMilestoneAndBlocking(t *testing.T) {
	snapshot := []NibSnapshot{
		{ID: "tnib-m01", Path: "tnib-m01--release.md"},
		{
			ID:        "tnib-aaa",
			Path:      "tnib-aaa--enqueued.md",
			Milestone: "tnib-m01",
			Blocking:  []string{"tnib-m01"},
		},
	}
	plan, err := BuildPlan(snapshot, "tnib-", "new-", stubExists)
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}
	fp := plan.Files[1]
	if fp.OldMilestone != "tnib-m01" || fp.NewMilestone != "new-m01" {
		t.Errorf("milestone: got (old=%q new=%q) want (old=%q new=%q)",
			fp.OldMilestone, fp.NewMilestone, "tnib-m01", "new-m01")
	}
	if !stringSlicesEqual(fp.OldBlocking, []string{"tnib-m01"}) {
		t.Errorf("OldBlocking = %v, want [tnib-m01]", fp.OldBlocking)
	}
	if !stringSlicesEqual(fp.NewBlocking, []string{"new-m01"}) {
		t.Errorf("NewBlocking = %v, want [new-m01]", fp.NewBlocking)
	}
	if !fp.HasReferenceUpdates() {
		t.Error("HasReferenceUpdates() = false for a nib whose milestone and blocking both moved, want true")
	}
	// The milestone nib itself carries no links, so nothing may be invented for it.
	if plan.Files[0].HasReferenceUpdates() {
		t.Error("HasReferenceUpdates() = true for a link-free nib, want false")
	}
}

func TestBuildPlan_OldBlockingNotAliased(t *testing.T) {
	targets := []string{"tnib-aaa", "tnib-bbb"}
	snapshot := []NibSnapshot{
		{ID: "tnib-aaa", Path: "tnib-aaa.md"},
		{ID: "tnib-bbb", Path: "tnib-bbb.md"},
		{ID: "tnib-ccc", Path: "tnib-ccc.md", Blocking: targets},
	}
	plan, err := BuildPlan(snapshot, "tnib-", "new-", stubExists)
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}
	// Mutate the caller's slice after BuildPlan returns.
	targets[0] = "MUTATED"
	if plan.Files[2].OldBlocking[0] != "tnib-aaa" {
		t.Errorf("plan.Files[2].OldBlocking was aliased to caller slice: got %q, want %q",
			plan.Files[2].OldBlocking[0], "tnib-aaa")
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
