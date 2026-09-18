package nibcore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/area"
)

// TestUpdateAreaSettingNoNameRewritesNoNib is the crux of one verb covering
// three fields: the member cascade is conditional on the NAME changing.
//
// A description or color edit stops declaring nothing, so it must plan like
// AddArea — no emptied path, no members, no rewrite. Were `emptied` set
// unconditionally, this edit would also run editArea's confirming membership
// scan, where a nib arriving under the area could refuse a color change for a
// reason that has nothing to do with it.
func TestUpdateAreaSettingNoNameRewritesNoNib(t *testing.T) {
	core, nibsDir := areaVerbCore(t)
	before, err := core.Get("nibs-ae01")
	if err != nil {
		t.Fatalf("the fixture's member is missing: %v", err)
	}
	wantArea := before.Area

	res, err := core.UpdateArea(context.Background(), "web", area.NodeUpdate{
		Description: ptrTo("The browser client"),
		Color:       ptrTo("teal"),
	})
	if err != nil {
		t.Fatalf("UpdateArea: %v", err)
	}

	// The cascade did not run: nothing was claimed as a member and nothing was
	// written. These are the fields a surface words "and rewrote N nibs" from, so
	// a non-empty one here is a sentence about an edit that never happened.
	if len(res.Written) != 0 {
		t.Errorf("Written = %v, want none — this edit renames nothing", res.Written)
	}
	if len(res.Members) != 0 {
		t.Errorf("Members = %v, want none — nothing stops being declared", res.Members)
	}
	if res.NewPath != "" {
		t.Errorf("NewPath = %q, want empty for an edit that does not rename", res.NewPath)
	}

	// And the member is untouched, which is what "rewrites no nib" means on disk.
	after, err := core.Get("nibs-ae01")
	if err != nil {
		t.Fatalf("the member is gone after a description edit: %v", err)
	}
	if after.Area != wantArea {
		t.Errorf("area = %q, want %q — a description edit rewrote a member", after.Area, wantArea)
	}

	if stored := storedAreasOf(t, nibsDir); !strings.Contains(stored, "The browser client") {
		t.Errorf("areas.yml does not carry the description this edit set:\n%s", stored)
	}
}

// TestUpdateAreaOmittedAndClearedDiffer pins the tri-state through the ENGINE,
// where the planner's own test pins it over bytes: a nil field leaves the stored
// value alone, and a non-nil empty one clears it. Collapse the two and a caller
// setting only a color silently wipes the description beside it.
func TestUpdateAreaOmittedAndClearedDiffer(t *testing.T) {
	core, nibsDir := areaVerbCore(t)

	if _, err := core.UpdateArea(context.Background(), "web", area.NodeUpdate{
		Description: ptrTo("Kept through the next edit"),
		Color:       ptrTo("teal"),
	}); err != nil {
		t.Fatalf("seeding the fields: %v", err)
	}

	// Color cleared, description not named at all.
	if _, err := core.UpdateArea(context.Background(), "web", area.NodeUpdate{Color: ptrTo("")}); err != nil {
		t.Fatalf("clearing the color: %v", err)
	}

	stored := storedAreasOf(t, nibsDir)
	if !strings.Contains(stored, "Kept through the next edit") {
		t.Errorf("the omitted description was dropped by an edit that did not name it:\n%s", stored)
	}
	if strings.Contains(stored, "teal") {
		t.Errorf("the cleared color survived:\n%s", stored)
	}
	if strings.Contains(stored, `color: ""`) {
		t.Errorf("the clear wrote an empty key instead of removing it:\n%s", stored)
	}
}

// TestUpdateAreaRefusesAnUpdateThatSetsNothing: a silent success is the one
// answer a caller cannot tell apart from a real edit, which is the reasoning
// AreaDispositionEmptyError already carries for a disposition nothing needs.
//
// It is refused BEFORE the store's write lock, so it cannot sit behind another
// writer — asserted by the refusal arriving over a store whose lock is held.
func TestUpdateAreaRefusesAnUpdateThatSetsNothing(t *testing.T) {
	core, nibsDir := areaVerbCore(t)
	vocabBefore := storedAreasOf(t, nibsDir)

	lock, err := AcquireStoreLock(nibsDir)
	if err != nil {
		t.Fatalf("holding the store's write lock: %v", err)
	}
	defer func() { _ = lock.Release() }()

	done := make(chan error, 1)
	go func() {
		_, err := core.UpdateArea(context.Background(), "web", area.NodeUpdate{})
		done <- err
	}()

	select {
	case err := <-done:
		var empty *AreaUpdateEmptyError
		if !errors.As(err, &empty) {
			t.Fatalf("error = %v (%T), want *AreaUpdateEmptyError", err, err)
		}
		if empty.Path != "web" {
			t.Errorf("Path = %q, want the path the caller named", empty.Path)
		}
	case <-t.Context().Done():
		t.Fatal("the refusal waited for the store's write lock, which the arguments alone answer")
	}

	if after := storedAreasOf(t, nibsDir); after != vocabBefore {
		t.Errorf("the refused update rewrote the vocabulary:\n%s", after)
	}
}
