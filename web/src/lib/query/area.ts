import type { AreaVocabulary } from "../areas";

// The ownership tokens, `area:<path>` and `area-exact:<path>`, kept out of the
// other two token tables:
//
// - Not metadata facets (`FIELD_SPECS`): they are scalar, `NibFilter` has no
//   `excludeArea`, and their values are a per-store tree known only at runtime.
// - Not relationship tokens (`REL_TOKEN_ORDER`): `relComplete.ts` aims the nib-id
//   typeahead at every name in `REL_ID_FIELDS`.
//
// The server makes `area:web` match `web/dashboard` too and `areaExact: "web"`
// match `web` alone (`filterByAreaWithin` and the areaExact branch,
// internal/graph/filters.go); do not widen either value client-side.
//
// Values are NOT lowercased: the server compares path segments case-sensitively
// (`area.Vocabulary.Get`).

/** One ownership token: its field-name in the box, and the `NibFilter` key it
 *  writes. */
export interface AreaTokenSpec {
  /** The token field-name, hyphenated ones included (`area-exact`). */
  name: string;
  /** The scalar `NibFilter` key this token assigns. */
  field: "area" | "areaExact";
  /** One line for the in-UI syntax help, beside the token because the help is
   *  generated from the vocabulary. */
  description: string;
}

/** The ownership vocabulary in canonical serialization order, the broad reading
 *  first. `as const` keeps the literal `field` types the guards below read. */
export const AREA_TOKEN_ORDER = [
  {
    name: "area",
    field: "area",
    description: "Nibs in this area, and in every area declared beneath it",
  },
  {
    name: "area-exact",
    field: "areaExact",
    description: "Nibs in exactly this area, and in nothing declared beneath it",
  },
] as const satisfies readonly AreaTokenSpec[];

/** The scalar `NibFilter` keys the ownership tokens write. */
export type AreaKey = (typeof AREA_TOKEN_ORDER)[number]["field"];

// Token field-name → its spec. A Map, not an object: an object lookup finds
// inherited members, so `constructor:foo` would be recognized.
const AREA_SPEC_BY_NAME: ReadonlyMap<string, AreaTokenSpec> = new Map(
  AREA_TOKEN_ORDER.map((spec) => [spec.name, spec as AreaTokenSpec]),
);

/** The path an ownership token names, or a negated token for the caller to park. */
export type AreaMatch =
  | { kind: "area"; field: AreaKey; name: string; value: string }
  | { kind: "invalid"; token: string };

/**
 * Recognize `token` as one of the ownership tokens, or return `undefined`. The
 * name is case-insensitive, the value is returned verbatim, and a name with no
 * value is not a token.
 *
 * A negated token returns `invalid`: there is no `excludeArea`, and as free text
 * it excludes nothing, for the reason `recognizeRelationship` gives.
 */
export function recognizeArea(token: string): AreaMatch | undefined {
  const negated = token.startsWith("-");
  const body = negated ? token.slice(1) : token;
  const colon = body.indexOf(":");
  if (colon <= 0) return undefined;
  const spec = AREA_SPEC_BY_NAME.get(body.slice(0, colon).toLowerCase());
  if (!spec) return undefined;
  const value = body.slice(colon + 1);
  if (value === "") return undefined;
  return negated
    ? { kind: "invalid", token: `-${spec.name}:${value}` }
    : { kind: "area", field: spec.field, name: spec.name, value };
}

/**
 * Whether an ownership value is parked as invalid instead of written to the
 * filter. `parseQuery` and `tokenizeSpans` both ask it.
 *
 * Only "undeclared" refuses. "unknown" — loading, unavailable, or no `areas`, as
 * from `Preferences.setQuery` — keeps the value, because a restored filter is
 * parsed before the config query resolves. `withSendableAreas` (filter.ts) keeps
 * an unchecked value off the wire.
 *
 * Both tokens ask the same question: the server refuses an undeclared path on
 * either field, so the closure plays no part in it.
 */
export function isRefusedArea(value: string, areas: AreaVocabulary | undefined): boolean {
  return areas?.validity(value) === "undeclared";
}
