package mdsection

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// ChecklistItem is one task-list item: a "-" or "*" bullet, at any indent,
// followed by the marker "[ ]", "[x]" or "[X]".
type ChecklistItem struct {
	// Checked is whether the marker is "[x]" or "[X]".
	Checked bool
	// Text is the rest of the line after the marker, without surrounding
	// whitespace or a trailing carriage return.
	Text string
	// line is the item's index in the body's "\n"-separated lines.
	line int
	// end is the exclusive line index where the item's block ends; see
	// blockEnd.
	end int
	// holdsFence is whether a fence opens inside the item's block.
	holdsFence bool
	// mark is the byte offset of the marker's middle character within the line.
	mark int
}

// Checklist is the task-list items of one section of a body, in document order.
type Checklist struct {
	// Heading is the matched heading line without surrounding whitespace.
	Heading string
	Items   []ChecklistItem
	lines   []string
}

// ChecklistAction is the change Checklist.Edit makes to one item.
type ChecklistAction int

const (
	ChecklistCheck ChecklistAction = iota + 1
	ChecklistUncheck
	ChecklistRemove
)

// FindChecklist returns the task-list items of the section that Find would
// return, and whether that section exists. The section's subsections count, and
// a line inside a fenced code block is never an item.
func FindChecklist(body, heading string, matchLevel int) (*Checklist, bool) {
	lines := strings.Split(body, "\n")
	sec, found := findSection(lines, heading, matchLevel)
	if !found {
		return nil, false
	}
	c := &Checklist{
		Heading: strings.TrimSpace(lines[sec.headingIdx]),
		Items:   []ChecklistItem{},
		lines:   lines,
	}
	fences := fenceOpeners(lines)
	levels := headingLevels(lines)
	for i := sec.startIdx; i < sec.endIdx; i++ {
		if fences[i] >= 0 {
			continue
		}
		if item, ok := parseChecklistItem(lines[i]); ok {
			item.line = i
			contentCol := columnAt(lines[i], item.mark-1)
			item.end, item.holdsFence = blockEnd(lines, i, sec.endIdx, contentCol, fences, levels)
			c.Items = append(c.Items, item)
		}
	}
	return c, true
}

// parseChecklistItem returns the item a line holds, and whether it holds one.
func parseChecklistItem(line string) (ChecklistItem, bool) {
	content := strings.TrimSuffix(line, "\r")
	p := len(content) - len(strings.TrimLeft(content, " \t"))
	if p >= len(content) || (content[p] != '-' && content[p] != '*') {
		return ChecklistItem{}, false
	}
	p++
	afterBullet := p
	for p < len(content) && (content[p] == ' ' || content[p] == '\t') {
		p++
	}
	if p == afterBullet || p+3 > len(content) || content[p] != '[' || content[p+2] != ']' {
		return ChecklistItem{}, false
	}
	mark := content[p+1]
	if mark != ' ' && mark != 'x' && mark != 'X' {
		return ChecklistItem{}, false
	}
	rest := content[p+3:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return ChecklistItem{}, false
	}
	return ChecklistItem{Checked: mark != ' ', Text: strings.TrimSpace(rest), mark: p + 1}, true
}

// blockEnd returns the exclusive end of the list-item block that starts at line
// start, a CommonMark list item whose content begins at contentCol, and whether
// a fence opens inside that block. The block holds the following lines indented
// to at least contentCol, and lazy continuation lines (see isLazyContinuation)
// that directly follow block text. A blank line belongs to it only when a block
// line follows. A heading, a fence opener and limit each end it, so the block
// never reaches into a fence.
func blockEnd(lines []string, start, limit, contentCol int, fences, levels []int) (int, bool) {
	end := start + 1
	// paragraph is whether the previous line is block text that a lazy line can
	// continue.
	paragraph := true
	for j := start + 1; j < limit; j++ {
		line := lines[j]
		switch {
		case levels[j] > 0:
			return end, false
		case strings.TrimSpace(line) == "":
			paragraph = false
		case fences[j] == j:
			return end, indentWidth(line) >= contentCol
		case indentWidth(line) >= contentCol || (paragraph && isLazyContinuation(line)):
			end, paragraph = j+1, true
		default:
			return end, false
		}
	}
	return end, false
}

// isLazyContinuation returns whether a line outside a fence and not a heading
// can continue the paragraph above it: it does not start a list item, a
// thematic break, a blockquote or an HTML block.
func isLazyContinuation(line string) bool {
	t := strings.TrimSpace(line)
	return !startsListItem(t) && !isThematicBreak(t) && !strings.HasPrefix(t, ">") && !startsHTMLBlock(t)
}

// startsHTMLBlock returns whether a trimmed line opens an HTML block: "<" then a
// letter, "/", "!" or "?".
func startsHTMLBlock(t string) bool {
	if len(t) < 2 || t[0] != '<' {
		return false
	}
	c := t[1]
	return c == '/' || c == '!' || c == '?' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// startsListItem returns whether a trimmed line opens a list item: a "-", "*"
// or "+" bullet, or one to nine digits and "." or ")", then whitespace or the
// end of the line.
func startsListItem(t string) bool {
	p := 0
	if t != "" && strings.IndexByte("-*+", t[0]) >= 0 {
		p = 1
	} else {
		for p < len(t) && p < 9 && t[p] >= '0' && t[p] <= '9' {
			p++
		}
		if p == 0 || p >= len(t) || (t[p] != '.' && t[p] != ')') {
			return false
		}
		p++
	}
	return p == len(t) || t[p] == ' ' || t[p] == '\t'
}

// isThematicBreak returns whether a trimmed line is three or more "-", "*" or
// "_" of one kind, with optional spaces or tabs between them.
func isThematicBreak(t string) bool {
	marks := strings.NewReplacer(" ", "", "\t", "").Replace(t)
	return len(marks) >= 3 && strings.IndexByte("-*_", marks[0]) >= 0 &&
		strings.Count(marks, marks[:1]) == len(marks)
}

// indentWidth returns the column of a line's first non-blank character.
func indentWidth(line string) int {
	return columnAt(line, len(line)-len(strings.TrimLeft(line, " \t")))
}

// columnAt returns the column of byte offset idx in line, with a tab advancing
// to the next multiple of 4, the CommonMark tab stop.
func columnAt(line string, idx int) int {
	width := 0
	for i := 0; i < idx && i < len(line); i++ {
		if line[i] == '\t' {
			width += 4 - width%4
		} else {
			width++
		}
	}
	return width
}

// Contains returns whether item inner lies inside the block of item outer, the
// lines that removing outer deletes.
func (c *Checklist) Contains(outer, inner int) bool {
	o, in := c.Items[outer], c.Items[inner]
	return in.line > o.line && in.line < o.end
}

// FencedItemError reports a removal of an item whose block holds a fenced code
// block. Where such a fence ends cannot be told safely, so Edit changes nothing.
type FencedItemError struct {
	// Index is the item's 0-based index in Items.
	Index int
}

func (e *FencedItemError) Error() string {
	return fmt.Sprintf("item %d holds a fenced block", e.Index+1)
}

// Edit returns the body with each action applied to the item at its index in
// Items. Every index refers to Items as found, so a removal does not shift the
// others. A removal deletes the item's block as blockEnd finds it. Every line
// no action changes keeps its bytes, and checking a checked item or unchecking
// an unchecked one changes nothing. An index outside Items panics.
//
// Removing an item whose block holds a fence returns a *FencedItemError for the
// lowest such index, and nothing changes.
func (c *Checklist) Edit(actions map[int]ChecklistAction) (string, error) {
	for _, index := range slices.Sorted(maps.Keys(actions)) {
		if actions[index] == ChecklistRemove && c.Items[index].holdsFence {
			return "", &FencedItemError{Index: index}
		}
	}
	lines := append([]string(nil), c.lines...)
	removed := make(map[int]bool)
	for index, action := range actions {
		item := c.Items[index]
		switch action {
		case ChecklistCheck:
			if !item.Checked {
				lines[item.line] = setMark(lines[item.line], item.mark, 'x')
			}
		case ChecklistUncheck:
			if item.Checked {
				lines[item.line] = setMark(lines[item.line], item.mark, ' ')
			}
		case ChecklistRemove:
			for i := item.line; i < item.end; i++ {
				removed[i] = true
			}
		}
	}
	kept := lines[:0]
	for i, line := range lines {
		if !removed[i] {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n"), nil
}

func setMark(line string, offset int, mark byte) string {
	return line[:offset] + string(mark) + line[offset+1:]
}
