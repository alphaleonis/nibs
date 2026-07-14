import { describe, it, expect, vi } from "vitest";
import DOMPurify from "dompurify";
import { renderMarkdown, toggleTaskLine, taskSourceLines, TAG_REGEX } from "./markdown";

/** Parse rendered HTML and return the checked-state of each task checkbox, in
 *  ascending `data-task-ordinal` order. */
function renderedCheckboxStates(html: string): boolean[] {
  const tpl = document.createElement("template");
  tpl.innerHTML = html;
  const boxes = Array.from(
    tpl.content.querySelectorAll<HTMLInputElement>("input[data-task-ordinal]"),
  );
  boxes.sort(
    (a, b) =>
      Number(a.getAttribute("data-task-ordinal")) - Number(b.getAttribute("data-task-ordinal")),
  );
  return boxes.map((b) => b.hasAttribute("checked"));
}

/** The `data-task-ordinal` values a body renders, in document order. */
function renderedOrdinals(html: string): number[] {
  const tpl = document.createElement("template");
  tpl.innerHTML = html;
  return Array.from(
    tpl.content.querySelectorAll<HTMLInputElement>("input[data-task-ordinal]"),
  ).map((b) => Number(b.getAttribute("data-task-ordinal")));
}

describe("renderMarkdown", () => {
  it("renders a heading as h1", () => {
    const html = renderMarkdown("# Hello");
    expect(html).toContain("<h1");
    expect(html).toContain("Hello");
  });

  it("strips XSS script tags", () => {
    const html = renderMarkdown('<script>alert("xss")</script>Safe text');
    expect(html).not.toContain("<script>");
    expect(html).toContain("Safe text");
  });

  it("returns empty string for empty input", () => {
    expect(renderMarkdown("")).toBe("");
  });

  it("renders code blocks as pre>code", () => {
    const html = renderMarkdown("```\nconst x = 1;\n```");
    expect(html).toContain("<pre>");
    expect(html).toContain("<code>");
    expect(html).toContain("const x = 1;");
  });
});

describe("renderMarkdown mention rewriting", () => {
  it("rewrites a resolved short-form #<id> into an anchor with data-nib-id", () => {
    const html = renderMarkdown("see #gx0f", (t) => (t === "gx0f" ? "nibs-gx0f" : null));
    expect(html).toContain('data-nib-id="nibs-gx0f"');
    expect(html).toContain(">#gx0f</a>");
  });

  it("rewrites a resolved full-form #nibs-<id> into an anchor with the same full id", () => {
    const html = renderMarkdown("see #nibs-gx0f", (t) => (t === "nibs-gx0f" ? "nibs-gx0f" : null));
    expect(html).toContain('data-nib-id="nibs-gx0f"');
    expect(html).toContain(">#nibs-gx0f</a>");
  });

  it("leaves an unresolved token as plain text (no anchor)", () => {
    const html = renderMarkdown("see #unknown", () => null);
    expect(html).not.toContain("data-nib-id");
    expect(html).not.toContain("<a ");
    expect(html).toContain("#unknown");
  });

  it("omitted resolver disables rewriting (preview behavior)", () => {
    const html = renderMarkdown("see #gx0f");
    expect(html).not.toContain("data-nib-id");
    expect(html).not.toContain("<a ");
    expect(html).toContain("#gx0f");
  });

  it("does not rewrite #<id> inside a fenced code block", () => {
    const html = renderMarkdown("```\nsee #gx0f\n```", () => "nibs-gx0f");
    expect(html).not.toContain("data-nib-id");
    expect(html).toContain("#gx0f");
    expect(html).toContain("<pre>");
  });

  it("does not rewrite #<id> inside an inline code span", () => {
    const html = renderMarkdown("use `#gx0f` here", () => "nibs-gx0f");
    expect(html).not.toContain("data-nib-id");
    expect(html).toContain("#gx0f");
    expect(html).toContain("<code>");
  });

  it("does not mangle an existing markdown link whose href starts with #", () => {
    const html = renderMarkdown("[see](#elsewhere)", () => "nibs-anything");
    // The existing anchor should preserve its href, and the linked text is "see"
    expect(html).toContain('href="#elsewhere"');
    expect(html).toContain(">see</a>");
    expect(html).not.toContain("data-nib-id");
  });

  it("does not rewrite #<id> when adjacent to a word char (e.g. email#gx0f)", () => {
    const html = renderMarkdown("email#gx0f", () => "nibs-gx0f");
    expect(html).not.toContain("data-nib-id");
    expect(html).toContain("email#gx0f");
  });

  it("strips XSS scripts but still rewrites a valid mention in the same body", () => {
    const html = renderMarkdown(
      '<script>alert(1)</script>#gx0f',
      (t) => (t === "gx0f" ? "nibs-gx0f" : null),
    );
    expect(html).not.toContain("<script");
    expect(html).toContain('data-nib-id="nibs-gx0f"');
    expect(html).toContain(">#gx0f</a>");
  });

  it("rewrites multiple resolved mentions with distinct data-nib-id values", () => {
    const html = renderMarkdown(
      "see #aaaa and #bbbb",
      (t) => (t === "aaaa" ? "nibs-aaaa" : t === "bbbb" ? "nibs-bbbb" : null),
    );
    expect(html).toContain('data-nib-id="nibs-aaaa"');
    expect(html).toContain('data-nib-id="nibs-bbbb"');
    expect(html).toContain(">#aaaa</a>");
    expect(html).toContain(">#bbbb</a>");
  });

  it("generated anchor carries the text-link class", () => {
    const html = renderMarkdown("see #gx0f", () => "nibs-gx0f");
    expect(html).toContain("text-link");
  });

  it("escapes malicious resolver return values (defense in depth)", () => {
    const html = renderMarkdown("see #x", () => '" onclick=alert(1) "');
    expect(html).not.toMatch(/onclick\s*=/);
    expect(html).not.toMatch(/alert\s*\(1\)/);
  });

  it("round-trips non-mention content without corruption", () => {
    const input = "See &amp; more <em>text</em> with <br/>";
    const html = renderMarkdown(input, () => null);
    // Entity preserved / not corrupted:
    expect(html).toMatch(/&amp;|&#38;/);
    expect(html).toContain("<em>");
  });

  it("treats a throwing resolver as unresolved", () => {
    const html = renderMarkdown("see #gx0f", () => {
      throw new Error("boom");
    });
    expect(html).toContain("#gx0f");
    expect(html).not.toContain("data-nib-id");
  });
});

describe("renderMarkdown task-list checkboxes", () => {
  it("renders task items as ENABLED checkboxes (no disabled attr)", () => {
    const html = renderMarkdown("- [ ] one\n- [x] two");
    expect(html).toContain("<input");
    expect(html).toContain('type="checkbox"');
    expect(html).not.toContain("disabled");
  });

  it("stamps each checkbox with its document-order ordinal", () => {
    const html = renderMarkdown("- [ ] one\n- [x] two\n- [ ] three");
    expect(html).toContain('data-task-ordinal="0"');
    expect(html).toContain('data-task-ordinal="1"');
    expect(html).toContain('data-task-ordinal="2"');
  });

  it("marks a checked task item's checkbox as checked", () => {
    const html = renderMarkdown("- [ ] unchecked\n- [x] checked");
    // Parse to inspect the checked property robustly (attr serialization varies).
    const tpl = document.createElement("template");
    tpl.innerHTML = html;
    const boxes = tpl.content.querySelectorAll<HTMLInputElement>("input[data-task-ordinal]");
    expect(boxes).toHaveLength(2);
    expect(boxes[0].hasAttribute("checked")).toBe(false);
    expect(boxes[1].hasAttribute("checked")).toBe(true);
  });

  it("assigns ordinals across nesting in document order", () => {
    const html = renderMarkdown("- [ ] a\n  - [x] b\n- [ ] c");
    const tpl = document.createElement("template");
    tpl.innerHTML = html;
    const ords = Array.from(
      tpl.content.querySelectorAll<HTMLInputElement>("input[data-task-ordinal]"),
    ).map((el) => el.getAttribute("data-task-ordinal"));
    expect(ords).toEqual(["0", "1", "2"]);
  });

  it("emits NO checkbox for a `[ ]` inside a fenced code block", () => {
    const html = renderMarkdown("```\n- [ ] not a task\n```");
    expect(html).not.toContain("data-task-ordinal");
    expect(html).not.toContain('type="checkbox"');
    expect(html).toContain("<pre>");
  });

  it("resets ordinals per render call (no cross-call leakage)", () => {
    renderMarkdown("- [ ] a\n- [ ] b\n- [ ] c");
    const html = renderMarkdown("- [ ] only");
    expect(html).toContain('data-task-ordinal="0"');
    expect(html).not.toContain('data-task-ordinal="1"');
  });

  it("strips a non-checkbox raw <input> smuggled via inline HTML", () => {
    const html = renderMarkdown('before <input type="text" value="x"> after');
    expect(html).not.toContain("<input");
    expect(html).toContain("before");
    expect(html).toContain("after");
  });

  it("strips a raw <input> with an event handler (defense in depth)", () => {
    const html = renderMarkdown('x <input type="button" onclick="alert(1)"> y');
    expect(html).not.toContain("<input");
    expect(html).not.toMatch(/onclick/i);
  });

  it("keeps task checkboxes clickable alongside a resolved mention", () => {
    const html = renderMarkdown("- [ ] do #gx0f", (t) => (t === "gx0f" ? "nibs-gx0f" : null));
    expect(html).toContain('data-task-ordinal="0"');
    expect(html).toContain('data-nib-id="nibs-gx0f"');
  });
});

describe("toggleTaskLine", () => {
  it("flips an unchecked item to checked (ordinal 0)", () => {
    expect(toggleTaskLine("- [ ] task", 0)).toBe("- [x] task");
  });

  it("flips a checked item back to unchecked", () => {
    expect(toggleTaskLine("- [x] task", 0)).toBe("- [ ] task");
  });

  it("treats uppercase [X] as checked and unchecks it", () => {
    expect(toggleTaskLine("- [X] task", 0)).toBe("- [ ] task");
  });

  it("flips only the addressed ordinal, leaving other task lines untouched", () => {
    const body = "- [ ] a\n- [ ] b\n- [ ] c";
    expect(toggleTaskLine(body, 1)).toBe("- [ ] a\n- [x] b\n- [ ] c");
  });

  it("maps ordinals by position, NOT text — duplicate lines don't drift", () => {
    const body = "- [ ] same\n- [ ] same\n- [ ] same";
    expect(toggleTaskLine(body, 2)).toBe("- [ ] same\n- [ ] same\n- [x] same");
    expect(toggleTaskLine(body, 0)).toBe("- [x] same\n- [ ] same\n- [ ] same");
  });

  it("preserves indentation and nesting when flipping a nested item", () => {
    const body = "- [ ] parent\n  - [ ] child\n  - [x] child2";
    expect(toggleTaskLine(body, 1)).toBe("- [ ] parent\n  - [x] child\n  - [x] child2");
    expect(toggleTaskLine(body, 2)).toBe("- [ ] parent\n  - [ ] child\n  - [ ] child2");
  });

  it("supports * and + bullets", () => {
    expect(toggleTaskLine("* [ ] star", 0)).toBe("* [x] star");
    expect(toggleTaskLine("+ [x] plus", 0)).toBe("+ [ ] plus");
  });

  it("supports ordered-list task markers (matches marked)", () => {
    expect(toggleTaskLine("1. [ ] first", 0)).toBe("1. [x] first");
    expect(toggleTaskLine("2) [x] second", 0)).toBe("2) [ ] second");
  });

  it("preserves surrounding non-task content and trailing text", () => {
    const body = "# Title\n\nSome text.\n\n- [ ] todo item with **bold**\n\nMore text.";
    expect(toggleTaskLine(body, 0)).toBe(
      "# Title\n\nSome text.\n\n- [x] todo item with **bold**\n\nMore text.",
    );
  });

  it("does NOT count a `[ ]` inside a fenced code block (ordinal skips it)", () => {
    const body = "- [ ] real\n```\n- [ ] fake in code\n```\n- [ ] real2";
    // Ordinal 1 is the SECOND real task, not the fenced one.
    expect(toggleTaskLine(body, 1)).toBe(
      "- [ ] real\n```\n- [ ] fake in code\n```\n- [x] real2",
    );
  });

  it("leaves a fenced-code `[ ]` untouched even when flipping around it", () => {
    const body = "```\n- [ ] fake\n```\n- [ ] real";
    expect(toggleTaskLine(body, 0)).toBe("```\n- [ ] fake\n```\n- [x] real");
  });

  it("handles tilde fences too", () => {
    const body = "~~~\n- [ ] fake\n~~~\n- [ ] real";
    expect(toggleTaskLine(body, 0)).toBe("~~~\n- [ ] fake\n~~~\n- [x] real");
  });

  it("returns the body unchanged for an out-of-range ordinal", () => {
    const body = "- [ ] a\n- [ ] b";
    expect(toggleTaskLine(body, 5)).toBe(body);
    expect(toggleTaskLine(body, -1)).toBe(body);
  });

  it("returns the body unchanged when there are no task lines", () => {
    const body = "just a paragraph\n\n- a plain bullet\n1. an ordered item";
    expect(toggleTaskLine(body, 0)).toBe(body);
  });

  it("does not treat a bare `- [ ]` with no content as a task (matches marked)", () => {
    // `- [ ]` (no trailing content) is not a task item; the real one is ordinal 0.
    const body = "- [ ]\n- [ ] real";
    expect(toggleTaskLine(body, 0)).toBe("- [ ]\n- [x] real");
  });

  it("does not treat `[ ]` mid-text as a task", () => {
    expect(toggleTaskLine("- foo [ ] bar", 0)).toBe("- foo [ ] bar");
  });
});

// These lock the fixes for review findings #1/#2/#4: the ordinal->source-line
// map is now derived from marked's tokenizer (the same parser that renders), so
// clicking the Nth rendered checkbox flips the Nth task line even for the
// constructs where the old flat line-scanner diverged (blockquote, imperfect
// fence detection, tab/HTML-block indented code). The old regex impl flipped the
// WRONG source line (or a line inside code) for every case below.
describe("toggleTaskLine — divergence classes (token-derived map)", () => {
  it("#1 blockquoted task: flips the QUOTED line, not the real one below it", () => {
    // Old impl: `> - [ ]` never matched, so ordinal 0 flipped `real` (line 1).
    expect(toggleTaskLine("> - [ ] quoted\n- [ ] real", 0)).toBe("> - [x] quoted\n- [ ] real");
    expect(toggleTaskLine("> - [ ] quoted\n- [ ] real", 1)).toBe("> - [ ] quoted\n- [x] real");
  });

  it("#1 tasks nested inside a blockquote map in document order", () => {
    const body = "> - [ ] q1\n> - [x] q2\n\n- [ ] real";
    expect(toggleTaskLine(body, 0)).toBe("> - [x] q1\n> - [x] q2\n\n- [ ] real");
    expect(toggleTaskLine(body, 1)).toBe("> - [ ] q1\n> - [ ] q2\n\n- [ ] real");
    expect(toggleTaskLine(body, 2)).toBe("> - [ ] q1\n> - [x] q2\n\n- [x] real");
  });

  it("#2 fence closer with trailing content: whole block is code, so NO tasks", () => {
    // ```x is not a valid closer -> marked treats everything as one code block.
    const body = "```\n- [ ] a\n```x\n- [ ] b";
    expect(taskSourceLines(body)).toEqual([]);
    expect(toggleTaskLine(body, 0)).toBe(body); // nothing to flip
  });

  it("#2 fence indented >=4 spaces inside a nested list item is code (not a task)", () => {
    const body = "- outer\n\n    ```\n    - [ ] fake\n    ```\n\n- [ ] real";
    // Only `real` (line 6) is a task; the fenced `- [ ] fake` is code.
    expect(taskSourceLines(body)).toEqual([6]);
    expect(toggleTaskLine(body, 0)).toBe(
      "- outer\n\n    ```\n    - [ ] fake\n    ```\n\n- [x] real",
    );
  });

  it("#4 tab-indented top-level code: only the real task flips", () => {
    // `\t- [ ] tabbed` is an indented code block; `real` is the only task.
    const body = "\t- [ ] tabbed\n- [ ] real";
    expect(taskSourceLines(body)).toEqual([1]);
    expect(toggleTaskLine(body, 0)).toBe("\t- [ ] tabbed\n- [x] real");
  });

  it("#4 HTML-block code: a `[ ]` inside an HTML block is not a task", () => {
    const body = "<div>\n- [ ] x\n</div>\n\n- [ ] real";
    expect(taskSourceLines(body)).toEqual([4]);
    expect(toggleTaskLine(body, 0)).toBe("<div>\n- [ ] x\n</div>\n\n- [x] real");
  });

  it("nested tasks map to their own indented source lines", () => {
    const body = "- [ ] a\n  - [x] b\n- [ ] c";
    expect(taskSourceLines(body)).toEqual([0, 1, 2]);
    expect(toggleTaskLine(body, 1)).toBe("- [ ] a\n  - [ ] b\n- [ ] c");
  });

  it("CRLF bodies flip the correct line and preserve \\r\\n endings", () => {
    const body = "- [ ] a\r\n- [x] b";
    expect(taskSourceLines(body)).toEqual([0, 1]);
    expect(toggleTaskLine(body, 0)).toBe("- [x] a\r\n- [x] b");
    expect(toggleTaskLine(body, 1)).toBe("- [ ] a\r\n- [ ] b");
  });

  it("duplicate task lines flip by position, not text", () => {
    const body = "- [ ] same\n- [ ] same\n- [ ] same";
    expect(taskSourceLines(body)).toEqual([0, 1, 2]);
    expect(toggleTaskLine(body, 1)).toBe("- [ ] same\n- [x] same\n- [ ] same");
  });
});

// Findings #1/#2 (round 2): the ordinal->source-line map must stay MONOTONIC
// over the FULL token stream. A non-structural token (fenced code documenting
// checklist syntax, or prose echoing `- [ ] x`) sitting before/between real
// tasks must be CONSUMED by the search cursor so a real task's checkbox can
// never back-match into it; and lone-`\r` / mixed line endings must map each
// checkbox to its own line (marked normalizes lone `\r` to `\n`).
describe("toggleTaskLine — cursor must not back-match into non-task regions (#1)", () => {
  it("a fenced ```md block documenting `- [ ]` before the real task does not steal the ordinal", () => {
    const body = "How to:\n\n```md\n- [ ] Buy milk\n```\n\n- [ ] Buy milk";
    // The ONLY real task is line 6; line 3 lives inside the code fence.
    expect(taskSourceLines(body)).toEqual([6]);
    expect(toggleTaskLine(body, 0)).toBe(
      "How to:\n\n```md\n- [ ] Buy milk\n```\n\n- [x] Buy milk",
    );
  });

  it("a ```md doc fence BETWEEN two real tasks does not shift either ordinal", () => {
    const body = "- [ ] real\n\n```md\n- [ ] doc\n```\n\n- [ ] real2";
    expect(taskSourceLines(body)).toEqual([0, 6]);
    expect(toggleTaskLine(body, 0)).toBe(
      "- [x] real\n\n```md\n- [ ] doc\n```\n\n- [ ] real2",
    );
    expect(toggleTaskLine(body, 1)).toBe(
      "- [ ] real\n\n```md\n- [ ] doc\n```\n\n- [x] real2",
    );
  });

  it("a prose paragraph literally containing `- [ ] a` between two real tasks maps to the REAL lines", () => {
    const body = "- [ ] a\n\ntext that says - [ ] a literally as prose\n\n- [ ] a";
    expect(taskSourceLines(body)).toEqual([0, 4]);
    // Flipping ordinal 1 must touch the real line 4, NOT the prose echo on line 2.
    expect(toggleTaskLine(body, 1)).toBe(
      "- [ ] a\n\ntext that says - [ ] a literally as prose\n\n- [x] a",
    );
    expect(toggleTaskLine(body, 0)).toBe(
      "- [x] a\n\ntext that says - [ ] a literally as prose\n\n- [ ] a",
    );
  });

  it("an HTML block containing `- [ ]` before the real task does not steal the ordinal", () => {
    const body = "<pre>- [ ] second</pre>\n\n- [ ] second";
    const map = taskSourceLines(body);
    expect(map).toHaveLength(1);
    expect(map).toEqual([2]);
    expect(toggleTaskLine(body, 0)).toBe("<pre>- [ ] second</pre>\n\n- [x] second");
  });
});

describe("toggleTaskLine — line-ending normalization (#2)", () => {
  it("lone-`\\r` separated tasks map to distinct lines and preserve the `\\r`", () => {
    const body = "- [ ] a\r- [ ] b";
    expect(taskSourceLines(body)).toEqual([0, 1]);
    expect(toggleTaskLine(body, 1)).toBe("- [ ] a\r- [x] b");
    expect(toggleTaskLine(body, 0)).toBe("- [x] a\r- [ ] b");
  });

  it("mixed CRLF/LF/CR in one body maps each checkbox to its own line", () => {
    const body = "- [ ] a\r\n- [ ] b\n- [ ] c\r- [ ] d";
    expect(taskSourceLines(body)).toEqual([0, 1, 2, 3]);
    expect(toggleTaskLine(body, 0)).toBe("- [x] a\r\n- [ ] b\n- [ ] c\r- [ ] d");
    expect(toggleTaskLine(body, 1)).toBe("- [ ] a\r\n- [x] b\n- [ ] c\r- [ ] d");
    expect(toggleTaskLine(body, 2)).toBe("- [ ] a\r\n- [ ] b\n- [x] c\r- [ ] d");
    expect(toggleTaskLine(body, 3)).toBe("- [ ] a\r\n- [ ] b\n- [ ] c\r- [x] d");
  });
});

// Round 3 (redesign): a fenced/indented code block echoing `- [ ]` INSIDE a
// list item, FOLLOWED by a multi-line nested sub-task, previously back-matched
// the sub-task's ordinal onto a source line INSIDE the code fence (silent
// code-block corruption on Save). The ordinal->line map is now derived by
// accumulating LINE offsets over marked's own token tree (a `- [ ]` inside a
// `code`/`html`/prose span is inert text, never a `checkbox` token), so it can
// never be recorded and a real checkbox can never map onto it.
describe("toggleTaskLine — code-echo INSIDE a list item, then a nested sub-task (#round3)", () => {
  it("the proven failing body maps to [0, 4], NOT [0, 2] inside the fence", () => {
    const body = "- [ ] Set up CI\n  ```\n  - [ ] example\n  ```\n  - [ ] Deploy\n    - detail";
    expect(taskSourceLines(body)).toEqual([0, 4]);
    // ordinal 1 is "Deploy" (line 4) — NOT the fenced `- [ ] example` (line 2).
    // The fenced echo stays `- [ ] example`; only Deploy flips.
    expect(toggleTaskLine(body, 1)).toBe(
      "- [ ] Set up CI\n  ```\n  - [ ] example\n  ```\n  - [x] Deploy\n    - detail",
    );
    expect(toggleTaskLine(body, 0)).toBe(
      "- [x] Set up CI\n  ```\n  - [ ] example\n  ```\n  - [ ] Deploy\n    - detail",
    );
  });

  it("yaml fence in item + nested sub-task maps to [0, 5]", () => {
    const body =
      "- [ ] Configure pipeline\n  ```yaml\n  steps:\n    - [ ] run\n  ```\n  - [ ] Add caching\n    - use actions/cache";
    expect(taskSourceLines(body)).toEqual([0, 5]);
    expect(toggleTaskLine(body, 1)).toBe(
      "- [ ] Configure pipeline\n  ```yaml\n  steps:\n    - [ ] run\n  ```\n  - [x] Add caching\n    - use actions/cache",
    );
  });

  it("multiple independent fence-echo occurrences (multi-line subtasks) do not compound", () => {
    // Each sub-task ("b", "d") is MULTI-LINE (has a child bullet), so the old
    // impl parked the cursor and mapped ordinals 1 and 3 INTO the fences
    // (old buggy map ~= [0, 2, 8, 10]). Both must resolve to the real subtasks.
    const body =
      "- [ ] a\n  ```\n  - [ ] x\n  ```\n  - [ ] b\n    - d1\n- [ ] c\n  ```\n  - [ ] y\n  ```\n  - [ ] d\n    - d2";
    expect(taskSourceLines(body)).toEqual([0, 4, 6, 10]);
    expect(toggleTaskLine(body, 1)).toBe(
      "- [ ] a\n  ```\n  - [ ] x\n  ```\n  - [x] b\n    - d1\n- [ ] c\n  ```\n  - [ ] y\n  ```\n  - [ ] d\n    - d2",
    );
    expect(toggleTaskLine(body, 3)).toBe(
      "- [ ] a\n  ```\n  - [ ] x\n  ```\n  - [ ] b\n    - d1\n- [ ] c\n  ```\n  - [ ] y\n  ```\n  - [x] d\n    - d2",
    );
  });

  it("ordered-list outer marker with a fence echo + nested multi-line subtask maps to [0, 4]", () => {
    const body = "1. [ ] Set up\n   ```\n   - [ ] echo\n   ```\n   - [ ] Deploy\n     - detail";
    expect(taskSourceLines(body)).toEqual([0, 4]);
    expect(toggleTaskLine(body, 1)).toBe(
      "1. [ ] Set up\n   ```\n   - [ ] echo\n   ```\n   - [x] Deploy\n     - detail",
    );
  });

  it("a fence echo nested inside a blockquote item + sub-task maps to [0, 4]", () => {
    const body =
      "> - [ ] Set up\n>   ```\n>   - [ ] echo\n>   ```\n>   - [ ] Deploy\n>     - detail";
    expect(taskSourceLines(body)).toEqual([0, 4]);
    expect(toggleTaskLine(body, 1)).toBe(
      "> - [ ] Set up\n>   ```\n>   - [ ] echo\n>   ```\n>   - [x] Deploy\n>     - detail",
    );
  });
});

// The regression LOCK the test-reviewer asked for: render each tricky body,
// read the DOM `data-task-ordinal` values in document order, then assert that
// `toggleTaskLine(body, ord)` flips EXACTLY the checkbox rendered at that ordinal
// (and no other) — so the render side and the flip side can never silently drift
// apart again. Under the old regex scanner, the blockquote / fence / tab / HTML
// cases flipped the wrong (or a code) line, changing a different checkbox.
describe("ordinal-consistency cross-check (render <-> toggleTaskLine)", () => {
  const trickyBodies: Record<string, string> = {
    simple: "- [ ] a\n- [x] b\n- [ ] c",
    blockquote: "> - [ ] quoted\n- [ ] real",
    "blockquote multi": "> - [ ] q1\n> - [x] q2\n\n- [ ] real",
    nested: "- [ ] a\n  - [x] b\n- [ ] c",
    "deeply nested": "- [ ] a\n  - [ ] b\n    - [x] c\n- [ ] d",
    "loose (blank lines)": "- [ ] a\n\n  - [ ] b\n\n- [ ] c",
    "fence between tasks": "- [ ] real\n```\n- [ ] fake\n```\n- [ ] real2",
    "wide-indent nested fence": "- outer\n\n    ```\n    - [ ] fake\n    ```\n\n- [ ] real",
    "tab-indented code": "\t- [ ] tabbed\n- [ ] real",
    "html block": "<div>\n- [ ] x\n</div>\n\n- [ ] real",
    "ordered markers": "1. [ ] first\n2) [x] second",
    "star and plus bullets": "* [ ] star\n+ [x] plus",
    crlf: "- [ ] a\r\n- [x] b",
    duplicates: "- [ ] same\n- [ ] same\n- [ ] same",
    "uppercase X": "- [X] done\n- [ ] todo",
    "text after checkbox with brackets": "- [ ] see [x] in text\n- [ ] two",
    // #1: a fenced ```md block documenting `- [ ]` syntax must not steal an
    // ordinal. IDENTICAL text to the real task so the list `.raw` back-matches
    // into the fence unless the cursor has consumed the code block.
    "doc fence before task": "How to:\n\n```md\n- [ ] task\n```\n\n- [ ] task",
    "doc fence between tasks": "- [ ] task\n\n```md\n- [ ] task\n```\n\n- [ ] task",
    "doc fence after task": "- [ ] task\n\n```md\n- [ ] task\n```",
    // #1: prose that literally echoes `- [ ] a` between two real tasks.
    "prose echo between tasks": "- [ ] a\n\ntext with - [ ] a inline as prose\n\n- [ ] a",
    // #1 (adversarial): HTML comment / top-level 4-space indented code echoing a task.
    "html comment echo": "<!-- - [ ] hidden -->\n\n- [ ] hidden",
    "top-level indented-code echo": "    - [ ] snippet\n\n- [ ] snippet",
    // #2: lone `\r` and mixed line endings — each checkbox its own line.
    "lone CR separators": "- [ ] a\r- [ ] b",
    "mixed CRLF/LF/CR": "- [ ] a\r\n- [ ] b\n- [ ] c\r- [ ] d",
    // #round3: a fenced/indented code echo INSIDE a list item, followed by a
    // multi-line nested sub-task — the shape all three prior rounds shipped
    // broken. The sub-task must NOT map onto a line inside the fence.
    "fence echo in item + nested subtask (proven)":
      "- [ ] Set up CI\n  ```\n  - [ ] example\n  ```\n  - [ ] Deploy\n    - detail",
    "yaml fence in item + nested subtask":
      "- [ ] Configure pipeline\n  ```yaml\n  steps:\n    - [ ] run\n  ```\n  - [ ] Add caching\n    - use actions/cache",
    "tilde fence in item + nested subtask":
      "- [ ] Set up\n  ~~~\n  - [ ] echo\n  ~~~\n  - [ ] Deploy\n    - more",
    "fence echo in item, ordered outer + nested subtask":
      "1. [ ] Set up\n   ```\n   - [ ] echo\n   ```\n   - [ ] Deploy\n     - detail",
    "fence echo nested in blockquote":
      "> - [ ] Set up\n>   ```\n>   - [ ] echo\n>   ```\n>   - [ ] Deploy\n>     - detail",
    "multiple fence-echo occurrences (multi-line subtasks)":
      "- [ ] a\n  ```\n  - [ ] x\n  ```\n  - [ ] b\n    - d1\n- [ ] c\n  ```\n  - [ ] y\n  ```\n  - [ ] d\n    - d2",
    "4-space nested fence in loose item + multi-line subtask":
      "- [ ] Set up\n\n    ```\n    - [ ] echo\n    ```\n\n  - [ ] Deploy\n    - detail",
    "tab-indented fence in item + multi-line subtask":
      "- [ ] Set up\n\t```\n\t- [ ] echo\n\t```\n  - [ ] Deploy\n    - detail",
    "very deeply nested (4 levels)": "- [ ] a\n  - [ ] b\n    - [ ] c\n      - [x] d",
  };

  for (const [name, body] of Object.entries(trickyBodies)) {
    it(`ordinals stay aligned with source lines: ${name}`, () => {
      const before = renderedCheckboxStates(renderMarkdown(body));
      // Rendered ordinals must be a contiguous 0..n-1 document-order sequence.
      expect(renderedOrdinals(renderMarkdown(body))).toEqual(before.map((_, i) => i));
      // The source-line map must have exactly one entry per rendered checkbox.
      expect(taskSourceLines(body)).toHaveLength(before.length);

      for (let ord = 0; ord < before.length; ord++) {
        const flipped = toggleTaskLine(body, ord);
        const after = renderedCheckboxStates(renderMarkdown(flipped));
        // No checkbox appears/disappears (we never flip a code/non-task line).
        expect(after).toHaveLength(before.length);
        for (let i = 0; i < before.length; i++) {
          // Only the addressed ordinal's checkbox changes; all others are stable.
          expect(after[i]).toBe(i === ord ? !before[i] : before[i]);
        }
      }
    });
  }
});

// Finding #3: a raw-HTML checkbox (with or without a forged data-task-ordinal)
// must NOT become a clickable, ordinal-bearing task checkbox, and must not shift
// the ordinals of real task items around it. Provenance is enforced at the
// sanitize boundary via a per-render nonce that body content cannot predict.
describe("renderMarkdown task-checkbox provenance (finding #3)", () => {
  it("strips a raw <input type=checkbox> with a forged data-task-ordinal", () => {
    const html = renderMarkdown(
      '- [ ] real\n\n<input type="checkbox" data-task-ordinal="0" checked onclick="alert(1)"> decoy',
    );
    // Exactly one checkbox survives — the real task item at ordinal 0.
    expect(renderedOrdinals(html)).toEqual([0]);
    expect(html).not.toMatch(/onclick/i);
  });

  it("strips a raw <input type=checkbox> with no ordinal (decoy neutralized)", () => {
    const html = renderMarkdown('<input type="checkbox"> decoy\n\n- [ ] real');
    const tpl = document.createElement("template");
    tpl.innerHTML = html;
    // The decoy must be REMOVED, not merely ordinal-less: only the real task's
    // checkbox may survive sanitization. (Counting inputs — not filtering on
    // data-task-ordinal — is what actually observes the strip.)
    expect(tpl.content.querySelectorAll("input")).toHaveLength(1);
    const survivor = tpl.content.querySelector("input");
    expect(survivor?.getAttribute("data-task-ordinal")).toBe("0");
    expect(renderedOrdinals(html)).toEqual([0]);
  });

  it("strips a malformed raw <input> with junk/unquoted attrs adjacent to a real task", () => {
    // marked escapes malformed inline HTML to text before DOMPurify runs; either
    // way the forged control must not survive as a clickable checkbox.
    const html = renderMarkdown('<input type=checkbox foo=bar baz> decoy\n\n- [ ] real');
    const tpl = document.createElement("template");
    tpl.innerHTML = html;
    // Only the real task's checkbox survives; the malformed decoy is gone.
    expect(tpl.content.querySelectorAll("input")).toHaveLength(1);
    expect(renderedOrdinals(html)).toEqual([0]);
  });

  it("mints a working (non-empty) nonce via the fallback when crypto.randomUUID is absent", () => {
    // freshNonce()'s fallback path (insecure context: no crypto.randomUUID).
    // If it returned "" the `renderNonce !== ""` provenance guard would strip
    // even OUR real checkboxes — so a surviving, ordinal-bearing box proves the
    // fallback produced a usable nonce.
    vi.stubGlobal("crypto", {}); // no randomUUID -> Date.now()+Math.random() path
    try {
      const html = renderMarkdown("- [ ] a\n- [x] b");
      expect(renderedOrdinals(html)).toEqual([0, 1]);
      expect(html).not.toContain("data-task-nonce"); // still stripped from output
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("a raw checkbox before real tasks does not shift their ordinals", () => {
    const html = renderMarkdown(
      '<input type="checkbox" data-task-ordinal="5"> decoy\n\n- [ ] a\n- [ ] b',
    );
    expect(renderedOrdinals(html)).toEqual([0, 1]);
  });

  it("does not leak the provenance nonce into the rendered DOM", () => {
    const html = renderMarkdown("- [ ] a\n- [x] b");
    expect(html).not.toContain("data-task-nonce");
  });
});

// Finding #5: the input-hardening hook is registered on a MODULE-SCOPED
// DOMPurify instance, not the shared global default export — so it never leaks
// onto other consumers or accumulates across module re-evaluation.
describe("DOMPurify isolation (finding #5)", () => {
  it("does not mutate the shared global DOMPurify singleton", () => {
    renderMarkdown("- [ ] ensure our scoped hook is active");
    // The GLOBAL default export must still keep a plain text input — proof our
    // input-stripping hook is scoped to the module instance only.
    const out = DOMPurify.sanitize('<input type="text" value="y">');
    expect(out).toContain("<input");
  });
});

describe("TAG_REGEX", () => {
  it("accepts valid tags", () => {
    expect(TAG_REGEX.test("auth")).toBe(true);
    expect(TAG_REGEX.test("new-tag")).toBe(true);
    expect(TAG_REGEX.test("a1")).toBe(true);
    expect(TAG_REGEX.test("my-long-tag-123")).toBe(true);
  });

  it("rejects invalid tags", () => {
    expect(TAG_REGEX.test("INVALID")).toBe(false);
    expect(TAG_REGEX.test("123")).toBe(false);
    expect(TAG_REGEX.test("-start")).toBe(false);
    expect(TAG_REGEX.test("end-")).toBe(false);
    expect(TAG_REGEX.test("has space")).toBe(false);
  });
});
