import { describe, it, expect } from "vitest";
import {
  EXISTENCE_TOKENS,
  REL_ID_FIELDS,
  REL_TOKEN_ORDER,
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
