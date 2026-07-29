import { describe, it, expect } from "vitest";
import {
  EXISTENCE_TOKENS,
  REL_ID_FIELDS,
  REL_TOKEN_ORDER,
  recognizeRelationship,
} from "./relations";

// Three hand-maintained literals describe the same vocabulary from different angles:
// `REL_ID_FIELDS` + `EXISTENCE_TOKENS` are what `recognizeRelationship` accepts,
// while `REL_TOKEN_ORDER` is what `serialize.ts` emits and `complete.ts` offers.
// Nothing in the type system ties them together — the compile-time guard at the foot
// of `relations.ts` only checks that the ordered tokens' FIELDS are `QueryFilter`
// keys, not that every recognized SPELLING appears. These tests are the tie: adding a
// token to either half without the other fails here rather than shipping a menu entry
// the parser rejects, or a recognized token that never completes or serializes.
describe("relations — the token literals stay in sync", () => {
  const orderedIdNames = REL_TOKEN_ORDER.flatMap((t) => (t.kind === "id" ? [t.name] : []));
  const orderedBoolTokens = REL_TOKEN_ORDER.flatMap((t) => (t.kind === "bool" ? [t.token] : []));
  const sorted = (values: readonly string[]) => [...values].sort();

  it("orders exactly the recognized relationship-id field names", () => {
    expect(sorted(orderedIdNames)).toEqual(sorted(Object.keys(REL_ID_FIELDS)));
  });

  it("orders exactly the recognized existence tokens", () => {
    expect(sorted(orderedBoolTokens)).toEqual(sorted(Object.keys(EXISTENCE_TOKENS)));
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

  it("lists no name or token twice", () => {
    expect(new Set(orderedIdNames).size).toBe(orderedIdNames.length);
    expect(new Set(orderedBoolTokens).size).toBe(orderedBoolTokens.length);
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
