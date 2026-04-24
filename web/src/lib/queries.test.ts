import { describe, it, expect } from "vitest";
import { NIB_DETAIL_QUERY } from "./queries";

describe("NIB_DETAIL_QUERY", () => {
  it("requests the mentions relationship with id and title fields", () => {
    const source = NIB_DETAIL_QUERY.loc?.source.body ?? "";
    // Tolerate formatting variations (`mentions {`, `mentions  {`, `mentions\n{`).
    expect(source).toMatch(/\bmentions\s*\{/);
    // Pin at least the load-bearing sub-selections.
    expect(source).toMatch(/\bmentions\s*\{[^}]*\bid\b/);
    expect(source).toMatch(/\bmentions\s*\{[^}]*\btitle\b/);
  });

  it("requests the mentionedBy relationship with id and title fields", () => {
    const source = NIB_DETAIL_QUERY.loc?.source.body ?? "";
    expect(source).toMatch(/\bmentionedBy\s*\{/);
    expect(source).toMatch(/\bmentionedBy\s*\{[^}]*\bid\b/);
    expect(source).toMatch(/\bmentionedBy\s*\{[^}]*\btitle\b/);
  });
});
