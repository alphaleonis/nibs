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
