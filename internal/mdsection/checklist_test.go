package mdsection

import (
	"errors"
	"reflect"
	"testing"
)

type itemView struct {
	Checked bool
	Text    string
}

func viewItems(c *Checklist) []itemView {
	views := []itemView{}
	for _, it := range c.Items {
		views = append(views, itemView{Checked: it.Checked, Text: it.Text})
	}
	return views
}

func TestFindChecklist(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		heading     string
		matchLevel  int
		wantFound   bool
		wantHeading string
		want        []itemView
	}{
		{
			name:        "bullets and marks",
			body:        "## Todo\n- [ ] a\n- [x] b\n* [X] c\n* [ ] d\n",
			heading:     "Todo",
			wantFound:   true,
			wantHeading: "## Todo",
			want:        []itemView{{false, "a"}, {true, "b"}, {true, "c"}, {false, "d"}},
		},
		{
			name:        "nested and indented items count",
			body:        "## Todo\n- [ ] parent\n  - [x] child\n\t- [ ] tabbed\n",
			heading:     "Todo",
			wantFound:   true,
			wantHeading: "## Todo",
			want:        []itemView{{false, "parent"}, {true, "child"}, {false, "tabbed"}},
		},
		{
			name:        "lines that are not items",
			body:        "## Todo\n+ [ ] plus\n1. [ ] ordered\n- [~] tilde\n-[ ] no space\n- [x]glued\n- [] empty\nplain [ ] text\n- [ ] real\n",
			heading:     "Todo",
			wantFound:   true,
			wantHeading: "## Todo",
			want:        []itemView{{false, "real"}},
		},
		{
			name:        "a bare marker is an item with empty text",
			body:        "## Todo\n- [ ]\n- [x]   \n",
			heading:     "Todo",
			wantFound:   true,
			wantHeading: "## Todo",
			want:        []itemView{{false, ""}, {true, ""}},
		},
		{
			name:        "items inside a fence are not numbered",
			body:        "## Todo\n- [ ] one\n```md\n- [ ] example\n```\n~~~\n- [x] tilde example\n~~~\n- [x] two\n",
			heading:     "Todo",
			wantFound:   true,
			wantHeading: "## Todo",
			want:        []itemView{{false, "one"}, {true, "two"}},
		},
		{
			name:        "an unclosed fence hides the rest of the section",
			body:        "## Todo\n- [ ] one\n```\n- [ ] hidden\n- [x] also hidden\n",
			heading:     "Todo",
			wantFound:   true,
			wantHeading: "## Todo",
			want:        []itemView{{false, "one"}},
		},
		{
			name:        "a closer shorter than the opener does not close the fence",
			body:        "## Todo\n````\n- [ ] in\n```\n- [ ] still in\n````\n- [ ] out\n",
			heading:     "Todo",
			wantFound:   true,
			wantHeading: "## Todo",
			want:        []itemView{{false, "out"}},
		},
		{
			name:        "a closer of the other character does not close the fence",
			body:        "## Todo\n```\n- [ ] in\n~~~\n- [ ] still in\n```\n- [ ] out\n",
			heading:     "Todo",
			wantFound:   true,
			wantHeading: "## Todo",
			want:        []itemView{{false, "out"}},
		},
		{
			name:        "subsection items count, later sections do not",
			body:        "## Todo\n- [ ] one\n### Sub\n- [x] sub item\n## Other\n- [ ] elsewhere\n",
			heading:     "Todo",
			wantFound:   true,
			wantHeading: "## Todo",
			want:        []itemView{{false, "one"}, {true, "sub item"}},
		},
		{
			name:        "CRLF text excludes the carriage return",
			body:        "## Todo\r\n- [ ] one\r\n- [x] two \r\n",
			heading:     "Todo",
			wantFound:   true,
			wantHeading: "## Todo",
			want:        []itemView{{false, "one"}, {true, "two"}},
		},
		{
			name:        "parenthetical fallback matches as Find does",
			body:        "### Acceptance (draft)\n- [ ] one\n",
			heading:     "Acceptance",
			wantFound:   true,
			wantHeading: "### Acceptance (draft)",
			want:        []itemView{{false, "one"}},
		},
		{
			name:        "section without items",
			body:        "## Todo\nnothing here\n",
			heading:     "Todo",
			wantFound:   true,
			wantHeading: "## Todo",
			want:        []itemView{},
		},
		{
			name:       "spelled level must match",
			body:       "## Todo\n- [ ] one\n",
			heading:    "Todo",
			matchLevel: 3,
			wantFound:  false,
		},
		{
			name:      "absent section",
			body:      "## Other\n- [ ] one\n",
			heading:   "Todo",
			wantFound: false,
		},
		{
			name:      "a heading inside a fence is not a section",
			body:      "```\n## Todo\n- [ ] one\n```\n",
			heading:   "Todo",
			wantFound: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := FindChecklist(tt.body, tt.heading, tt.matchLevel)
			if found != tt.wantFound {
				t.Fatalf("FindChecklist() found = %v, want %v", found, tt.wantFound)
			}
			if !found {
				return
			}
			if got.Heading != tt.wantHeading {
				t.Errorf("Heading = %q, want %q", got.Heading, tt.wantHeading)
			}
			if views := viewItems(got); !reflect.DeepEqual(views, tt.want) {
				t.Errorf("Items = %+v, want %+v", views, tt.want)
			}
		})
	}
}

func TestChecklistEdit(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		actions map[int]ChecklistAction
		want    string
	}{
		{
			name:    "check touches only the named lines",
			body:    "intro\n\n## Todo\n- [ ] a\n- [ ] b\n- [ ] c\n\n## Tail\n- [ ] t\n",
			actions: map[int]ChecklistAction{0: ChecklistCheck, 2: ChecklistCheck},
			want:    "intro\n\n## Todo\n- [x] a\n- [ ] b\n- [x] c\n\n## Tail\n- [ ] t\n",
		},
		{
			name:    "uncheck clears either letter case",
			body:    "## Todo\n- [x] a\n* [X] b\n",
			actions: map[int]ChecklistAction{0: ChecklistUncheck, 1: ChecklistUncheck},
			want:    "## Todo\n- [ ] a\n* [ ] b\n",
		},
		{
			name:    "remove deletes a lone item's line",
			body:    "## Todo\n- [ ] a\n- [ ] b\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\n- [ ] b\n",
		},
		{
			name:    "remove takes the item's continuation line",
			body:    "## Todo\n- [ ] a\n- [ ] b first\n  b continues\n- [ ] c\n",
			actions: map[int]ChecklistAction{1: ChecklistRemove},
			want:    "## Todo\n- [ ] a\n- [ ] c\n",
		},
		{
			name:    "remove takes the item's nested children",
			body:    "## Todo\n- [ ] a\n  - [ ] child\n  detail\n- [ ] b\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\n- [ ] b\n",
		},
		{
			name:    "remove of a nested child keeps its parent and siblings",
			body:    "## Todo\n- [ ] a\n  - [ ] child\n\n  detail\n- [ ] b\n",
			actions: map[int]ChecklistAction{1: ChecklistRemove},
			want:    "## Todo\n- [ ] a\n\n  detail\n- [ ] b\n",
		},
		{
			name:    "a subsection heading at the content column ends the block",
			body:    "## Todo\n- [ ] a\n- [ ] b\n  more\n  ### Sub\n  - [ ] s\n",
			actions: map[int]ChecklistAction{1: ChecklistRemove},
			want:    "## Todo\n- [ ] a\n  ### Sub\n  - [ ] s\n",
		},
		{
			name:    "a blank line before a shallower line ends the block and stays",
			body:    "## Todo\n- [ ] a\n\n- [ ] b\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\n\n- [ ] b\n",
		},
		{
			name:    "a blank line between deeper lines belongs to the block",
			body:    "## Todo\n- [ ] a\n\n  more of a\n\n## Next\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\n\n## Next\n",
		},
		{
			name:    "a tab indents deeper than a bullet at column 0",
			body:    "## Todo\n- [ ] a\n\t- [ ] tabbed child\n- [ ] b\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\n- [ ] b\n",
		},
		{
			name:    "two spaces are shallower than a tab-indented bullet",
			body:    "## Todo\n\t- [ ] a\n  - [ ] b\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\n  - [ ] b\n",
		},
		{
			name:    "a subsection heading directly after item text ends the block",
			body:    "## Todo\n- [ ] a\n- [ ] b\n### Sub\n- [ ] s\n",
			actions: map[int]ChecklistAction{1: ChecklistRemove},
			want:    "## Todo\n- [ ] a\n### Sub\n- [ ] s\n",
		},
		{
			name:    "a heading indented 1 space directly after item text ends the block",
			body:    "## Todo\n\n### Group A\n\n- [ ] a1\n- [ ] a2\n ### Group B\n\n- [ ] b1\n",
			actions: map[int]ChecklistAction{1: ChecklistRemove},
			want:    "## Todo\n\n### Group A\n\n- [ ] a1\n ### Group B\n\n- [ ] b1\n",
		},
		{
			name:    "a blockquote directly after item text is not a lazy continuation",
			body:    "## Todo\n- [ ] a\n> Note: keep me\n- [ ] b\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\n> Note: keep me\n- [ ] b\n",
		},
		{
			name:    "an HTML block directly after item text is not a lazy continuation",
			body:    "## Todo\n- [ ] a\n<details>\n<summary>x</summary>\n</details>\n- [ ] b\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\n<details>\n<summary>x</summary>\n</details>\n- [ ] b\n",
		},
		{
			name:    "HTML comments, closing tags and declarations end the block",
			body:    "## Todo\n- [ ] a\n<!-- c -->\n- [ ] b\n</p>\n- [ ] c\n<?x?>\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove, 1: ChecklistRemove, 2: ChecklistRemove},
			want:    "## Todo\n<!-- c -->\n</p>\n<?x?>\n",
		},
		{
			name:    "a less-than sign in prose stays a lazy continuation",
			body:    "## Todo\n- [ ] a\na < b holds\n- [ ] b\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\n- [ ] b\n",
		},
		{
			name:    "a fence in a later item's block is untouched",
			body:    "## Todo\n- [ ] a\n  - [ ] a1\n- [ ] b\n  ```\n  x\n  ```\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\n- [ ] b\n  ```\n  x\n  ```\n",
		},
		{
			name:    "an item holding a fence can still be checked",
			body:    "## Todo\n- [ ] a\n  ```\n  x\n  ```\n",
			actions: map[int]ChecklistAction{0: ChecklistCheck},
			want:    "## Todo\n- [x] a\n  ```\n  x\n  ```\n",
		},
		{
			name:    "a fence opened short of the content column ends the block",
			body:    "## Todo\n- [ ] a\n```\n- [ ] x\n```\n- [ ] b\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\n```\n- [ ] x\n```\n- [ ] b\n",
		},
		{
			name:    "a lazy continuation at column 0 belongs to the block",
			body:    "## Todo\n- [ ] x\n- [ ] a long criterion that wraps\nonto col 0 lazily\n- [ ] b\n",
			actions: map[int]ChecklistAction{1: ChecklistRemove},
			want:    "## Todo\n- [ ] x\n- [ ] b\n",
		},
		{
			name:    "a lazy continuation of a nested child goes with the child",
			body:    "## Todo\n- [ ] a\n  - [ ] child\n  wraps\n- [ ] b\n",
			actions: map[int]ChecklistAction{1: ChecklistRemove},
			want:    "## Todo\n- [ ] a\n- [ ] b\n",
		},
		{
			name:    "a shallow line after a blank line is not a lazy continuation",
			body:    "## Todo\n- [ ] a\n\nnot lazy\n- [ ] b\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\n\nnot lazy\n- [ ] b\n",
		},
		{
			name:    "a shallow list item is not a lazy continuation",
			body:    "## Todo\n- [ ] a\n1. ordered\n+ plus\n- [ ] b\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\n1. ordered\n+ plus\n- [ ] b\n",
		},
		{
			name:    "a thematic break is not a lazy continuation",
			body:    "## Todo\n- [ ] a\n___\n- [ ] b\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\n___\n- [ ] b\n",
		},
		{
			name:    "a sibling indented 1 space stays",
			body:    "## Todo\n- [ ] a\n - [ ] b\n- [ ] c\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\n - [ ] b\n- [ ] c\n",
		},
		{
			name:    "after a blank line, the content column is in the block",
			body:    "## Todo\n-   [ ] a\n\n    para\n- [ ] b\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\n- [ ] b\n",
		},
		{
			name:    "after a blank line, one column short of the content column is not",
			body:    "## Todo\n-   [ ] a\n\n   para\n- [ ] b\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\n\n   para\n- [ ] b\n",
		},
		{
			name:    "a CRLF block is removed whole",
			body:    "## Todo\r\n- [ ] a\r\n  more\r\n\r\n- [ ] b\r\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove},
			want:    "## Todo\r\n\r\n- [ ] b\r\n",
		},
		{
			name:    "combined actions address pre-edit indices",
			body:    "## Todo\n- [ ] a\n- [ ] b\n- [x] c\n- [ ] d\n",
			actions: map[int]ChecklistAction{0: ChecklistRemove, 1: ChecklistCheck, 2: ChecklistUncheck, 3: ChecklistRemove},
			want:    "## Todo\n- [x] b\n- [ ] c\n",
		},
		{
			name:    "a no-op leaves the body unchanged",
			body:    "## Todo\n- [x] a\n- [ ] b\n",
			actions: map[int]ChecklistAction{0: ChecklistCheck, 1: ChecklistUncheck},
			want:    "## Todo\n- [x] a\n- [ ] b\n",
		},
		{
			name:    "fenced lines and nested items keep their bytes",
			body:    "## Todo\n```\n- [ ] example\n```\n  * [ ] nested  \n",
			actions: map[int]ChecklistAction{0: ChecklistCheck},
			want:    "## Todo\n```\n- [ ] example\n```\n  * [x] nested  \n",
		},
		{
			name:    "CRLF lines keep their carriage return",
			body:    "## Todo\r\n- [ ] a\r\n- [ ] b\r\n- [ ] c\r\n",
			actions: map[int]ChecklistAction{0: ChecklistCheck, 1: ChecklistRemove},
			want:    "## Todo\r\n- [x] a\r\n- [ ] c\r\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, found := FindChecklist(tt.body, "Todo", AnyLevel)
			if !found {
				t.Fatal("FindChecklist() found = false")
			}
			got, err := c.Edit(tt.actions)
			if err != nil {
				t.Fatalf("Edit() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Edit() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

// An index outside Items is a caller defect, so Edit panics rather than
// guessing.
func TestChecklistEditPanicsOnIndexOutOfRange(t *testing.T) {
	c, _ := FindChecklist("## Todo\n- [ ] a\n", "Todo", AnyLevel)
	defer func() {
		if recover() == nil {
			t.Error("Edit() with index 1 of a 1-item list did not panic")
		}
	}()
	_, _ = c.Edit(map[int]ChecklistAction{1: ChecklistCheck})
}

func TestChecklistContains(t *testing.T) {
	body := "## Todo\n- [ ] a\n  - [ ] a1\n    - [ ] a1x\n\n  text\n- [ ] b\n"
	c, _ := FindChecklist(body, "Todo", AnyLevel)
	for _, tc := range []struct {
		outer, inner int
		want         bool
	}{
		{0, 1, true},
		{1, 3, false},
		{0, 2, true},
		{1, 2, true},
		{0, 3, false},
		{1, 0, false},
		{2, 1, false},
		{0, 0, false},
	} {
		if got := c.Contains(tc.outer, tc.inner); got != tc.want {
			t.Errorf("Contains(%d, %d) = %v, want %v", tc.outer, tc.inner, got, tc.want)
		}
	}
}

// Where a fence inside an item ends cannot be told safely, so removing that
// item is refused and nothing changes.
func TestChecklistEditRefusesRemovingAnItemHoldingAFence(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		index      int
	}{
		{"unclosed fence", "## Acceptance\n- [ ] a\n  ```sh\n  make\n- [ ] b\n- [ ] c\n\n## Notes\nimportant\n", 0},
		{"closed fence at the content column", "## Acceptance\n- [ ] a\n  ```yaml\n  key: v\n  ```\n- [ ] b\n", 0},
		{"closer at column 0", "## Acceptance\n- [ ] a\n  ```yaml\n  key: v\n```\n- [ ] b\n", 0},
		{"heredoc", "## Acceptance\n- [ ] a\n  ```sh\n  cat <<EOF\nx\nEOF\n  ```\n- [ ] b\n", 0},
		{"inline triple backticks", "## Acceptance\n- [ ] a\n  ```nibs list``` returns rows\n- [ ] b\n", 0},
		{"fence after a blank line", "## Acceptance\n- [ ] x\n- [ ] a\n\n  ```\n  y\n  ```\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := FindChecklist(tc.body, "Acceptance", AnyLevel)
			got, err := c.Edit(map[int]ChecklistAction{tc.index: ChecklistRemove})
			var fenced *FencedItemError
			if !errors.As(err, &fenced) || fenced.Index != tc.index {
				t.Fatalf("Edit() error = %v, want *FencedItemError for index %d", err, tc.index)
			}
			if got != "" {
				t.Errorf("Edit() body = %q, want empty on refusal", got)
			}
		})
	}
}
