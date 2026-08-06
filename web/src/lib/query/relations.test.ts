import { describe, it, expect } from "vitest";
import {
  EXISTENCE_TOKENS,
  REL_ID_FIELDS,
  REL_TOKEN_ORDER,
  clearHierarchyFilters,
  contradictionTokens,
  hierarchyTokens,
  recognizeRelationship,
} from "./relations";

// `REL_TOKEN_ORDER` is the single source of truth for the rel/existence vocabulary:
// `serialize.ts` emits from it, `complete.ts` offers from it, and the two recognition
// lookups (`REL_ID_FIELDS`, `EXISTENCE_TOKENS`) are derived from it. Set parity
// between the three is therefore true by construction and is no longer asserted here
// — the compile-time guards at the foot of `relations.ts` cover the other direction
// (a field dropped from the array is a type error, not a silently dropped token).
//
// What is left for runtime is what the type system cannot see: that each ordered
// entry actually round-trips through `recognizeRelationship`, that the derivation
// keys each map the way its lookup site expects, and that no two entries collide.
describe("relations — the ordered vocabulary drives recognition", () => {
  const orderedIdNames = REL_TOKEN_ORDER.flatMap((t) => (t.kind === "id" ? [t.name] : []));
  const orderedBoolTokens = REL_TOKEN_ORDER.flatMap((t) => (t.kind === "bool" ? [t.token] : []));

  it("agrees with the recognizer on the field each relationship-id name writes", () => {
    for (const spec of REL_TOKEN_ORDER) {
      if (spec.kind !== "id") continue;
      expect(recognizeRelationship(`${spec.name}:tnib-1`)).toEqual({
        kind: "id",
        field: spec.field,
        value: "tnib-1",
      });
    }
  });

  it("agrees with the recognizer on the field and value each existence token writes", () => {
    for (const spec of REL_TOKEN_ORDER) {
      if (spec.kind !== "bool") continue;
      expect(recognizeRelationship(spec.token)).toEqual({
        kind: "bool",
        field: spec.field,
        value: spec.value,
      });
    }
  });

  // The two derived maps are keyed differently on purpose, and `matchToken` looks
  // each up its own way: the whole token in `EXISTENCE_TOKENS` first, then the
  // pre-colon half in `REL_ID_FIELDS`. Keying either the other way would make its
  // lookups miss silently, so pin both shapes.
  it("keys REL_ID_FIELDS by the bare field-name and EXISTENCE_TOKENS by the full token", () => {
    expect(REL_ID_FIELDS.get("blocked-by")).toBe("blockedById");
    expect(REL_ID_FIELDS.get("has:parent")).toBeUndefined();
    expect(EXISTENCE_TOKENS.get("has:blocked-by")).toEqual({ field: "hasBlockedBy", value: true });
    expect(EXISTENCE_TOKENS.get("blocked-by")).toBeUndefined();
  });

  // Both lookups must be `Map`s. A plain object inherits `Object.prototype`, so
  // `REL_ID_FIELDS["constructor"]` returns the Object constructor and reads as a
  // hit: `constructor:foo` parsed as a relationship token whose field was that
  // function, and a bare `constructor` was swallowed out of free-text search —
  // a real word to lose in a tracker for a codebase.
  it("does not treat inherited Object.prototype members as vocabulary", () => {
    for (const inherited of ["constructor", "__proto__", "toString", "valueOf", "hasOwnProperty"]) {
      expect(REL_ID_FIELDS.get(inherited)).toBeUndefined();
      expect(EXISTENCE_TOKENS.get(inherited)).toBeUndefined();
      expect(recognizeRelationship(`${inherited}:foo`)).toBeUndefined();
      expect(recognizeRelationship(inherited)).toBeUndefined();
    }
  });

  // A duplicate name or token collapses in the derivation — the later entry wins
  // the lookup while both still serialize, which would emit a token the box then
  // re-parses as something else.
  it("lists no name or token twice", () => {
    expect(new Set(orderedIdNames).size).toBe(orderedIdNames.length);
    expect(new Set(orderedBoolTokens).size).toBe(orderedBoolTokens.length);
    expect(REL_ID_FIELDS.size).toBe(orderedIdNames.length);
    expect(EXISTENCE_TOKENS.size).toBe(orderedBoolTokens.length);
  });

  // `complete.ts` derives the completable existence words by splitting each token on
  // its first colon; a token without one silently yields a truncated word.
  it("spells every existence token as `word:value` with a single colon", () => {
    for (const token of orderedBoolTokens) {
      expect(token.split(":")).toHaveLength(2);
      expect(token.split(":").every((part) => part !== "")).toBe(true);
    }
  });
});

// The hierarchy subset: what the empty-result explanation names, and what its
// escape hatch removes. Both walk the same vocabulary the box parses and
// serializes, so the tokens shown are spelled the way the user could retype them.
describe("relations — the hierarchy subset", () => {
  it("names each active hierarchy filter with its canonical token, in vocabulary order", () => {
    expect(hierarchyTokens({ descendantId: "tnib-2", ancestorId: "tnib-1" })).toEqual([
      "ancestor:tnib-1",
      "descendant:tnib-2",
    ]);
    expect(hierarchyTokens({ siblingId: "tnib-4", parentId: "tnib-3" })).toEqual([
      "parent:tnib-3",
      "sibling:tnib-4",
    ]);
  });

  // `hasParent` is tri-state: the `no:` spelling is a set value, not an absent one,
  // and it constrains tree position exactly as the id tokens do.
  it("emits the spelling that matches the parent-existence value", () => {
    expect(hierarchyTokens({ hasParent: false, ancestorId: "tnib-1" })).toEqual([
      "no:parent",
      "ancestor:tnib-1",
    ]);
    expect(hierarchyTokens({ hasParent: true })).toEqual(["has:parent"]);
  });

  it("ignores relationship fields outside the hierarchy dimension", () => {
    expect(
      hierarchyTokens({
        blockingId: "tnib-8",
        blockedById: "tnib-9",
        mentionsId: "tnib-7",
        isBlocked: true,
        type: ["bug"],
        search: "login",
      }),
    ).toEqual([]);
  });

  it("removes every hierarchy field and leaves the rest of the filter untouched", () => {
    expect(
      clearHierarchyFilters({
        parentId: "tnib-1",
        ancestorId: "tnib-2",
        descendantId: "tnib-3",
        siblingId: "tnib-4",
        hasParent: false,
        blockingId: "tnib-8",
        type: ["bug"],
        search: "login",
      }),
    ).toEqual({ blockingId: "tnib-8", type: ["bug"], search: "login" });
  });

  it("does not mutate the filter it clears", () => {
    const filter = { ancestorId: "tnib-1", type: ["bug"] };
    clearHierarchyFilters(filter);
    expect(filter).toEqual({ ancestorId: "tnib-1", type: ["bug"] });
  });
});

// The pairs the server refuses as unanswerable. The table lives here rather than
// in the component so the refusal is explained in the same spellings the box
// parses and serializes — the user can only edit what the box shows.
describe("relations — contradictory pairs", () => {
  it("names each refused pair in canonical token spelling", () => {
    expect(contradictionTokens({ parentId: "tnib-1", hasParent: false })).toEqual([
      ["parent:tnib-1", "no:parent"],
    ]);
    expect(contradictionTokens({ blockedById: "tnib-9", hasBlockedBy: false })).toEqual([
      ["blocked-by:tnib-9", "no:blocked-by"],
    ]);
  });

  it("names both pairs when both are set", () => {
    expect(
      contradictionTokens({
        parentId: "tnib-1",
        hasParent: false,
        blockedById: "tnib-9",
        hasBlockedBy: false,
      }),
    ).toEqual([
      ["parent:tnib-1", "no:parent"],
      ["blocked-by:tnib-9", "no:blocked-by"],
    ]);
  });

  // Only the `false` half contradicts. `has:parent` alongside `parent:<id>` is
  // redundant, which the server answers rather than refuses, and an unset
  // existence field constrains nothing at all.
  it("reports nothing for a redundant or unset existence half", () => {
    expect(contradictionTokens({ parentId: "tnib-1", hasParent: true })).toEqual([]);
    expect(contradictionTokens({ parentId: "tnib-1" })).toEqual([]);
    expect(contradictionTokens({ hasParent: false })).toEqual([]);
  });

  // `blocking:<id>` selects membership in the target's blocked_by whatever the
  // candidate's status, while `no:blocking` selects the nibs not ACTIVELY blocking
  // anything — so the pair asks for the blockers the target still lists that block
  // nothing, which the server answers. Calling it a contradiction here would
  // explain away a working query.
  it("does not treat the blocking pair as a contradiction", () => {
    expect(contradictionTokens({ blockingId: "tnib-8", hasBlocking: false })).toEqual([]);
  });
});
