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
//   anything else (`has:mentions`, `has:ancestor`, `is:foo`) is NOT recognized and
//   falls through to free-text `search` in the caller. Existence spellings exist
//   only where the server has a matching predicate, which is why the hierarchy
//   tokens beyond `parent` have none.
// - Negation is a metadata-only feature: a leading `-` disqualifies the token from
//   rel/existence recognition. Such a token is PARKED as invalid rather than routed
//   to free text — see `recognizeRelationship` for why free text is unsafe here.
//
// `REL_TOKEN_ORDER` below is the single source of truth for the whole vocabulary:
// the two recognition lookups are derived from it, and compile-time guards at the
// foot of this file make a missing entry a type error. Before that, the spellings
// lived in three hand-maintained literals, and a token present in recognition but
// absent from the ordered array was silently DROPPED on every canonicalization —
// box blur, `localStorage`, and the `?q=` URL param all round-trip through
// `serializeQuery`, which walks that array to decide what to emit.

/** Scalar relationship-id fields, keyed by their token field-name. */
export type RelIdKey =
  | "parentId"
  | "ancestorId"
  | "descendantId"
  | "siblingId"
  | "blockingId"
  | "blockedById"
  | "mentionsId"
  | "mentionedById";

/** The existence dimensions that carry BOTH a `has:` and a `no:` spelling. Split
 *  out of `ExistenceKey` (not duplicated from it) so a guard below can require
 *  both halves — `is:blocked` has no `no:` twin and must not be held to that. */
type PairedExistenceKey = "hasParent" | "hasBlocking" | "hasBlockedBy";

/** Tri-state existence/state fields. Each is one field with two token
 *  spellings: `has:parent` writes true, `no:parent` writes false. The backend
 *  filter collapsed the `no*` twins into these same fields, so the grammar keeps
 *  both words while the model holds one value. */
export type ExistenceKey = PairedExistenceKey | "isBlocked";

/** One entry of the rel/existence vocabulary: a relationship-id token
 *  (`parent:<id>`), or one spelling of an existence token (`has:parent`). */
export type RelTokenSpec =
  | {
      kind: "id";
      field: RelIdKey;
      /** The token field-name, including the hyphenated ones (`blocked-by`).
       *
       *  Each name states the relationship the MATCHED nib holds toward the supplied
       *  id, never the reverse — so `ancestor:X` keeps nibs whose ancestor is X (X's
       *  descendants at any depth) and `descendant:X` keeps nibs whose descendant is
       *  X (X's ancestor chain). This mirrors the server `NibFilter` fields exactly. */
      name: string;
      /** One line of prose for the in-UI syntax help, phrased from the matched
       *  nib's side to match `name`'s semantics. Required, so a token cannot enter
       *  the vocabulary undocumented — the help panel is generated from this array
       *  rather than hand-listed beside it. */
      description: string;
    }
  | {
      kind: "bool";
      field: ExistenceKey;
      /** The full existence token, and it must be exactly `word:value` with a single
       *  colon: `complete.ts` splits on the first colon to derive the completable
       *  existence words and the values each accepts. A colon-less token would put a
       *  truncated word in the completion menu and insert something the parser
       *  rejects. `relations.test.ts` pins the shape of every entry. */
      token: string;
      value: boolean;
      /** One line of prose for the in-UI syntax help. Required for the same reason
       *  as the id variant's. */
      description: string;
    };

// Canonical serialization order for the rel/existence block, and the source the
// recognition lookups below are built from. Grouped by relationship dimension —
// hierarchy (parent + ancestor, descendant, sibling), blocking, blocked-by
// (+ is:blocked), mentions, mentioned-by — with each dimension's id token first,
// then its has/no existence tokens. This order is fixed so
// `serializeQuery(parseQuery(s)) === s` holds for any canonical string containing
// these tokens; moving an entry silently changes what counts as canonical.
//
// `as const` is load-bearing: it keeps each entry's literal types, which is what
// lets the exhaustiveness guards at the foot of this file see WHICH fields the
// array covers. `satisfies` still checks every entry against `RelTokenSpec`, so a
// misspelled field name remains a compile error.
export const REL_TOKEN_ORDER = [
  { kind: "id", field: "parentId", name: "parent", description: "Direct children of this nib" },
  { kind: "bool", field: "hasParent", token: "has:parent", value: true, description: "Nibs that have a parent" },
  { kind: "bool", field: "hasParent", token: "no:parent", value: false, description: "Root nibs, with no parent" },
  // The rest of the hierarchy dimension, kept adjacent to parent so the tree
  // questions read together. None of these has a has/no spelling — the server has
  // no matching existence predicate, and the grammar only offers what it can answer.
  { kind: "id", field: "ancestorId", name: "ancestor", description: "Everything under this nib, at any depth" },
  { kind: "id", field: "descendantId", name: "descendant", description: "This nib's ancestor chain, up to the root" },
  { kind: "id", field: "siblingId", name: "sibling", description: "Nibs sharing this nib's parent" },
  { kind: "id", field: "blockingId", name: "blocking", description: "Nibs that block this one" },
  { kind: "bool", field: "hasBlocking", token: "has:blocking", value: true, description: "Nibs that block something" },
  { kind: "bool", field: "hasBlocking", token: "no:blocking", value: false, description: "Nibs that block nothing" },
  { kind: "id", field: "blockedById", name: "blocked-by", description: "Nibs this one blocks" },
  { kind: "bool", field: "hasBlockedBy", token: "has:blocked-by", value: true, description: "Nibs listing a blocker" },
  { kind: "bool", field: "hasBlockedBy", token: "no:blocked-by", value: false, description: "Nibs listing no blocker" },
  { kind: "bool", field: "isBlocked", token: "is:blocked", value: true, description: "Nibs held up by an unmet blocker" },
  { kind: "id", field: "mentionsId", name: "mentions", description: "Nibs whose body mentions this nib" },
  { kind: "id", field: "mentionedById", name: "mentioned-by", description: "Nibs mentioned in this nib's body" },
] as const satisfies readonly RelTokenSpec[];

/** The literal-preserving entry type of `REL_TOKEN_ORDER`, narrowed to one kind.
 *  Reading `["field"]` off this yields exactly the fields the array covers, which
 *  is what the exhaustiveness guards compare the unions against. */
type OrderedSpec<K extends RelTokenSpec["kind"]> = Extract<
  (typeof REL_TOKEN_ORDER)[number],
  { kind: K }
>;

// Token field-name → scalar-id NibFilter key. Includes the hyphenated names.
// Keyed by the BARE field-name (`blocked-by`), because `matchToken` splits the
// token on its first colon and looks the left half up here.
//
// Derived from `REL_TOKEN_ORDER` rather than written out, so recognition accepts
// exactly what completion offers and serialization emits. Exported so the rel-token
// typeahead detector (relComplete.ts) recognizes the same field-names without
// duplicating the set.
// A `Map`, not a plain object, and the distinction is load-bearing: an object
// literal (and anything `Object.fromEntries` builds) inherits `Object.prototype`,
// so a lookup of `constructor` or `__proto__` returns an inherited member and
// reads as a hit. That made `constructor:foo` parse as a relationship token whose
// field was the `Object` constructor, and swallowed a bare `constructor` out of
// free-text search. `fields.ts` already keys its own lookup this way.
export const REL_ID_FIELDS: ReadonlyMap<string, RelIdKey> = new Map(
  REL_TOKEN_ORDER.flatMap((spec): [string, RelIdKey][] =>
    spec.kind === "id" ? [[spec.name, spec.field]] : [],
  ),
);

// Full (lowercased) existence token → the field it writes and the value it
// writes there. Keyed by the WHOLE token (`has:parent`), because `matchToken`
// tries the untouched token here before falling back to the colon split.
//
// Enumerated by `REL_TOKEN_ORDER`, so invalid combos (`has:mentions`, `no:mentions`,
// `is:foo`) simply are not present. The `has:`/`no:` pair for one dimension targets
// the SAME field with opposite values — writing them as two fields is what the
// backend filter model retired.
// A `Map` for the same prototype reason as `REL_ID_FIELDS` above.
export const EXISTENCE_TOKENS: ReadonlyMap<string, { field: ExistenceKey; value: boolean }> =
  new Map(
    REL_TOKEN_ORDER.flatMap((spec): [string, { field: ExistenceKey; value: boolean }][] =>
      spec.kind === "bool" ? [[spec.token, { field: spec.field, value: spec.value }]] : [],
    ),
  );

// --- The hierarchy subset ------------------------------------------------------

/** The fields that constrain a nib's position in the tree, as opposed to the
 *  blocking/mention dimensions. `REL_TOKEN_ORDER` covers ALL relationship fields,
 *  so this subset is named here — the guard below keeps it from drifting away from
 *  the vocabulary it selects from. */
export type HierarchyKey = "parentId" | "ancestorId" | "descendantId" | "siblingId" | "hasParent";

/** Membership set for the subset, used to filter `REL_TOKEN_ORDER`. `as const`
 *  keeps the literal types so the guard below can check the array against the
 *  union in both directions. */
const HIERARCHY_FIELDS = [
  "parentId",
  "ancestorId",
  "descendantId",
  "siblingId",
  "hasParent",
] as const satisfies readonly HierarchyKey[];

const HIERARCHY_FIELD_SET: ReadonlySet<string> = new Set(HIERARCHY_FIELDS);

/**
 * The canonical tokens for the hierarchy filters this filter has set, in
 * `REL_TOKEN_ORDER` order.
 *
 * Rendered from the vocabulary rather than formatted ad hoc, so what a
 * hierarchy-specific empty state shows the user is exactly what the box would
 * serialize and re-parse. `hasParent` emits whichever of its two spellings matches
 * the value, so an explicit `no:parent` is named rather than treated as unset.
 */
export function hierarchyTokens(filter: QueryFilter): string[] {
  const tokens: string[] = [];
  for (const spec of REL_TOKEN_ORDER) {
    if (!HIERARCHY_FIELD_SET.has(spec.field)) continue;
    if (spec.kind === "id") {
      const id = filter[spec.field];
      if (id) tokens.push(`${spec.name}:${id}`);
    } else if (filter[spec.field] === spec.value) {
      tokens.push(spec.token);
    }
  }
  return tokens;
}

/** A copy of `filter` with every hierarchy field removed and everything else — the
 *  metadata facets, free text, and the other relationship dimensions — kept. The
 *  escape hatch out of a hierarchy combination that matches nothing. */
export function clearHierarchyFilters<T extends QueryFilter>(filter: T): T {
  const next = { ...filter };
  for (const field of HIERARCHY_FIELDS) {
    delete next[field];
  }
  return next;
}

/** Recognition result: a scalar-id assignment, a boolean-existence assignment, or
 *  a rejected token the caller must park in its invalid-token sidecar. */
export type RelMatch =
  | { kind: "id"; field: RelIdKey; value: string }
  | { kind: "bool"; field: ExistenceKey; value: boolean }
  | { kind: "invalid"; token: string };

/**
 * Recognize a single token as a relationship-id or existence/state token, or
 * `undefined` when it is neither (the caller then routes it to free text).
 *
 * A leading `-` (negation) is not a rel/existence feature — there is no server
 * predicate for "not in this subtree". A negated token that would OTHERWISE be
 * recognized is returned as `invalid` so the caller parks it: it must not reach
 * free text, because free text is handed to the server's Bleve query string, where
 * `-ancestor:x` is valid MUST-NOT syntax over a field Bleve does not index (only
 * id/slug/title/body are). The clause then excludes nothing and the query silently
 * degrades to match-all — `-ancestor:<id>` returns the entire dataset, and in a
 * compound query like `type:bug -ancestor:<id>` the surviving `type` filter makes
 * the result look plausible. Parked, it is flagged in the box and round-trips
 * verbatim. A negated token that is NOT a rel/existence spelling (`-title:foo`,
 * `-has:mentions`) still falls to free text, where Bleve's syntax is the point.
 *
 * Field-names and id values are lowercased, matching the rest of the query language.
 */
export function recognizeRelationship(token: string): RelMatch | undefined {
  if (token.startsWith("-")) {
    return matchToken(token.slice(1))
      ? { kind: "invalid", token: token.toLowerCase() }
      : undefined;
  }
  return matchToken(token);
}

/** The positive half of recognition, shared by the plain and negated paths: the
 *  negated path only needs to know WHETHER the rest of the token is a rel or
 *  existence spelling, so this never yields an `invalid` result. */
function matchToken(token: string): Extract<RelMatch, { kind: "id" | "bool" }> | undefined {
  const lower = token.toLowerCase();
  const existence = EXISTENCE_TOKENS.get(lower);
  if (existence) return { kind: "bool", field: existence.field, value: existence.value };

  // Split on the FIRST colon so hyphenated field-names (`blocked-by`) are handled
  // without the metadata FIELD_TOKEN regex. Value is everything after it, taken
  // whole (scalar — no comma split), and only accepted when non-empty.
  const colon = token.indexOf(":");
  if (colon <= 0) return undefined;
  const name = token.slice(0, colon).toLowerCase();
  const value = token.slice(colon + 1).toLowerCase();
  const idField = REL_ID_FIELDS.get(name);
  if (idField && value !== "") return { kind: "id", field: idField, value };

  return undefined;
}

// --- Compile-time guards -------------------------------------------------------

// Membership: every field the vocabulary names must be a key the box owns
// (QueryFilter). If QueryFilter loses one of these keys, this fails to typecheck.
// Note this constrains the two UNIONS, not `REL_TOKEN_ORDER` — the array's own
// entries are checked against `RelTokenSpec` by its `satisfies` clause.
type _RelKeysAreQueryFilterKeys = (RelIdKey | ExistenceKey) extends keyof QueryFilter ? true : never;
const _relKeysCheck: _RelKeysAreQueryFilterKeys = true;
void _relKeysCheck;

// Exhaustiveness: every field in the two unions must appear in `REL_TOKEN_ORDER`.
// This is the guard that closes the silent-drop hole. Deleting an entry from the
// array otherwise compiles cleanly — recognition still accepts nothing extra,
// nothing indexes the missing key — and the token then vanishes on every
// canonicalization, because `serializeQuery` emits only what the array lists.
type _OrderCoversRelIds = RelIdKey extends OrderedSpec<"id">["field"] ? true : never;
const _orderCoversRelIds: _OrderCoversRelIds = true;
void _orderCoversRelIds;

type _OrderCoversExistence = ExistenceKey extends OrderedSpec<"bool">["field"] ? true : never;
const _orderCoversExistence: _OrderCoversExistence = true;
void _orderCoversExistence;

// Both spellings of a paired dimension. The guard above matches on the FIELD, and
// `has:parent`/`no:parent` share one — so dropping just the `no:` half slips past
// it while still silently losing that spelling. These two check the halves apart.
type ExistenceFieldsWriting<V extends boolean> = Extract<OrderedSpec<"bool">, { value: V }>["field"];
type _PairsKeepHas = PairedExistenceKey extends ExistenceFieldsWriting<true> ? true : never;
const _pairsKeepHas: _PairsKeepHas = true;
void _pairsKeepHas;

type _PairsKeepNo = PairedExistenceKey extends ExistenceFieldsWriting<false> ? true : never;
const _pairsKeepNo: _PairsKeepNo = true;
void _pairsKeepNo;

// The hierarchy subset must name fields the vocabulary actually carries. Renaming
// or dropping one of these upstream otherwise leaves `HierarchyKey` naming a field
// no `REL_TOKEN_ORDER` entry has, and `hierarchyTokens` silently stops emitting it
// — the filter would still be active while the empty-state explanation omits it.
type _HierarchySubsetOfVocabulary = HierarchyKey extends
  | OrderedSpec<"id">["field"]
  | OrderedSpec<"bool">["field"]
  ? true
  : never;
const _hierarchySubset: _HierarchySubsetOfVocabulary = true;
void _hierarchySubset;

// And the runtime array must cover the whole union: `satisfies` above rejects a
// WRONG entry but not a MISSING one, and a missing entry drops that field from both
// the explanation and the clear-hierarchy escape hatch.
type _HierarchyArrayCoversUnion = HierarchyKey extends (typeof HIERARCHY_FIELDS)[number]
  ? true
  : never;
const _hierarchyArrayCovers: _HierarchyArrayCoversUnion = true;
void _hierarchyArrayCovers;
