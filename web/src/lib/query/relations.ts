import type { QueryFilter } from "./fields";

// Relationship and existence tokens, recognized separately from the metadata
// grammar. `FIELD_TOKEN`'s `[A-Za-z]+` name group cannot match the hyphenated
// names (`blocked-by`, `mentioned-by`), so recognition splits on the first colon.
//
// - Id tokens (`blocking:<id>`, `milestone:<id>`) set a scalar string field to
//   any non-empty lowercased value; the id is not checked.
// - Existence tokens (`has:parent`, `no:parent`, `is:blocked`) set a boolean
//   field. Only spellings in `REL_TOKEN_ORDER` are recognized; the rest
//   (`has:mentions`, `is:foo`) fall through to free text. Add a spelling only
//   where the server has a matching predicate.
// - A negated token is parked as invalid (see `recognizeRelationship`).
//
// `REL_TOKEN_ORDER` is the only list of spellings: the lookups derive from it,
// `serializeQuery` emits only what it lists, and the guards at the foot of this
// file fail compilation when a field is missing from it.

/** Scalar id-valued fields. `milestone` is an assignment rather than a link, but
 *  takes a nib id and the same typeahead. */
export type RelIdKey =
  | "parentId"
  | "ancestorId"
  | "descendantId"
  | "siblingId"
  | "blockingId"
  | "blockedById"
  | "mentionsId"
  | "mentionedById"
  | "milestone";

/** Existence fields with both a `has:` and a `no:` spelling. */
type PairedExistenceKey = "hasParent" | "hasBlocking" | "hasBlockedBy";

/** Tri-state existence fields. A paired field takes two spellings — `has:parent`
 *  writes true, `no:parent` writes false. `isBlocked` and `noMilestone` have one
 *  spelling each, so only `true` is typeable. */
export type ExistenceKey = PairedExistenceKey | "isBlocked" | "noMilestone";

/** One entry of the rel/existence vocabulary: a relationship-id token
 *  (`parent:<id>`), or one spelling of an existence token (`has:parent`). */
export type RelTokenSpec =
  | {
      kind: "id";
      field: RelIdKey;
      /** The token field-name, hyphenated ones included (`blocked-by`). It names
       *  the relationship the MATCHED nib holds toward the id, as the server's
       *  `NibFilter` fields do: `ancestor:X` keeps X's descendants, `descendant:X`
       *  keeps X's ancestor chain. */
      name: string;
      /** One line for the in-UI syntax help, from the matched nib's side. */
      description: string;
    }
  | {
      kind: "bool";
      field: ExistenceKey;
      /** The full token, exactly `word:value` with one colon: `complete.ts` splits
       *  on the first colon to derive completion words and values. */
      token: string;
      value: boolean;
      /** One line for the in-UI syntax help. */
      description: string;
    };

// The rel/existence vocabulary in canonical serialization order: grouped by
// dimension, each dimension's id token before its existence tokens. Moving an
// entry changes which strings are canonical.
//
// `as const` keeps the literal field types the guards at the foot of this file
// read; `satisfies` checks each entry against `RelTokenSpec`.
export const REL_TOKEN_ORDER = [
  { kind: "id", field: "parentId", name: "parent", description: "Direct children of this nib" },
  { kind: "bool", field: "hasParent", token: "has:parent", value: true, description: "Nibs that have a parent" },
  { kind: "bool", field: "hasParent", token: "no:parent", value: false, description: "Root nibs, with no parent" },
  // No has/no spellings: the server has no existence predicate for these.
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
  // The assignment axis. `milestone:<id>` matches DIRECT assignment; `is:backlog`
  // matches nibs with no DERIVED membership, so a child of an assigned epic is not
  // backlog.
  //
  // Backlog is one `is:` token, not a `has:`/`no:milestone` pair: on a field named
  // `noMilestone` a pair would write true for `no:`, while
  // `NEGATIVE_EXISTENCE_TOKENS` and the `_PairsKeep*` guards read an entry's value
  // as its polarity.
  { kind: "id", field: "milestone", name: "milestone", description: "Nibs assigned to this milestone" },
  { kind: "bool", field: "noMilestone", token: "is:backlog", value: true, description: "Nibs in no milestone's plan, their own or an inherited one" },
] as const satisfies readonly RelTokenSpec[];

/** A `REL_TOKEN_ORDER` entry with its literal types, narrowed to one kind. */
type OrderedSpec<K extends RelTokenSpec["kind"]> = Extract<
  (typeof REL_TOKEN_ORDER)[number],
  { kind: K }
>;

// Token field-name (`blocked-by`) → scalar-id NibFilter key. Also read by
// relComplete.ts.
// A Map, not an object: an object lookup finds inherited members, so
// `constructor:foo` would be recognized.
export const REL_ID_FIELDS: ReadonlyMap<string, RelIdKey> = new Map(
  REL_TOKEN_ORDER.flatMap((spec): [string, RelIdKey][] =>
    spec.kind === "id" ? [[spec.name, spec.field]] : [],
  ),
);

// Whole lowercased existence token (`has:parent`) → the field and value it
// writes. A Map for the same reason as `REL_ID_FIELDS`.
export const EXISTENCE_TOKENS: ReadonlyMap<string, { field: ExistenceKey; value: boolean }> =
  new Map(
    REL_TOKEN_ORDER.flatMap((spec): [string, { field: ExistenceKey; value: boolean }][] =>
      spec.kind === "bool" ? [[spec.token, { field: spec.field, value: spec.value }]] : [],
    ),
  );

// --- The hierarchy subset ------------------------------------------------------

/** The fields that constrain a nib's position in the tree. */
export type HierarchyKey = "parentId" | "ancestorId" | "descendantId" | "siblingId" | "hasParent";

/** `HierarchyKey` as a runtime list; guards below check the two match. */
const HIERARCHY_FIELDS = [
  "parentId",
  "ancestorId",
  "descendantId",
  "siblingId",
  "hasParent",
] as const satisfies readonly HierarchyKey[];

const HIERARCHY_FIELD_SET: ReadonlySet<string> = new Set(HIERARCHY_FIELDS);

/**
 * The canonical tokens for the hierarchy fields set on `filter`, in
 * `REL_TOKEN_ORDER` order — the text the box would serialize. `hasParent: false`
 * yields `no:parent`.
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

// --- Contradictory pairs -------------------------------------------------------

/**
 * The id-field / `no:` pairs the server refuses as contradictory
 * (`refuseContradiction`, internal/graph/filters.go). The server decides; this
 * table only lets the UI name the refusal in the box's spelling, so a missing
 * entry loses the explanation, not the result.
 *
 * Do not add `blockingId` + `hasBlocking`: `has:blocking` asks whether a nib is
 * ACTIVELY blocking, while `blocking:<id>` reads the target's stored blocked_by,
 * so the server answers that pair.
 */
const CONTRADICTORY_PAIRS = [
  { idField: "parentId", existenceField: "hasParent" },
  { idField: "blockedById", existenceField: "hasBlockedBy" },
] as const satisfies readonly { idField: RelIdKey; existenceField: PairedExistenceKey }[];

/** Scalar-id NibFilter key → token field-name; the reverse of `REL_ID_FIELDS`. */
const REL_ID_NAMES: ReadonlyMap<string, string> = new Map(
  REL_TOKEN_ORDER.flatMap((spec): [string, string][] =>
    spec.kind === "id" ? [[spec.field, spec.name]] : [],
  ),
);

/** Existence field → its `no:` spelling, for fields that have one. */
const NEGATIVE_EXISTENCE_TOKENS: ReadonlyMap<string, string> = new Map(
  REL_TOKEN_ORDER.flatMap((spec): [string, string][] =>
    spec.kind === "bool" && !spec.value ? [[spec.field, spec.token]] : [],
  ),
);

/**
 * The contradictory pairs `filter` holds, as `[idToken, noToken]` —
 * `[["parent:tnib-1", "no:parent"]]`. Empty means the refusal cannot be named
 * from this filter, which may have changed since the server refused.
 */
export function contradictionTokens(filter: QueryFilter): string[][] {
  const pairs: string[][] = [];
  for (const { idField, existenceField } of CONTRADICTORY_PAIRS) {
    const id = filter[idField];
    const name = REL_ID_NAMES.get(idField);
    const noToken = NEGATIVE_EXISTENCE_TOKENS.get(existenceField);
    if (!id || filter[existenceField] !== false || !name || !noToken) continue;
    pairs.push([`${name}:${id}`, noToken]);
  }
  return pairs;
}

/** A copy of `filter` with every hierarchy field removed. */
export function clearHierarchyFilters<T extends QueryFilter>(filter: T): T {
  const next = { ...filter };
  for (const field of HIERARCHY_FIELDS) {
    delete next[field];
  }
  return next;
}

/** A scalar-id assignment, a boolean existence assignment, or a negated token for
 *  the caller to park as invalid. */
export type RelMatch =
  | { kind: "id"; field: RelIdKey; value: string }
  | { kind: "bool"; field: ExistenceKey; value: boolean }
  | { kind: "invalid"; token: string };

/**
 * Recognize `token` as a relationship-id or existence token, or return
 * `undefined` for the caller to route to free text. Names and values are
 * lowercased.
 *
 * A negated token that would otherwise be recognized returns `invalid`. Do not
 * route it to free text: that becomes a Bleve query string, where `-ancestor:x`
 * is a MUST-NOT clause over an unindexed field (only id/slug/title/body are) and
 * excludes nothing. Other negated tokens (`-title:foo`) return `undefined`.
 */
export function recognizeRelationship(token: string): RelMatch | undefined {
  if (token.startsWith("-")) {
    return matchToken(token.slice(1))
      ? { kind: "invalid", token: token.toLowerCase() }
      : undefined;
  }
  return matchToken(token);
}

/** Recognition of an unnegated token; never returns `invalid`. */
function matchToken(token: string): Extract<RelMatch, { kind: "id" | "bool" }> | undefined {
  const lower = token.toLowerCase();
  const existence = EXISTENCE_TOKENS.get(lower);
  if (existence) return { kind: "bool", field: existence.field, value: existence.value };

  // The value is the whole run after the first colon, with no comma split.
  const colon = token.indexOf(":");
  if (colon <= 0) return undefined;
  const name = token.slice(0, colon).toLowerCase();
  const value = token.slice(colon + 1).toLowerCase();
  const idField = REL_ID_FIELDS.get(name);
  if (idField && value !== "") return { kind: "id", field: idField, value };

  return undefined;
}

// --- Compile-time guards -------------------------------------------------------

// Every field in the two unions is a QueryFilter key.
type _RelKeysAreQueryFilterKeys = (RelIdKey | ExistenceKey) extends keyof QueryFilter ? true : never;
const _relKeysCheck: _RelKeysAreQueryFilterKeys = true;
void _relKeysCheck;

// Every field in the two unions has a `REL_TOKEN_ORDER` entry; a missing one
// would compile and be dropped by `serializeQuery`.
type _OrderCoversRelIds = RelIdKey extends OrderedSpec<"id">["field"] ? true : never;
const _orderCoversRelIds: _OrderCoversRelIds = true;
void _orderCoversRelIds;

type _OrderCoversExistence = ExistenceKey extends OrderedSpec<"bool">["field"] ? true : never;
const _orderCoversExistence: _OrderCoversExistence = true;
void _orderCoversExistence;

// Both spellings of each paired field; the guard above matches on field, which
// the two spellings share.
type ExistenceFieldsWriting<V extends boolean> = Extract<OrderedSpec<"bool">, { value: V }>["field"];
type _PairsKeepHas = PairedExistenceKey extends ExistenceFieldsWriting<true> ? true : never;
const _pairsKeepHas: _PairsKeepHas = true;
void _pairsKeepHas;

type _PairsKeepNo = PairedExistenceKey extends ExistenceFieldsWriting<false> ? true : never;
const _pairsKeepNo: _PairsKeepNo = true;
void _pairsKeepNo;

// Every hierarchy field has a `REL_TOKEN_ORDER` entry, or `hierarchyTokens`
// omits it.
type _HierarchySubsetOfVocabulary = HierarchyKey extends
  | OrderedSpec<"id">["field"]
  | OrderedSpec<"bool">["field"]
  ? true
  : never;
const _hierarchySubset: _HierarchySubsetOfVocabulary = true;
void _hierarchySubset;

// `HIERARCHY_FIELDS` covers the union; its `satisfies` rejects a wrong entry but
// not a missing one.
type _HierarchyArrayCoversUnion = HierarchyKey extends (typeof HIERARCHY_FIELDS)[number]
  ? true
  : never;
const _hierarchyArrayCovers: _HierarchyArrayCoversUnion = true;
void _hierarchyArrayCovers;
