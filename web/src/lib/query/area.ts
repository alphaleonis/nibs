import type { AreaVocabulary } from "../areas";

// The ownership token, `area:<path>` — the query language's THIRD token kind.
//
// It is neither of the other two, and the differences are what keep it out of
// their tables rather than a matter of taste:
//
// - A metadata facet (`FIELD_SPECS`) pairs an include-list key with an
//   exclude-list key over a static, flat value set. `area` is a scalar,
//   `NibFilter` has no `excludeArea` to pair with, and its values are a
//   per-store tree that only exists at runtime.
// - A relationship token (`REL_TOKEN_ORDER`) is scalar and would fit that shape,
//   but its value is a NIB ID: completed by the async nib typeahead
//   (`relComplete.ts` reads `REL_ID_FIELDS`, so an entry there would aim the
//   typeahead at `area:`), and accepted on non-emptiness alone. An area path is
//   completed from the declared vocabulary and is checked against it.
//
// Downward closure — `area:web` also selecting `web/dashboard` — has no
// expression here at all, deliberately. It is the server's rule
// (`filterByAreaWithin`, internal/graph/filters.go); this token carries one
// path, and every token in this grammar means membership in what its value
// names. Widening the token to the subtree client-side would put the same rule
// in two places, over a vocabulary only one of them holds authoritatively.
//
// VALUES ARE NOT LOWERCASED, unlike every other token's. `config.GetArea`
// descends the declared tree comparing segment names byte-for-byte, so `Web` and
// `web` are different paths on the server and folding case here would send a
// value the store does not declare.

/** The token's field-name. */
export const AREA_FIELD = "area";

/** One line of prose for the in-UI syntax help. Kept beside the token rather
 *  than in `help.ts` for the reason the rel/metadata descriptions are: the help
 *  is generated from the vocabulary, so it cannot document a spelling the parser
 *  rejects. */
export const AREA_DESCRIPTION = "Nibs in this area, and in every area declared beneath it";

/** Recognition result: the path an `area:` token names, or a negated one the
 *  caller must park in its invalid-token sidecar. */
export type AreaMatch =
  | { kind: "area"; value: string }
  | { kind: "invalid"; token: string };

/**
 * Recognize a single token as an `area:` token, or `undefined` when it is not
 * one (the caller then routes it onward — to free text, in `parseQuery`).
 *
 * A leading `-` is rejected rather than ignored: there is no `excludeArea` for
 * it to write, and the token must not reach free text, where it becomes a Bleve
 * MUST-NOT clause over a field Bleve does not index (only id/slug/title/body
 * are, `internal/search/index.go`) — the clause excludes nothing and the query
 * degrades to match-all. `recognizeRelationship` parks negated rel tokens for
 * exactly this reason.
 *
 * The field-name is matched case-insensitively and normalized; the VALUE is
 * returned verbatim (see the note above). An empty value is not a token —
 * `area:` falls through to free text, as `type:` does.
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
 * Whether an `area:` value must be REFUSED — parked as invalid rather than
 * written to the filter.
 *
 * The one question both `parseQuery` (routing) and `tokenizeSpans` (coloring)
 * ask, so a value cannot parse as legal while rendering as an error;
 * `isValidValue` serves the metadata facets the same way.
 *
 * Only "undeclared" refuses. "unknown" — a vocabulary that has not arrived, one
 * whose config query failed, or (an absent `areas`) a caller with none to
 * consult — KEEPS the value, because the filter is rebuilt from localStorage and
 * `?q=` before the config query resolves: judging a token then would drop a
 * valid one at a moment the user did not act, and `Preferences.setQuery` is
 * precisely that caller. What keeps an unjudged value off the wire meanwhile is
 * `withSendableArea` (filter.ts), which re-asks at query time and sends only on
 * "declared".
 */
export function isRefusedArea(value: string, areas: AreaVocabulary | undefined): boolean {
  return areas?.validity(value) === "undeclared";
}
