package config

import (
	"slices"
	"testing"
)

// TestStatusRoles pins the role each declared status carries. The mapping is
// the whole point of the role model: a consumer keying on a role must get the
// same answer the status name used to give, so a wrong assignment here rewires
// progress arithmetic, the ready queue, and the blocking graph at once.
func TestStatusRoles(t *testing.T) {
	want := map[string]Role{
		"in-progress": RoleOpen,
		"draft":       RoleOpen,
		"todo":        RoleStartable,
		"deferred":    RoleParked,
		"completed":   RoleDone,
		"scrapped":    RoleDropped,
	}
	if len(DefaultStatuses) != len(want) {
		t.Fatalf("DefaultStatuses has %d entries, this test expects %d — a new status must be classified here", len(DefaultStatuses), len(want))
	}
	for _, s := range DefaultStatuses {
		wantRole, ok := want[s.Name]
		if !ok {
			t.Errorf("status %q has no expected role in this test — classify it", s.Name)
			continue
		}
		if s.Role != wantRole {
			t.Errorf("status %q carries role %v, want %v", s.Name, s.Role, wantRole)
		}
	}

	for name, wantRole := range want {
		got, ok := StatusRole(name)
		if !ok {
			t.Errorf("StatusRole(%q) reports the status unknown", name)
			continue
		}
		if got != wantRole {
			t.Errorf("StatusRole(%q) = %v, want %v", name, got, wantRole)
		}
	}
	for _, name := range []string{"", "no-such-status"} {
		if _, ok := StatusRole(name); ok {
			t.Errorf("StatusRole(%q) claims to know a status outside the vocabulary", name)
		}
	}
}

// TestDoneStatusNames pins the done group: the statuses whose close counts as
// an accomplishment, in DefaultStatuses order — `nibs close` derives its
// default reason from the FIRST of them. The second half mutates the
// vocabulary to prove the set follows the roles rather than a kept list.
func TestDoneStatusNames(t *testing.T) {
	cfg := Default()
	if got, want := cfg.DoneStatusNames(), []string{"completed"}; !slices.Equal(got, want) {
		t.Errorf("DoneStatusNames() = %v, want %v", got, want)
	}

	original := DefaultStatuses
	t.Cleanup(func() { DefaultStatuses = original })
	swapped := make([]StatusConfig, len(original))
	copy(swapped, original)
	for i := range swapped {
		if swapped[i].Name == "deferred" {
			swapped[i].Role = RoleDone
		}
	}
	DefaultStatuses = swapped

	// deferred precedes completed in DefaultStatuses order, so it must lead.
	if got, want := cfg.DoneStatusNames(), []string{"deferred", "completed"}; !slices.Equal(got, want) {
		t.Errorf("DoneStatusNames() with deferred done = %v, want %v — the set must follow the roles", got, want)
	}
}

// TestRolePredicates pins the three derived predicates for every role. The
// table is the legality rule made structural: each role IS one of the four
// legal flag combinations, so an illegal combination cannot be expressed.
func TestRolePredicates(t *testing.T) {
	cases := []struct {
		role                        Role
		closed, releases, startable bool
	}{
		{RoleOpen, false, false, false},
		{RoleStartable, false, false, true},
		{RoleParked, true, false, false},
		{RoleDone, true, true, false},
		{RoleDropped, true, true, false},
	}
	for _, c := range cases {
		if got := c.role.Closed(); got != c.closed {
			t.Errorf("%v.Closed() = %v, want %v", c.role, got, c.closed)
		}
		if got := c.role.ReleasesDependents(); got != c.releases {
			t.Errorf("%v.ReleasesDependents() = %v, want %v", c.role, got, c.releases)
		}
		if got := c.role.Startable(); got != c.startable {
			t.Errorf("%v.Startable() = %v, want %v", c.role, got, c.startable)
		}
	}
}
