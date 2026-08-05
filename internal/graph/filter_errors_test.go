package graph

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/alphaleonis/nibs/internal/nib"
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

// idEchoingRefusals is every refusal class that repeats a caller-supplied id in
// its message, paired with a renderer that builds one and asks for that message.
// The two are driven through identical rows so neither can be given a cap the
// other lacks.
//
// FilterTargetEmptyError is absent because it carries no ID field at all — its
// value is empty by definition of the type.
var idEchoingRefusals = map[string]func(id string) string{
	"FilterTargetNotFoundError": func(id string) string {
		return (&FilterTargetNotFoundError{Field: "parentId", ID: id}).Error()
	},
	"FilterTargetUnreadableError": func(id string) string {
		return (&FilterTargetUnreadableError{Field: "siblingId", ID: id, ReaderErr: nib.ErrNotFound}).Error()
	},
}

// wholeRunePrefix returns the longest prefix of id that is at most limit bytes
// and ends on a rune boundary.
//
// It walks runes FORWARD and stops before the first one that would overrun the
// limit, which is a different derivation from backing a byte offset off to the
// nearest rune start. Restating the implementation's own arithmetic would make
// the expectations agree with a wrong cut as readily as a right one.
//
// The two derivations agree only for WELL-FORMED UTF-8, which is why every row
// using this oracle is well-formed. DecodeRuneInString reports width 1 for a bad
// byte while RuneStart cannot tell one from a rune start, so on malformed input
// this walk fills to the limit where echoID backs off instead. Both are bounded
// and both quote to valid UTF-8 — neither cut is the right one — so malformed
// input is pinned by the length bound in TestRefusalMessageLengthIsBoundedByTheCap
// rather than by an exact prefix.
func wholeRunePrefix(id string, limit int) string {
	end := 0
	for end < len(id) {
		_, size := utf8.DecodeRuneInString(id[end:])
		if end+size > limit {
			break
		}
		end += size
	}
	return id[:end]
}

// TestRefusalMessagesCapAnOversizedEchoedID pins the cap on how much of a
// caller-supplied id a refusal message repeats.
//
// Repeating the id in full is what turns one oversized id into an oversized
// response. A relationship-field filter is evaluated once per parent nib, so a
// single refused `children(filter:{parentId:...})` produces one error object per
// nib in the store, and each carries its own copy of the rendered message. See
// echoID for the measurement. Nothing on the read path bounds that: the id is not
// length-checked anywhere between the request body and the message.
//
// The rows walk the boundary rather than the two obvious extremes. An id at or
// under the cap must come back BYTE-IDENTICAL — the whole value of echoing an id
// is that a typo is visible in it, and a cap that nibbled at ordinary ids would
// cost that for no gain. Just over the cap must be truncated, and truncated on a
// RUNE boundary: cutting a multi-byte rune in half renders a character the
// caller really sent as a bogus escape and points the diagnosis at an encoding
// problem that does not exist.
func TestRefusalMessagesCapAnOversizedEchoedID(t *testing.T) {
	const (
		// A 3-byte rune, so the cut point lands inside it rather than
		// conveniently between two of them.
		multi = "日"
		// Filler that reaches one byte short of the cap, leaving the rune that
		// follows straddling it.
		fillToStraddle = maxEchoedIDBytes - 1
	)

	tests := []struct {
		name string
		id   string
		// truncated says whether the message may abbreviate this id at all.
		truncated bool
	}{
		{"far below the cap", "nibs-abc1", false},
		{"one byte below the cap", strings.Repeat("a", maxEchoedIDBytes-1), false},
		{"exactly at the cap", strings.Repeat("a", maxEchoedIDBytes), false},
		{"one byte over the cap", strings.Repeat("a", maxEchoedIDBytes+1), true},
		{
			// The cut point falls between the first and second byte of `multi`,
			// so a byte-offset slice would emit half a rune.
			"a multi-byte rune straddling the cap",
			strings.Repeat("a", fillToStraddle) + multi + "tail",
			true,
		},
		{"entirely multi-byte", strings.Repeat(multi, 30), true},
		{
			// Only the exact empty string is FilterTargetEmptyError's class, and
			// resolveFilterTarget decides that before either of these types is
			// built (TestEveryIDValuedFilterFieldRefusesAnEmptyValue holds that
			// split). What this row pins is that the cap does not reach down and
			// rewrite the shortest id there is.
			"empty",
			"",
			false,
		},
	}

	for typeName, render := range idEchoingRefusals {
		t.Run(typeName, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					msg := render(tt.id)

					if !utf8.ValidString(msg) {
						t.Errorf("message is not valid UTF-8, so it cannot be encoded into a JSON response unchanged: %q", msg)
					}

					if !tt.truncated {
						if !strings.Contains(msg, strconv.Quote(tt.id)) {
							t.Errorf("message %q does not carry the id %q verbatim; an id within the cap must be echoed whole", msg, tt.id)
						}
						if strings.Contains(msg, "truncated") {
							t.Errorf("message %q reports a truncation for an id of %d bytes, which is within the %d-byte cap", msg, len(tt.id), maxEchoedIDBytes)
						}
						return
					}

					wantPrefix := wholeRunePrefix(tt.id, maxEchoedIDBytes)
					if !strings.Contains(msg, strconv.Quote(wantPrefix)) {
						t.Errorf("message %q does not carry the first %d bytes of the id (%q)", msg, len(wantPrefix), wantPrefix)
					}
					if strings.Contains(msg, strconv.Quote(tt.id)) {
						t.Errorf("message repeats all %d bytes of the id; that is the amplification this cap exists to remove", len(tt.id))
					}
					// The reader has to be able to tell an abbreviation from an
					// id that really ends there, and two oversized ids sharing a
					// prefix have to render differently — the original length is
					// what carries both.
					if !strings.Contains(msg, "truncated") {
						t.Errorf("message %q abbreviates the id without saying so", msg)
					}
					if !strings.Contains(msg, strconv.Itoa(len(tt.id))) {
						t.Errorf("message %q omits the id's real length (%d bytes)", msg, len(tt.id))
					}
				})
			}
		})
	}
}

// TestRefusalMessageLengthIsBoundedByTheCap is the property the cap exists for:
// the rendered message stops growing with the id, so N copies of it cannot make
// the response grow with the id either.
//
// A row asserting some exact byte count would pass just as happily against a cap
// that scaled with the input. What has to hold is that one bound covers ids
// spanning three orders of magnitude, INCLUDING the one whose rendering expands
// worst.
func TestRefusalMessageLengthIsBoundedByTheCap(t *testing.T) {
	// strconv.Quote escapes an invalid or non-printable byte as \xNN — four
	// characters for one byte, and the widest ratio it produces (a 4-byte rune
	// it cannot print costs ten characters, only 2.5x). The rest of the bound
	// covers the fixed prose, the field name, the wrapped reader error and the
	// decimal length.
	const limit = 4*maxEchoedIDBytes + 200

	ids := map[string]string{
		"1 KB of ASCII":         strings.Repeat("x", 1<<10),
		"100 KB of ASCII":       strings.Repeat("x", 100_000),
		"1 MB of ASCII":         strings.Repeat("x", 1<<20),
		"1 MB of multi-byte":    strings.Repeat("日", (1<<20)/3),
		"1 MB of invalid UTF-8": strings.Repeat("\xff", 1<<20),
	}

	for typeName, render := range idEchoingRefusals {
		for name, id := range ids {
			t.Run(fmt.Sprintf("%s/%s", typeName, name), func(t *testing.T) {
				msg := render(id)
				if len(msg) > limit {
					t.Errorf("message is %d bytes for an id of %d bytes, over the %d-byte bound; the message still scales with the id",
						len(msg), len(id), limit)
				}
			})
		}
	}
}

// TestRefusalKeepsTheFullIDInItsField pins the half of the id that is NOT
// capped. The cap belongs to the rendering, because the rendering is what a
// refusal repeats once per nib; the struct field is read once by whoever holds
// the error, and a caller correlating it against the id it sent needs the value
// it sent.
func TestRefusalKeepsTheFullIDInItsField(t *testing.T) {
	id := strings.Repeat("x", 100_000)

	notFound := &FilterTargetNotFoundError{Field: "parentId", ID: id}
	if notFound.ID != id {
		t.Errorf("FilterTargetNotFoundError.ID is %d bytes, want the %d supplied; the field is the structured value, not the message", len(notFound.ID), len(id))
	}

	unreadable := &FilterTargetUnreadableError{Field: "siblingId", ID: id, ReaderErr: nib.ErrNotFound}
	if unreadable.ID != id {
		t.Errorf("FilterTargetUnreadableError.ID is %d bytes, want the %d supplied; the field is the structured value, not the message", len(unreadable.ID), len(id))
	}
}
