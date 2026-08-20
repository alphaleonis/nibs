package config

// Role classifies a status by what it means for the work's lifecycle. It is
// the single axis behind three questions that used to be three independent
// flags — "is this nib finished", "does this blocker still count" and "can
// this nib be picked up" — and it adds a fourth the flags could not ask:
// whether closed work counts as done (RoleDone) or left the scope entirely
// (RoleDropped). Progress arithmetic keys on that distinction instead of on
// the literal status names.
//
// Every role IS one legal combination of the three derived predicates, so the
// illegal flag states (an open status releasing its dependents, a startable
// closed status) cannot be expressed. RoleDone and RoleDropped share a
// predicate row — both are closed and both release — and differ only in the
// done/dropped question, which is exactly why the roles exist.
type Role uint8

const (
	// RoleOpen is work on the board that cannot be picked up: it is either
	// already underway (in-progress) or not yet refined (draft).
	RoleOpen Role = iota
	// RoleStartable is open work ready to be picked up (todo).
	RoleStartable
	// RoleParked is closed work that is coming back (deferred): off the board,
	// still blocking its dependents, still counted as outstanding scope.
	RoleParked
	// RoleDone is closed work that was accomplished (completed): it releases
	// its dependents and is the numerator of progress arithmetic.
	RoleDone
	// RoleDropped is closed work that will never happen (scrapped): it
	// releases its dependents and leaves the scope — out of the denominator.
	RoleDropped
)

// String returns the role's name for test failures and diagnostics.
func (r Role) String() string {
	switch r {
	case RoleOpen:
		return "open"
	case RoleStartable:
		return "startable"
	case RoleParked:
		return "parked"
	case RoleDone:
		return "done"
	case RoleDropped:
		return "dropped"
	}
	return "unknown"
}

// Closed reports whether the role is terminal — the work is no longer on the
// board, whatever the reason.
func (r Role) Closed() bool {
	return r == RoleParked || r == RoleDone || r == RoleDropped
}

// ReleasesDependents reports whether closing a blocker in this role satisfies
// the dependency. Parked work does not: it is coming back, so its dependents
// stay blocked.
func (r Role) ReleasesDependents() bool {
	return r == RoleDone || r == RoleDropped
}

// Startable reports whether work can be picked up from this role. It is the
// status half of "can I start this?"; the other half is having no active
// blockers.
func (r Role) Startable() bool {
	return r == RoleStartable
}

// StatusRole returns the role of a declared status, and false for a name
// outside the vocabulary — including "", which a hand-edited nib with no
// `status:` carries. Callers decide what an unknown status means for them;
// the conservative readings (an unknown status is open, blocks, is not
// startable, and counts as outstanding scope) all follow from treating
// "unknown" as its own answer rather than defaulting to a role.
func StatusRole(name string) (Role, bool) {
	for _, s := range DefaultStatuses {
		if s.Name == name {
			return s.Role, true
		}
	}
	return RoleOpen, false
}
