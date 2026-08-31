import { describe, it, expect } from "vitest";
import { print } from "graphql";
import {
  NIB_DETAIL_QUERY,
  NIB_CONFLICT_SNAPSHOT_QUERY,
  TREE_TABLE_QUERY,
  REORDER_NIB_MUTATION,
} from "./queries";

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

describe("TREE_TABLE_QUERY", () => {
  // The assignment axis a membership lens groups by. A grouping lens reads it
  // off the row it was handed, so an unselected field is a lens that silently
  // sorts every nib into the same section — a wrong table rather than a broken
  // one. Named here so the failure says which selection went missing.
  it("selects the milestone assignment fields a membership lens groups by", () => {
    const source = print(TREE_TABLE_QUERY);
    // Assert against the `nibs(...)` SELECTION SET, not the whole document, so
    // a field name appearing only in the argument list cannot satisfy it. Each
    // pattern is line-anchored so `milestone` is not satisfied by the
    // `milestoneOrder` line.
    const selection = source.match(/nibs\s*\([^)]*\)\s*\{([\s\S]*)\}/)?.[1] ?? "";
    expect(selection).not.toBe("");
    for (const field of ["milestone", "milestoneOrder"]) {
      expect(selection).toMatch(new RegExp(`^\\s*${field}\\s*$`, "m"));
    }
  });
});

describe("REORDER_NIB_MUTATION", () => {
  // The ordering axis is a wire argument, not a client-side notion: a document
  // that declares no `$scope` can only ever reach the server's PARENT default,
  // so a milestone-queue move would silently land on the sibling order key.
  it("declares $scope and passes it to the reorderNib field", () => {
    const source = print(REORDER_NIB_MUTATION);

    // The operation signature must declare the variable...
    const signature = source.match(/mutation\s+ReorderNib\s*\(([^)]*)\)/)?.[1] ?? "";
    expect(signature).not.toBe("");
    expect(signature).toMatch(/\$scope:\s*OrderScope\b/);

    // ...and the field must actually forward it. The two assertions guard two
    // different failures, so neither is redundant. Declaring no `$scope` at all
    // reaches only the server's PARENT default: a milestone move then rewrites
    // the sibling `order` key and says nothing. Declaring it but not forwarding
    // it is refused outright — `Variable "$scope" is never used` — and since
    // this is the only reorder document in the app, that breaks every reorder.
    // Nothing upstream catches either: graphql-codegen strips
    // NoUnusedVariables from its rule set (the server does not), and the
    // generated Variables type is derived from the declarations, so a caller
    // passing `scope` type-checks in both half-states.
    const args = source.match(/reorderNib\s*\(([\s\S]*?)\)\s*\{/)?.[1] ?? "";
    expect(args).not.toBe("");
    expect(args).toMatch(/\bscope:\s*\$scope\b/);
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
