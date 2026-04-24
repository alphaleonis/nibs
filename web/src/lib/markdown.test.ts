import { describe, it, expect } from "vitest";
import { renderMarkdown, TAG_REGEX } from "./markdown";

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
