import { describe, it, expect } from "vitest";
import {
  NoUnusedVariablesRule,
  buildSchema,
  parse,
  print,
  validate,
  type DocumentNode,
} from "graphql";
import { readFileSync } from "node:fs";
import * as queries from "./queries";
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
  //
  // Only the DECLARATION is asserted. The general unused-variable guard below
  // owns the other half — a `$scope` declared but never forwarded — and cannot
  // see this one, because a variable that was never declared is never unused.
  // Measured in both directions rather than reasoned: dropping only the
  // argument reddens the general guard alone, dropping the declaration too
  // reddens only this test.
  it("declares $scope, which the server's default would otherwise silently replace", () => {
    const source = print(REORDER_NIB_MUTATION);

    const signature = source.match(/mutation\s+ReorderNib\s*\(([^)]*)\)/)?.[1] ?? "";
    expect(signature).not.toBe("");
    expect(signature).toMatch(/\$scope:\s*OrderScope\b/);
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

// --- Every document: no declared variable left unforwarded ---
//
// gqlgen validates with gqlparser's FULL default rule set, so a document that
// declares `$x` and never passes it to a field argument is refused outright —
// `Variable "$x" is never used` — failing every caller of that document, not
// just the one argument. Re-applied here because nothing else client-side sees
// it: @graphql-codegen/core hardcodes NoUnusedVariables out of its rule set
// (the list is push-only, so no config re-enables it), the generated
// `…Variables` type derives from the declarations rather than from what
// consumes them, and gql.ts keys its documents map on the raw source literal,
// so the overload tracks the defect instead of detecting it.
describe("every exported GraphQL document", () => {
  // Same path spelling codegen.ts uses, resolved the same way: relative to
  // web/, which is where both the codegen run and the canonical vitest command
  // start (resolve.alias in vitest.config.ts already resolves from cwd). One
  // schema location, so the guard cannot validate against a different SDL than
  // the one the documents were generated from. A wrong cwd is an ENOENT naming
  // the path, not a silent skip.
  const schema = buildSchema(readFileSync("../internal/graph/schema.graphqls", "utf8"));

  const documents = Object.entries(queries).filter(
    (entry): entry is [string, DocumentNode] =>
      typeof entry[1] === "object" && entry[1] !== null && (entry[1] as DocumentNode).kind === "Document",
  );

  // A rule applied to nothing passes silently, and this suite's whole subject is
  // the document set — so an empty walk is a failure, not a vacuous pass.
  it("finds documents to check", () => {
    expect(documents.length).toBeGreaterThan(0);
  });

  it.each(documents)("%s forwards every variable it declares", (_name, document) => {
    // Re-parse the printed source rather than validating the generated AST
    // directly: TypedDocumentNodes carry no location info, and the round trip
    // restores it so a failure names the line.
    const errors = validate(schema, parse(print(document)), [NoUnusedVariablesRule]);

    expect(errors.map(e => e.message)).toEqual([]);
  });
});
