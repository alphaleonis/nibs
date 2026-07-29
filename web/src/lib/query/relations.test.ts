import { describe, it, expect } from "vitest";
import {
  EXISTENCE_TOKENS,
  REL_ID_FIELDS,
  REL_TOKEN_ORDER,
  clearHierarchyFilters,
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
    expect(REL_ID_FIELDS["blocked-by"]).toBe("blockedById");
    expect(REL_ID_FIELDS["has:parent"]).toBeUndefined();
    expect(EXISTENCE_TOKENS["has:blocked-by"]).toEqual({ field: "hasBlockedBy", value: true });
    expect(EXISTENCE_TOKENS["blocked-by"]).toBeUndefined();
  });

  // A duplicate name or token collapses in the `Object.fromEntries` derivation —
  // the later entry wins the lookup while both still serialize, which would emit a
  // token the box then re-parses as something else.
  it("lists no name or token twice", () => {
    expect(new Set(orderedIdNames).size).toBe(orderedIdNames.length);
    expect(new Set(orderedBoolTokens).size).toBe(orderedBoolTokens.length);
    expect(Object.keys(REL_ID_FIELDS)).toHaveLength(orderedIdNames.length);
    expect(Object.keys(EXISTENCE_TOKENS)).toHaveLength(orderedBoolTokens.length);
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
