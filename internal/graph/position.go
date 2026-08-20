package graph

import "fmt"

// This file is the one home of the positioning-flag algebra. Every mutation
// that takes afterId/beforeId/first — createNib, reorderNib, the bulk
// reorders — reads those wire arguments through PositionFromArgs or
// PlacementFromArgs, so the exactly-one rule and the unset-shape reading (a
// nil pointer, an empty string, an explicit false all count as absent) exist
// once instead of once per resolver.

// positionKind is which anchor form a Position carries. The zero value is
// deliberately none of them: a Position can only be built through
// After/Before/First (or PositionFromArgs), so "no position" is inexpressible
// where a Position is required — Move has no default arm.
type positionKind uint8

const (
	posUnset positionKind = iota
	posAfter
	posBefore
	posFirst
)

// Position says where in a group an existing member moves to: after or before
// a named member, or first. There is no "default" Position — a move without a
// destination is a validation error, which PositionFromArgs enforces.
type Position struct {
	kind   positionKind
	anchor string
}

// After positions immediately after the member with the given id.
func After(id string) Position { return Position{kind: posAfter, anchor: id} }

// Before positions immediately before the member with the given id.
func Before(id string) Position { return Position{kind: posBefore, anchor: id} }

// First positions ahead of every current member.
func First() Position { return Position{kind: posFirst} }

// Placement says where a nib ENTERING a group lands: at an explicit Position,
// or wherever the scope's default policy puts it. The default is expressible
// here and not on Position because entering a group without naming a spot is
// meaningful (creation, reassignment) while moving within one is not.
type Placement struct {
	pos       Position
	isDefault bool
}

// At places at an explicit position.
func At(p Position) Placement { return Placement{pos: p} }

// DefaultPlacement places wherever the scope's default policy decides.
func DefaultPlacement() Placement { return Placement{isDefault: true} }

// positionArgs reads the wire shape shared by both From-args constructors:
// which flags are actually set, with unset-shaped values counting as absent.
func positionArgs(afterID, beforeID *string, first *bool) (Position, int) {
	var p Position
	count := 0
	if afterID != nil && *afterID != "" {
		p = After(*afterID)
		count++
	}
	if beforeID != nil && *beforeID != "" {
		p = Before(*beforeID)
		count++
	}
	if first != nil && *first {
		p = First()
		count++
	}
	return p, count
}

// PositionFromArgs resolves the wire arguments of a move: exactly one of
// afterId, beforeId, first must be set.
func PositionFromArgs(afterID, beforeID *string, first *bool) (Position, error) {
	p, count := positionArgs(afterID, beforeID, first)
	switch {
	case count == 0:
		return Position{}, fmt.Errorf("at least one positioning flag (afterId, beforeId, first) is required")
	case count > 1:
		return Position{}, fmt.Errorf("at most one of afterId, beforeId, first may be specified")
	}
	return p, nil
}

// PlacementFromArgs resolves the wire arguments of a group entry: at most one
// of afterId, beforeId, first may be set, and none of them means the scope's
// default placement.
func PlacementFromArgs(afterID, beforeID *string, first *bool) (Placement, error) {
	p, count := positionArgs(afterID, beforeID, first)
	if count > 1 {
		return Placement{}, fmt.Errorf("at most one of afterId, beforeId, first may be specified")
	}
	if count == 0 {
		return DefaultPlacement(), nil
	}
	return At(p), nil
}

// ContainerChange is the resolved reading of an optional container argument on
// the wire (reorderNib's parentId): a nil pointer keeps the current container,
// an empty string clears to the root group, anything else names the target.
// Note the contrast with updateNib's omittable parent, where null is the
// clear: a reorder must be able to omit reparenting entirely, so nil cannot
// double as a clear here.
type ContainerChange struct {
	requested bool
	target    string
}

// ContainerChangeFromArg reads the wire argument.
func ContainerChangeFromArg(p *string) ContainerChange {
	if p == nil {
		return ContainerChange{}
	}
	return ContainerChange{requested: true, target: *p}
}

// Requested returns the change target ("" clears to the root group) and
// whether a change was requested at all.
func (c ContainerChange) Requested() (string, bool) {
	return c.target, c.requested
}
