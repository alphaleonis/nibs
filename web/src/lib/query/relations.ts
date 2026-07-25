import type { QueryFilter } from "./fields";

// Relationship + existence tokens (design 2.4, phase 5).
//
// These are a SEPARATE recognition step layered on top of the metadata grammar so
// the shared `FIELD_TOKEN` regex (and `spans.ts`, which reuses it) stays untouched.
// Two rel field-names are hyphenated (`blocked-by`, `mentioned-by`) which the
// `[A-Za-z]+` field group in `FIELD_TOKEN` cannot match, so recognition here does
// its own first-colon split instead of relying on that regex.
//
// - Relationship-id tokens (`blocking:<id>`, …) set a SCALAR string field to any
//   non-empty (lowercased) value — pattern-only, no existence validation.
// - Existence tokens (`has:parent`, `is:blocked`, …) set a BOOLEAN field to `true`.
//   The valid set is fixed and enumerated (parent/blocking/blocked-by + is:blocked);
//   anything else (`has:mentions`, `is:foo`) is NOT recognized and falls through to
//   free-text `search` in the caller.
// - Negation is a metadata-only feature: a leading `-` disqualifies the token from
//   rel/existence recognition (it routes to free text instead).

/** Scalar relationship-id fields, keyed by their token field-name. */
export type RelIdKey =
  | "parentId"
  | "blockingId"
  | "blockedById"
  | "mentionsId"
  | "mentionedById";

/** Boolean existence/state fields, keyed by their full token string. */
export type ExistenceKey =
  | "hasParent"
  | "noParent"
  | "hasBlocking"
  | "noBlocking"
  | "hasBlockedBy"
  | "noBlockedBy"
  | "isBlocked";

// Token field-name → scalar-id NibFilter key. Includes the hyphenated names.
const REL_ID_FIELDS: Record<string, RelIdKey> = {
  parent: "parentId",
  blocking: "blockingId",
  "blocked-by": "blockedById",
  mentions: "mentionsId",
  "mentioned-by": "mentionedById",
};

// Full (lowercased) existence token → boolean NibFilter key. Enumerated, so
// invalid combos (`has:mentions`, `no:mentions`, `is:foo`) simply are not present.
const EXISTENCE_TOKENS: Record<string, ExistenceKey> = {
  "has:parent": "hasParent",
  "no:parent": "noParent",
  "has:blocking": "hasBlocking",
  "no:blocking": "noBlocking",
  "has:blocked-by": "hasBlockedBy",
  "no:blocked-by": "noBlockedBy",
  "is:blocked": "isBlocked",
};

// Canonical serialization order for the rel/existence block (design 2.4). Grouped
// by relationship dimension — parent, blocking, blocked-by (+ is:blocked), mentions,
// mentioned-by — with each dimension's id token first, then its has/no existence
// tokens. This order is fixed so `serializeQuery(parseQuery(s)) === s` holds for any
// canonical string containing these tokens.
export type RelTokenSpec =
  | { kind: "id"; field: RelIdKey; name: string }
  | { kind: "bool"; field: ExistenceKey; token: string };

export const REL_TOKEN_ORDER: readonly RelTokenSpec[] = [
  { kind: "id", field: "parentId", name: "parent" },
  { kind: "bool", field: "hasParent", token: "has:parent" },
  { kind: "bool", field: "noParent", token: "no:parent" },
  { kind: "id", field: "blockingId", name: "blocking" },
  { kind: "bool", field: "hasBlocking", token: "has:blocking" },
  { kind: "bool", field: "noBlocking", token: "no:blocking" },
  { kind: "id", field: "blockedById", name: "blocked-by" },
  { kind: "bool", field: "hasBlockedBy", token: "has:blocked-by" },
  { kind: "bool", field: "noBlockedBy", token: "no:blocked-by" },
  { kind: "bool", field: "isBlocked", token: "is:blocked" },
  { kind: "id", field: "mentionsId", name: "mentions" },
  { kind: "id", field: "mentionedById", name: "mentioned-by" },
];

/** Recognition result: a scalar-id assignment or a boolean-existence assignment. */
export type RelMatch =
  | { kind: "id"; field: RelIdKey; value: string }
  | { kind: "bool"; field: ExistenceKey };

/**
 * Recognize a single token as a relationship-id or existence/state token, or
 * `undefined` when it is neither (the caller then routes it to free text).
 *
 * A leading `-` (negation) is not a rel/existence feature, so such tokens are
 * rejected here and fall through to free text. Field-names and id values are
 * lowercased, matching the rest of the query language.
 */
export function recognizeRelationship(token: string): RelMatch | undefined {
  if (token.startsWith("-")) return undefined;

  const lower = token.toLowerCase();
  const boolField = EXISTENCE_TOKENS[lower];
  if (boolField) return { kind: "bool", field: boolField };

  // Split on the FIRST colon so hyphenated field-names (`blocked-by`) are handled
  // without the metadata FIELD_TOKEN regex. Value is everything after it, taken
  // whole (scalar — no comma split), and only accepted when non-empty.
  const colon = token.indexOf(":");
  if (colon <= 0) return undefined;
  const name = token.slice(0, colon).toLowerCase();
  const value = token.slice(colon + 1).toLowerCase();
  const idField = REL_ID_FIELDS[name];
  if (idField && value !== "") return { kind: "id", field: idField, value };

  return undefined;
}

// Compile-time guard: every ordered token's field must be a key the box owns
// (QueryFilter). If QueryFilter loses one of these keys, this fails to typecheck.
type _RelKeysAreQueryFilterKeys = (RelIdKey | ExistenceKey) extends keyof QueryFilter ? true : never;
const _relKeysCheck: _RelKeysAreQueryFilterKeys = true;
void _relKeysCheck;
