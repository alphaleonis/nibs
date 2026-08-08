import { describe, it, expect } from "vitest";
import { taskSourceLines } from "./markdown";

// `taskSourceLines` does not search the source for marker text. It accumulates
// line offsets over marked's token tree, which is sound only while a run of
// sibling `.raw` slices tiles the span its parent covers — that is what makes
// "count the \n in .raw" equal "lines this token spans". marked documents no
// such guarantee, so it is an empirical property of whichever major is pinned,
// and it has to be re-established whenever that major moves.
//
// This table is that check. Every expected value below was produced by running
// the same corpus against BOTH the outgoing and incoming marked major and
// diffing the two, rather than by reasoning about what the answer should be —
// which matters, because reasoning gets it wrong: under `indented code echoing
// markers` the four-space-indented marker is NOT a code block. Inside a list a
// four-space indent continues the list, so that line is a real task item and
// the expected value is [0, 2, 4], not [0, 4].
//
// The corpus is the construct list markdown.ts claims to survive: blockquotes,
// fenced and indented code, ordered and deeply nested lists, duplicates, prose
// and code that echo `- [ ]`, tables, HTML blocks, tab indentation, and every
// line ending (LF, CRLF, lone CR).

const CASES: Array<[name: string, body: string, expected: number[]]> = [
  ["flat task list", "- [ ] one\n- [x] two\n- [ ] three\n", [0, 1, 2]],
  ["nested task list", "- [ ] parent\n  - [x] child\n  - [ ] child2\n- [ ] sibling\n", [0, 1, 2, 3]],
  ["blockquoted tasks", "> - [ ] quoted\n> - [x] quoted two\n", [0, 1]],
  ["ordered list", "1. [ ] first\n2. [x] second\n", [0, 1]],
  ["fenced code echoing markers", "- [ ] real\n\n```\n- [ ] not real\n```\n\n- [x] also real\n", [0, 6]],
  ["indented markers continue the list", "- [ ] real\n\n    - [ ] nested\n\n- [x] real two\n", [0, 2, 4]],
  ["prose echoing markers", "A line mentioning - [ ] inline.\n\n- [ ] real\n", [2]],
  ["duplicate text", "- [ ] same\n- [ ] same\n- [ ] same\n", [0, 1, 2]],
  ["headings and paragraphs", "# Title\n\nSome prose.\n\n## Sub\n\n- [ ] task\n\nMore prose.\n", [6]],
  ["mixed emphasis", "- [ ] **bold** and _em_ and `code`\n- [x] [link](http://example.com)\n", [0, 1]],
  ["crlf endings", "- [ ] one\r\n- [x] two\r\n", [0, 1]],
  ["lone cr endings", "- [ ] one\r- [x] two\r", [0, 1]],
  ["tables", "| a | b |\n| - | - |\n| 1 | 2 |\n\n- [ ] after table\n", [4]],
  ["html block", "<div>\n- [ ] inside html\n</div>\n\n- [x] outside\n", [4]],
  ["loose list", "- [ ] one\n\n- [x] two\n\n", [0, 2]],
  ["deep nesting", "- [ ] a\n  - [ ] b\n    - [x] c\n      - [ ] d\n", [0, 1, 2, 3]],
  ["blockquote wrapping list", "> quote\n>\n> - [ ] q1\n>   - [x] q2\n", [2, 3]],
  ["tab indented", "- [ ] top\n\t- [x] tabbed\n", [0, 1]],
];

describe("taskSourceLines · marked token-tree line mapping", () => {
  it.each(CASES)("maps %s", (_name, body, expected) => {
    expect(taskSourceLines(body)).toEqual(expected);
  });

  it("returns one entry per rendered checkbox, in document order", () => {
    // The contract ActiveNibView relies on: ordinal N indexes into this array,
    // so the array must be exactly as long as the checkbox count and ascending
    // wherever tasks appear on distinct lines. A tree walk that double-counted
    // or skipped a sibling would break one or the other.
    for (const [name, body, expected] of CASES) {
      const lines = taskSourceLines(body);
      expect(lines, name).toHaveLength(expected.length);
      expect(lines, name).toEqual([...lines].sort((a, b) => a - b));
    }
  });
});
