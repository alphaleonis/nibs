package cmd

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/mdsection"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/projection"
	"github.com/spf13/pflag"
)

// acceptanceChecklist returns the checklist of a body's Acceptance section: the
// first heading matching "Acceptance" at any level, else "Acceptance Criteria".
// It is the one lookup behind `nibs plan`'s rollup and `nibs body --check`, so
// the rollup counts exactly the items the indices address.
func acceptanceChecklist(body string) (*mdsection.Checklist, bool) {
	if c, found := mdsection.FindChecklist(body, "Acceptance", mdsection.AnyLevel); found {
		return c, true
	}
	return mdsection.FindChecklist(body, "Acceptance Criteria", mdsection.AnyLevel)
}

// findBodyChecklist returns the checklist a checklist operation acts on: the
// --section heading when given, else the Acceptance section.
func findBodyChecklist(body string, req bodyRequest) (*mdsection.Checklist, error) {
	if req.acceptanceDefault {
		if c, found := acceptanceChecklist(body); found {
			return c, nil
		}
		return nil, errors.New(`no "Acceptance" or "Acceptance Criteria" section — the checklist flags act on it by default; name another heading with --section`)
	}
	if !req.section {
		return nil, &unhandledBodyOperationError{operation: req.operation, in: "findBodyChecklist"}
	}
	heading := sectionHeadingText(bodySection)
	if c, found := mdsection.FindChecklist(body, heading, sectionMatchLevel(bodySection)); found {
		return c, nil
	}
	_, otherLevel := mdsection.FindExact(body, heading, mdsection.AnyLevel)
	return nil, &sectionNotFoundError{heading: bodySection, operation: req.operation, otherLevelExists: otherLevel}
}

// checklistLookupError reports a findBodyChecklist failure: a defect in nibs as
// uncategorized, and anything else as a validation error.
func checklistLookupError(err error) error {
	var unhandled *unhandledBodyOperationError
	if errors.As(err, &unhandled) {
		return cmdError(bodyJSON, output.ErrUncategorized, "%s", err)
	}
	return cmdError(bodyJSON, output.ErrValidation, "%s", err)
}

// swallowedIndexValue reports whether a checklist flag's value is a flag token
// pflag bound by mistake. A value such as "-1" is a number, which index parsing
// refuses with its own message.
func swallowedIndexValue(value string) (string, bool) {
	if len(value) > 1 && value[0] == '-' && (value[1] < '0' || value[1] > '9') {
		return value, true
	}
	return "", false
}

// checklistActions returns the edit the --check, --uncheck and --remove flags
// request, keyed by 0-based item index. It refuses an index that is not a
// number, is out of range, or appears twice, in one flag or across two, and the
// refusal names the section and its item count.
func checklistActions(flags *pflag.FlagSet, c *mdsection.Checklist) (map[int]mdsection.ChecklistAction, error) {
	actions, err := parseChecklistActions(flags, c)
	if err != nil {
		return nil, withChecklistSize(err, c)
	}
	return actions, nil
}

// withChecklistSize appends the section and its item count to a refusal.
func withChecklistSize(err error, c *mdsection.Checklist) error {
	noun := "checklist items"
	if len(c.Items) == 1 {
		noun = "checklist item"
	}
	return fmt.Errorf("%w — %q has %d %s", err, sanitizeFileText(c.Heading), len(c.Items), noun)
}

func parseChecklistActions(flags *pflag.FlagSet, c *mdsection.Checklist) (map[int]mdsection.ChecklistAction, error) {
	actions := make(map[int]mdsection.ChecklistAction)
	owner := make(map[int]string)
	for _, f := range []struct {
		name   string
		values []string
		action mdsection.ChecklistAction
	}{
		{"check", bodyCheck, mdsection.ChecklistCheck},
		{"uncheck", bodyUncheck, mdsection.ChecklistUncheck},
		{"remove", bodyRemove, mdsection.ChecklistRemove},
	} {
		if !flags.Changed(f.name) {
			continue
		}
		// A repeated flag reads as one comma-separated list.
		var indices []int
		for _, value := range f.values {
			parsed, err := parseChecklistIndices(f.name, value, c)
			if err != nil {
				return nil, err
			}
			indices = append(indices, parsed...)
		}
		for _, n := range indices {
			if prev, taken := owner[n]; taken {
				if prev == f.name {
					return nil, fmt.Errorf("--%s names item %d twice", f.name, n)
				}
				return nil, fmt.Errorf("item %d is named by both --%s and --%s; give each item one change", n, prev, f.name)
			}
			owner[n] = f.name
			actions[n-1] = f.action
		}
	}
	return actions, refuseActionsInRemovedBlocks(c, actions)
}

// refuseActionsInRemovedBlocks refuses a change to an item that a removal in
// the same call deletes with its parent's block.
func refuseActionsInRemovedBlocks(c *mdsection.Checklist, actions map[int]mdsection.ChecklistAction) error {
	indices := slices.Sorted(maps.Keys(actions))
	for _, outer := range indices {
		if actions[outer] != mdsection.ChecklistRemove {
			continue
		}
		for _, inner := range indices {
			if inner != outer && c.Contains(outer, inner) {
				return fmt.Errorf("item %d lies inside item %d, which --remove deletes with its nested lines; drop one of them", inner+1, outer+1)
			}
		}
	}
	return nil
}

// parseChecklistIndices parses a comma-separated list of 1-based item numbers,
// refusing one that is not a number or names no item of c.
func parseChecklistIndices(flag, value string, c *mdsection.Checklist) ([]int, error) {
	var indices []int
	for _, field := range strings.Split(value, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			return nil, fmt.Errorf("--%s has an empty item number in %q; write numbers like 1,3", flag, value)
		}
		n, err := strconv.Atoi(field)
		if err != nil {
			return nil, fmt.Errorf("--%s takes item numbers like 1,3; %q is not a number", flag, field)
		}
		if n < 1 {
			return nil, fmt.Errorf("--%s item number %d must be 1 or greater", flag, n)
		}
		if n > len(c.Items) {
			return nil, fmt.Errorf("--%s item %d is out of range", flag, n)
		}
		indices = append(indices, n)
	}
	return indices, nil
}

// runBodyChecklistRead prints the numbered checklist and writes nothing, so it
// reads the nib without the archive fallback runBody takes, which unarchives.
func runBodyChecklistRead(ctx context.Context, resolver *graph.Resolver, id string, req bodyRequest) error {
	b, err := resolver.Query().Nib(ctx, id)
	if err != nil {
		return cmdError(bodyJSON, output.ErrNotFound, "failed to find nib: %v", err)
	}
	if b == nil {
		return cmdError(bodyJSON, output.ErrNotFound, "nib not found: %s", id)
	}
	c, err := findBodyChecklist(b.Body, req)
	if err != nil {
		return checklistLookupError(err)
	}
	if bodyJSON {
		return output.JSONRaw(map[string]any{"checklist": checklistJSON(c)})
	}
	printChecklist(c)
	return nil
}

// runBodyChecklistEdit applies --check, --uncheck and --remove to b and prints
// the checklist as it stands afterwards. When no item changes it writes
// nothing, but a given --if-match is still compared, so a stale etag fails as it
// would on a write.
func runBodyChecklistEdit(ctx context.Context, resolver *graph.Resolver, flags *pflag.FlagSet, b *nib.Nib, req bodyRequest) error {
	c, err := findBodyChecklist(b.Body, req)
	if err != nil {
		return checklistLookupError(err)
	}
	actions, err := checklistActions(flags, c)
	if err != nil {
		return cmdError(bodyJSON, output.ErrValidation, "%s", err)
	}

	newBody, err := c.Edit(actions)
	var fenced *mdsection.FencedItemError
	switch {
	case errors.As(err, &fenced):
		return cmdError(bodyJSON, output.ErrValidation, "%s", withChecklistSize(fmt.Errorf(
			"--remove: item %d holds a fenced block, so where it ends is unclear; edit the body with --replace-old or --section --set", fenced.Index+1), c))
	case err != nil:
		return cmdError(bodyJSON, output.ErrUncategorized, "internal error: %v", err)
	}
	if newBody != b.Body {
		input := model.UpdateNibInput{Body: &newBody}
		if bodyIfMatch != "" {
			input.IfMatch = &bodyIfMatch
		}
		if b, err = resolver.Mutation().UpdateNib(ctx, b.ID, input); err != nil {
			return bodyMutationError(bodyJSON, err)
		}
	} else if bodyIfMatch != "" {
		current, err := resolver.Reader.CurrentETag(b.ID)
		if err == nil && current != bodyIfMatch {
			err = &nibcore.ETagMismatchError{Provided: bodyIfMatch, Current: current}
		}
		if err != nil {
			return setMutationError(bodyJSON, err)
		}
	}

	after, err := findBodyChecklist(b.Body, req)
	if err != nil {
		return cmdError(bodyJSON, output.ErrUncategorized, "internal error: the edited body lost its checklist section: %v", err)
	}
	if !bodyJSON {
		printChecklist(after)
		return nil
	}
	card, _ := projection.ViewFields(string(projection.ViewCard))
	p, err := projection.Project(b, card, resolver.ProjectionResolver(ctx))
	if err != nil {
		return cmdError(true, output.ErrValidation, "failed to project nib: %v", err)
	}
	return output.JSONRaw(map[string]any{"nib": p, "checklist": checklistJSON(after)})
}

// checklistItemJSON is one item of the --json "checklist" array.
type checklistItemJSON struct {
	Index   int    `json:"index"`
	Checked bool   `json:"checked"`
	Text    string `json:"text"`
}

func checklistJSON(c *mdsection.Checklist) []checklistItemJSON {
	items := make([]checklistItemJSON, 0, len(c.Items))
	for i, item := range c.Items {
		items = append(items, checklistItemJSON{Index: i + 1, Checked: item.Checked, Text: item.Text})
	}
	return items
}

// printChecklist prints one "N. [x] text" line per item.
func printChecklist(c *mdsection.Checklist) {
	if len(c.Items) == 0 {
		fmt.Printf("No checklist items in %q.\n", sanitizeFileText(c.Heading))
		return
	}
	for i, item := range c.Items {
		mark := " "
		if item.Checked {
			mark = "x"
		}
		fmt.Println(strings.TrimRight(fmt.Sprintf("%d. [%s] %s", i+1, mark, item.Text), " "))
	}
}
