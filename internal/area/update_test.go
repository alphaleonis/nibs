package area

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/yamlfile"
)

// planUpdate drives the whole verb. The rename-only half has its own helper
// (planRename), so the tests that came from PlanRename keep their shape.
func planUpdate(vocab, path string, u NodeUpdate) ([]byte, error) {
	return PlanUpdate([]byte(vocab), true, path, u)
}

func ptrTo(s string) *string { return &s }

// TestUpdateSetsDescriptionAndColorOnADeclaredNode is the gap this verb exists
// to close: both fields could be set at creation and changed by nothing
// afterwards, on any surface.
func TestUpdateSetsDescriptionAndColorOnADeclaredNode(t *testing.T) {
	out := mustPlan(t)(planUpdate(areaEditFixture, "web", NodeUpdate{
		Description: ptrTo("The browser client, end to end"),
		Color:       ptrTo("#00aaff"),
	}))
	node := mustParse(t, out).Get("web")
	if node == nil {
		t.Fatal("web is no longer declared after the update")
	}
	if node.Description != "The browser client, end to end" {
		t.Errorf("description = %q, want the value the update set", node.Description)
	}
	if node.Color != "#00aaff" {
		t.Errorf("color = %q, want the value the update set", node.Color)
	}
}

// TestUpdateLeavesOmittedFieldsAlone is one half of the tri-state. `auth` is the
// fixture's most decorated node — description, color and an `order:` key this
// build no longer models — so an update naming only the color must return the
// other two untouched.
func TestUpdateLeavesOmittedFieldsAlone(t *testing.T) {
	out := mustPlan(t)(planUpdate(areaEditFixture, "auth", NodeUpdate{Color: ptrTo("purple")}))
	node := mustParse(t, out).Get("auth")
	if node.Color != "purple" {
		t.Errorf("color = %q, want the value the update set", node.Color)
	}
	if node.Description != "Sign-in, sessions and tokens" {
		t.Errorf("description = %q, want the stored one, untouched by an update that did not name it", node.Description)
	}
	if !strings.Contains(out, "order: a") {
		t.Errorf("the update dropped a key it was not given anything for:\n%s", out)
	}
}

// TestUpdateClearsAFieldGivenAnEmptyValue is the other half. Clearing must REMOVE
// the key rather than write `description: ""`, which reads back as a description
// someone deleted the text out of — the same distinction
// TestCreateOmitsTheKeysItWasGivenNothingFor draws for a new node.
func TestUpdateClearsAFieldGivenAnEmptyValue(t *testing.T) {
	out := mustPlan(t)(planUpdate(areaEditFixture, "auth", NodeUpdate{Description: ptrTo("")}))
	if node := mustParse(t, out).Get("auth"); node.Description != "" {
		t.Errorf("description = %q, want it cleared", node.Description)
	}
	if strings.Contains(out, `description: ""`) {
		t.Errorf("the update wrote an empty key instead of removing it:\n%s", out)
	}
	if strings.Contains(out, "Sign-in, sessions and tokens") {
		t.Errorf("the cleared description is still in the file:\n%s", out)
	}
	// Clearing one field is not clearing the node.
	if !strings.Contains(out, `color: "#ff8800"`) {
		t.Errorf("clearing the description took the color with it:\n%s", out)
	}
}

// TestUpdateRenamesAndSetsInOneEdit: the whole point of one verb is that a
// caller fixing a name and a description does not need two round trips, each
// with its own chance to fail half way.
func TestUpdateRenamesAndSetsInOneEdit(t *testing.T) {
	out := mustPlan(t)(planUpdate(areaEditFixture, "web", NodeUpdate{
		NewName:     ptrTo("client"),
		Description: ptrTo("Everything the browser runs"),
	}))
	vocab := mustParse(t, out)
	if vocab.Get("web") != nil {
		t.Error("the old path is still declared after a rename")
	}
	node := vocab.Get("client")
	if node == nil {
		t.Fatal("the renamed node is not declared")
	}
	if node.Description != "Everything the browser runs" {
		t.Errorf("description = %q, want the value set in the same edit", node.Description)
	}
	// The rename moves the subtree's paths, which is PlanRename's contract and
	// must survive being folded into this verb.
	if vocab.Get("client/dashboard") == nil {
		t.Errorf("the child did not move with its parent: %v", vocab.Paths())
	}
}

// TestUpdateKeepsEverythingElse is the node-tree hazard restated for this verb:
// the edit walks the parsed yaml.Node tree precisely so a hand-authored file
// keeps its comments, its key order and the keys this build does not model.
func TestUpdateKeepsEverythingElse(t *testing.T) {
	out := mustPlan(t)(planUpdate(areaEditFixture, "api", NodeUpdate{Color: ptrTo("slate")}))
	for _, want := range []string{
		"# Where the work happens.",
		"# the public surface",
		"future_key:",
		"a_newer_nibs_wrote_this: true",
		"prefix: tnib-",
		"- name: webhooks",
		"color: teal",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the update lost %q:\n%s", want, out)
		}
	}
}

// TestUpdateRefusesAnUndeclaredPath: there is no node to edit, and the refusal
// must arrive as an *EditRefusal so a surface reports it as a validation
// refusal rather than as a filesystem failure.
func TestUpdateRefusesAnUndeclaredPath(t *testing.T) {
	refusal := mustRefuse(t)(planUpdate(areaEditFixture, "nosuch", NodeUpdate{Color: ptrTo("teal")}))
	if !strings.Contains(refusal.Error(), "nosuch") {
		t.Errorf("refusal = %q, want it to name the path it could not find", refusal.Error())
	}
}

// TestUpdateRefusesAColorTheLoaderWouldReject holds this verb to the contract
// create and rename already meet: whatever reaches the planner, the file it
// renders must be one the loader accepts.
func TestUpdateRefusesAColorTheLoaderWouldReject(t *testing.T) {
	refusal := mustRefuse(t)(planUpdate(areaEditFixture, "auth", NodeUpdate{Color: ptrTo("#xyz")}))
	// The shapes are the actionable half: "not a usable hex code" alone leaves a
	// caller guessing which spellings this vocabulary accepts.
	if !strings.Contains(refusal.Error(), "#RRGGBB") {
		t.Errorf("refusal = %q, want it to name the color shapes a caller may use", refusal.Error())
	}
}

// TestUpdateRefusesADescriptionThatWouldOverflowTheFile is the hazard nothing
// else guards: no validator bounds a description — ValidateNewPath, ValidateName
// and ValidateColor are the only three — so the rendered-size cap is the whole
// defense. An unbounded value over the wire is the shape that already produced a
// Critical here once: an areas.yml written past the limit cannot be opened by any
// command afterwards, so the refusal has to land BEFORE the write.
func TestUpdateRefusesADescriptionThatWouldOverflowTheFile(t *testing.T) {
	huge := strings.Repeat("x", yamlfile.MaxBytes+1)
	refusal := mustRefuse(t)(planUpdate(areaEditFixture, "auth", NodeUpdate{Description: &huge}))
	if !strings.Contains(refusal.Error(), "configuration limit") {
		t.Errorf("refusal = %q, want it to name the limit it hit", refusal.Error())
	}
}
