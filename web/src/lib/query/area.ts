import type { AreaVocabulary } from "../areas";

// The ownership token, `area:<path>`, kept out of the other two token tables:
//
// - Not a metadata facet (`FIELD_SPECS`): it is scalar, `NibFilter` has no
//   `excludeArea`, and its values are a per-store tree known only at runtime.
// - Not a relationship token (`REL_TOKEN_ORDER`): `relComplete.ts` aims the nib-id
//   typeahead at every name in `REL_ID_FIELDS`.
//
// The server makes `area:web` match `web/dashboard` too (`filterByAreaWithin`,
// internal/graph/filters.go); do not widen the value client-side.
//
// Values are NOT lowercased: the server compares path segments case-sensitively
// (`Areas.Get`, internal/config/areas.go).

export const AREA_FIELD = "area";

/** Help-panel prose, beside the token because the help is generated from the
 *  vocabulary. */
export const AREA_DESCRIPTION = "Nibs in this area, and in every area declared beneath it";

/** The path an `area:` token names, or a negated token for the caller to park. */
export type AreaMatch =
  | { kind: "area"; value: string }
  | { kind: "invalid"; token: string };

/**
 * Recognize `token` as an `area:` token, or return `undefined`. The name is
 * case-insensitive, the value is returned verbatim, and `area:` with no value is
 * not a token.
 *
 * A negated token returns `invalid`: there is no `excludeArea`, and as free text
 * it excludes nothing, for the reason `recognizeRelationship` gives.
 */
export function recognizeArea(token: string): AreaMatch | undefined {
  const negated = token.startsWith("-");
  const body = negated ? token.slice(1) : token;
  const colon = body.indexOf(":");
  if (colon <= 0) return undefined;
  if (body.slice(0, colon).toLowerCase() !== AREA_FIELD) return undefined;
  const value = body.slice(colon + 1);
  if (value === "") return undefined;
  return negated ? { kind: "invalid", token: `-${AREA_FIELD}:${value}` } : { kind: "area", value };
}

/**
 * Whether an `area:` value is parked as invalid instead of written to the filter.
 * `parseQuery` and `tokenizeSpans` both ask it.
 *
 * Only "undeclared" refuses. "unknown" — loading, unavailable, or no `areas`, as
 * from `Preferences.setQuery` — keeps the value, because a restored filter is
 * parsed before the config query resolves. `withSendableArea` (filter.ts) keeps
 * an unchecked value off the wire.
 */
export function isRefusedArea(value: string, areas: AreaVocabulary | undefined): boolean {
  return areas?.validity(value) === "undeclared";
}
