import { describe, it, expect } from "vitest";
import { print } from "graphql";
import { NIB_DETAIL_QUERY, NIB_CONFLICT_SNAPSHOT_QUERY } from "./queries";

// The queries are now generated TypedDocumentNodes (graphql-codegen client
// preset), which serialize the AST WITHOUT location info — so `.loc` is
// undefined. Reconstruct the query text from the AST via graphql's `print`
// instead; it yields the same selections these guards assert against.

describe("NIB_DETAIL_QUERY", () => {
  it("requests the mentions relationship with id and title fields", () => {
    const source = print(NIB_DETAIL_QUERY);
    // Tolerate formatting variations (`mentions {`, `mentions  {`, `mentions\n{`).
    expect(source).toMatch(/\bmentions\s*\{/);
    // Pin at least the load-bearing sub-selections.
    expect(source).toMatch(/\bmentions\s*\{[^}]*\bid\b/);
    expect(source).toMatch(/\bmentions\s*\{[^}]*\btitle\b/);
  });

  it("requests the mentionedBy relationship with id and title fields", () => {
    const source = print(NIB_DETAIL_QUERY);
    expect(source).toMatch(/\bmentionedBy\s*\{/);
    expect(source).toMatch(/\bmentionedBy\s*\{[^}]*\bid\b/);
    expect(source).toMatch(/\bmentionedBy\s*\{[^}]*\btitle\b/);
  });
});

describe("NIB_CONFLICT_SNAPSHOT_QUERY", () => {
  // The null-remote conflict fallback MUST use a document
  // distinct from NIB_DETAIL_QUERY so urql does not share its result-source with
  // App's live detailStore — otherwise a `{ nib: null }` (deleted-in-race) reply
  // would trip the missing-nib effect and drop the dirty buffer (MEDIUM #3).
  it("is a distinct operation (different name) from NIB_DETAIL_QUERY", () => {
    const conflict = print(NIB_CONFLICT_SNAPSHOT_QUERY);
    const detail = print(NIB_DETAIL_QUERY);
    expect(conflict).toMatch(/\bquery\s+NibConflictSnapshot\b/);
    expect(detail).not.toMatch(/\bquery\s+NibConflictSnapshot\b/);
    // Distinct query text is what gives urql a separate operation key.
    expect(conflict).not.toBe(detail);
  });

  it("selects every field toNibSnapshot reads", () => {
    const source = print(NIB_CONFLICT_SNAPSHOT_QUERY);
    // Assert against the nib { ... } SELECTION SET, not the whole document — else
    // `id` is satisfied vacuously by the `$id` signature / `nib(id: $id)` even if
    // dropped from the selection (MEDIUM #2). The lean query has no nested
    // sub-selections, so the selection body is a flat scalar list.
    const selection = source.match(/nib\s*\([^)]*\)\s*\{([\s\S]*)\}/)?.[1] ?? "";
    expect(selection).not.toBe("");
    // Keep in lockstep with toNibSnapshot (nibChange.ts).
    for (const field of ["id", "title", "status", "type", "priority", "estimate", "tags", "body", "etag"]) {
      expect(selection).toMatch(new RegExp(`\\b${field}\\b`));
    }
  });

  it("stays lean — omits the relationship sub-selections detailStore renders", () => {
    const source = print(NIB_CONFLICT_SNAPSHOT_QUERY);
    // Not needed to build a NibSnapshot; their absence keeps this document a
    // strict subset of NIB_DETAIL_QUERY (and thus a separate operation).
    expect(source).not.toMatch(/\bchildren\b/);
    expect(source).not.toMatch(/\bblockedBy\b/);
    expect(source).not.toMatch(/\bmentions\b/);
  });
});
