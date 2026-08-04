package graph

import (
	"strings"
	"testing"
)

// TestFilterTargetEmptyErrorMessage pins the TEXT of the empty-id refusal,
// which for five of the eight id-valued fields is the whole of what a caller
// gets back. cmd/list.go intercepts --parent "", --mentions "" and
// --mentioned-by "" on the flag surface before this type is ever built;
// ancestorId, descendantId, siblingId, blockingId and blockedById have no flag
// at all, so `nibs query` and the HTTP endpoint reach this message and nothing
// else.
//
// The rows come from the same reflective walk the refusal guards use, which is
// what ties Error()'s hardcoded "parentId" literal to the schema spelling it
// has to match: rename the field and this test drives the new name.
//
// Two claims per field, and both are load-bearing. The message names the field
// the CALLER wrote, so the repair is findable in the schema rather than in the
// server. And the hasParent redirection is offered for parentId ALONE —
// pointing an ancestorId mistake at hasParent would send a caller to a filter
// that answers a different question.
func TestFilterTargetEmptyErrorMessage(t *testing.T) {
	const redirection = "hasParent: false"

	for _, field := range idValuedFilterFields(t) {
		t.Run(field.name, func(t *testing.T) {
			msg := (&FilterTargetEmptyError{Field: field.name}).Error()

			if !strings.Contains(msg, field.name) {
				t.Errorf("message %q does not name %q, the field the caller wrote", msg, field.name)
			}
			if !strings.Contains(msg, "empty id") {
				t.Errorf("message %q does not say the id was empty", msg)
			}
			if !strings.Contains(msg, "it takes a nib id") {
				t.Errorf("message %q does not say what the field takes instead", msg)
			}

			wantRedirection := field.name == "parentId"
			gotRedirection := strings.Contains(msg, redirection)
			switch {
			case wantRedirection && !gotRedirection:
				t.Errorf("message %q omits the %q redirection cmd/list.go gives --parent %q", msg, redirection, "")
			case !wantRedirection && gotRedirection:
				t.Errorf("message %q offers the %q redirection, which selects on a different relationship than %s", msg, redirection, field.name)
			}
		})
	}
}
