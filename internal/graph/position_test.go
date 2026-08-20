package graph

import "testing"

// TestPositionFromArgs pins the one home of the positioning-flag algebra: the
// exactly-one rule over afterId/beforeId/first, with an unset-shaped value (a
// nil pointer, an empty string, an explicit false) counting as absent.
func TestPositionFromArgs(t *testing.T) {
	cases := []struct {
		name    string
		after   *string
		before  *string
		first   *bool
		want    Position
		wantErr string
	}{
		{name: "after", after: strPtr("x"), want: After("x")},
		{name: "before", before: strPtr("y"), want: Before("y")},
		{name: "first", first: boolPtr(true), want: First()},
		{name: "none given", wantErr: "at least one positioning flag (afterId, beforeId, first) is required"},
		{name: "empty-string after counts as unset", after: strPtr(""), wantErr: "at least one positioning flag (afterId, beforeId, first) is required"},
		{name: "false first counts as unset", first: boolPtr(false), wantErr: "at least one positioning flag (afterId, beforeId, first) is required"},
		{name: "after and before", after: strPtr("x"), before: strPtr("y"), wantErr: "at most one of afterId, beforeId, first may be specified"},
		{name: "after and first", after: strPtr("x"), first: boolPtr(true), wantErr: "at most one of afterId, beforeId, first may be specified"},
		{name: "all three", after: strPtr("x"), before: strPtr("y"), first: boolPtr(true), wantErr: "at most one of afterId, beforeId, first may be specified"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PositionFromArgs(tc.after, tc.before, tc.first)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("PositionFromArgs() error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("PositionFromArgs() unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("PositionFromArgs() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestPlacementFromArgs pins the create-side variant: all-unset is the scope's
// default placement rather than an error, and the at-most-one half is shared.
func TestPlacementFromArgs(t *testing.T) {
	cases := []struct {
		name    string
		after   *string
		before  *string
		first   *bool
		want    Placement
		wantErr string
	}{
		{name: "none given is the default placement", want: DefaultPlacement()},
		{name: "after", after: strPtr("x"), want: At(After("x"))},
		{name: "before", before: strPtr("y"), want: At(Before("y"))},
		{name: "first", first: boolPtr(true), want: At(First())},
		{name: "two flags", before: strPtr("y"), first: boolPtr(true), wantErr: "at most one of afterId, beforeId, first may be specified"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PlacementFromArgs(tc.after, tc.before, tc.first)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("PlacementFromArgs() error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("PlacementFromArgs() unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("PlacementFromArgs() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestContainerChangeFromArg pins the wire reading that used to live in an
// eight-line comment: a nil pointer keeps the current container, an empty
// string clears to the root group, anything else names the move target.
func TestContainerChangeFromArg(t *testing.T) {
	if _, ok := ContainerChangeFromArg(nil).Requested(); ok {
		t.Error("ContainerChangeFromArg(nil) requests a change; nil must mean keep")
	}
	if target, ok := ContainerChangeFromArg(strPtr("")).Requested(); !ok || target != "" {
		t.Errorf("ContainerChangeFromArg(\"\") = (%q, %v), want a requested clear-to-root", target, ok)
	}
	if target, ok := ContainerChangeFromArg(strPtr("p1")).Requested(); !ok || target != "p1" {
		t.Errorf("ContainerChangeFromArg(\"p1\") = (%q, %v), want a requested move to p1", target, ok)
	}
}
