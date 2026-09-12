package config

// Role classifies a status by what it means for the work's lifecycle. The
// predicates below are derived from it: no open role releases its dependents,
// and no closed role is startable. None of them distinguishes RoleDone from
// RoleDropped; for that difference see progress.Rollup.
type Role uint8

const (
	// RoleOpen is work on the board that is not ready to start: already
	// underway (in-progress) or not yet refined (draft).
	RoleOpen Role = iota
	// RoleStartable is open work ready to be picked up (todo).
	RoleStartable
	// RoleParked is closed work that is coming back (deferred).
	RoleParked
	// RoleDone is closed work that was accomplished (completed).
	RoleDone
	// RoleDropped is closed work that will never happen (scrapped).
	RoleDropped
)

// String returns the role's name. internal/webvocab generates these names into
// the web's StatusRole union — after renaming one, run `task codegen` and
// update the web's role switches.
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

// Closed reports whether work in this role is off the board.
func (r Role) Closed() bool {
	return r == RoleParked || r == RoleDone || r == RoleDropped
}

// ReleasesDependents reports whether closing a blocker in this role satisfies
// the dependency.
func (r Role) ReleasesDependents() bool {
	return r == RoleDone || r == RoleDropped
}

// Startable reports whether work can be picked up from this role. A nib is
// ready to start only if it also has no active blocker.
func (r Role) Startable() bool {
	return r == RoleStartable
}

// StatusRole returns the role of a declared status. Any other name reports
// false with RoleOpen — including "", which a hand-edited nib with no
// `status:` carries.
func StatusRole(name string) (Role, bool) {
	for _, s := range DefaultStatuses {
		if s.Name == name {
			return s.Role, true
		}
	}
	return RoleOpen, false
}
