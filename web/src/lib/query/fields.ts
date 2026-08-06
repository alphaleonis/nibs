import type { NibFilter } from "../types";
import { TYPES, STATUSES, PRIORITIES, ESTIMATES, STATUS_GROUPS } from "../constants";

// The box-owned slice of a NibFilter: the five metadata facets, each with its
// positive include-list and negative exclude-list, the free-text `search`, and
// (phase 5) the relationship-id scalars + existence/state booleans. A full
// NibFilter is assignable to this (it is a superset), so the Toolbar can hand its
// canonical filter straight to `serializeQuery`.
export type QueryFilter = Pick<
  NibFilter,
  | "type"
  | "excludeType"
  | "priority"
  | "excludePriority"
  | "status"
  | "excludeStatus"
  | "estimate"
  | "excludeEstimate"
  | "tags"
  | "excludeTags"
  | "search"
  // Relationship-id scalars (phase 5).
  | "parentId"
  | "ancestorId"
  | "descendantId"
  | "siblingId"
  | "blockingId"
  | "blockedById"
  | "mentionsId"
  | "mentionedById"
  // Existence/state booleans (phase 5). Tri-state: `has:`/`no:` token pairs
  // write true/false on one field each.
  | "hasParent"
  | "hasBlocking"
  | "hasBlockedBy"
  | "isBlocked"
>;

type IncludeKey = "type" | "priority" | "status" | "estimate" | "tags";
type ExcludeKey = "excludeType" | "excludePriority" | "excludeStatus" | "excludeEstimate" | "excludeTags";

export interface FieldSpec {
  /** Token field name (lowercase), e.g. `type`. */
  name: string;
  /** The positive include-list key on NibFilter. */
  filterKey: IncludeKey;
  /** The negative exclude-list key on NibFilter. */
  excludeKey: ExcludeKey;
  /** Allowed values in canonical (declaration) order; `null` for tags, which
   *  are pattern-checked instead of validated against a fixed set. */
  values: readonly string[] | null;
  /** Group names accepted as shorthand for a set of `values` (`status:open`).
   *  A group is legal wherever a concrete value is, expands to its members on
   *  parse, and is re-collapsed on serialize wherever all of its members are
   *  present — beside other values, not only as the whole list. Absent for
   *  fields with no groups. */
  groups?: ReadonlyMap<string, readonly string[]>;
}

// Tag-value pattern: mirrors TAG_REGEX in markdown.ts. Defined locally so the
// query language stays pure and dependency-free (importing markdown.ts would
// pull marked + DOMPurify into this module's bundle). Values are lowercased
// before the check, so this validates structure only.
const TAG_VALUE_PATTERN = /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/;

// Canonical field order (also the serialization order): mirrors the dropdown row
// in the toolbar — type, priority, status, estimate, tags.
export const FIELD_SPECS: readonly FieldSpec[] = [
  { name: "type", filterKey: "type", excludeKey: "excludeType", values: TYPES },
  { name: "priority", filterKey: "priority", excludeKey: "excludePriority", values: PRIORITIES },
  { name: "status", filterKey: "status", excludeKey: "excludeStatus", values: STATUSES, groups: STATUS_GROUPS },
  { name: "estimate", filterKey: "estimate", excludeKey: "excludeEstimate", values: ESTIMATES },
  { name: "tags", filterKey: "tags", excludeKey: "excludeTags", values: null },
];

const SPEC_BY_NAME = new Map(FIELD_SPECS.map((spec) => [spec.name, spec]));

/** Look up a field spec by (case-insensitive) field name, or undefined if the
 *  name is not one of the five recognized metadata fields. */
export function fieldSpec(name: string): FieldSpec | undefined {
  return SPEC_BY_NAME.get(name.toLowerCase());
}

/** True when `value` (already lowercased) is a legal value for this field:
 *  membership for the four enums, tag-pattern for tags, plus the field's group
 *  names. Both `parseQuery` (routing) and `tokenizeSpans` (coloring) ask this
 *  one question, so a group can never parse as legal while rendering as an
 *  error. */
export function isValidValue(spec: FieldSpec, value: string): boolean {
  if (spec.values === null) return TAG_VALUE_PATTERN.test(value);
  return spec.values.includes(value) || spec.groups?.has(value) === true;
}

/** The concrete values a legal token value stands for: a group name yields its
 *  members, anything else stands for itself. Expanding at parse time keeps the
 *  group vocabulary out of NibFilter — only concrete values reach the backend. */
export function expandValue(spec: FieldSpec, value: string): readonly string[] {
  return spec.groups?.get(value) ?? [value];
}

/**
 * Render `values` as the canonical token list for this field: every group whose
 * members are ALL present collapses to the group name, and whatever is left over
 * stays spelled out beside it.
 *
 * This is the serialize-side inverse of `expandValue`, and it collapses a group
 * wherever its members appear rather than only when they are the entire list —
 * so `status:open,deferred` survives a round-trip through the box instead of
 * coming back as its four spelled-out members.
 *
 * Tokens are ordered by the LOWEST declaration index each one covers, which
 * generalizes `orderValues`' enum ordering to a list that mixes group names with
 * bare values: `open` covers index 0, so it precedes `completed` at index 4. Two
 * tokens cannot tie, because a collapsed group's members are removed from what
 * remains.
 *
 * Collapse is greedy in group-declaration order. The live status groups are
 * disjoint (`OPEN_STATUSES` is derived as the complement of `CLOSED_STATUSES`),
 * so there is no choice to make today; and greedy stays round-trip safe even if
 * that stops holding, because a group name expands to exactly the members it
 * consumed, leaving `parse(serialize(S)) === S` whatever order groups are taken in.
 */
export function collapseToTokens(spec: FieldSpec, values: readonly string[]): string[] {
  const remaining = new Set(values);
  const collapsed: { token: string; rank: number }[] = [];

  if (spec.groups) {
    for (const [name, members] of spec.groups) {
      // `every` over the raw members tolerates a repeated declaration without a
      // dedup step; an empty group is skipped so it cannot match vacuously and
      // emit a name standing for nothing.
      if (members.length === 0 || !members.every((m) => remaining.has(m))) continue;
      collapsed.push({ token: name, rank: rankOf(spec, members) });
      for (const m of members) remaining.delete(m);
    }
  }

  const rest = orderValues(spec, [...remaining]).map((v) => ({ token: v, rank: rankOf(spec, [v]) }));
  // Array.prototype.sort is stable, so equal ranks keep insertion order — which
  // is what preserves `orderValues`' alphabetical ordering for tags, where every
  // rank is the unknown-value sentinel.
  return [...collapsed, ...rest].sort((a, b) => a.rank - b.rank).map((t) => t.token);
}

/** The lowest index any of `members` occupies in the field's declared values —
 *  the sort key for a token. Fields with free-form values (tags) and values not
 *  in the enum share the sentinel, so they sort last and keep their relative order. */
function rankOf(spec: FieldSpec, members: readonly string[]): number {
  if (spec.values === null) return Number.MAX_SAFE_INTEGER;
  const order = spec.values;
  return members.reduce((lowest, m) => {
    const i = order.indexOf(m);
    return i === -1 ? lowest : Math.min(lowest, i);
  }, Number.MAX_SAFE_INTEGER);
}

/** The values to offer as completions for this field: group names first — they
 *  are the shorthand worth surfacing — then the concrete values in canonical
 *  order. Empty for tags, whose pool is the caller's available-tag list. */
export function completionValues(spec: FieldSpec): readonly string[] {
  if (spec.values === null) return [];
  if (!spec.groups) return spec.values;
  return [...spec.groups.keys(), ...spec.values];
}

/** Order a field's values canonically: enum-declaration order for the four
 *  enums, alphabetical for tags. Deduplicates. Unknown values (should not occur
 *  for a validated filter) sort last, preserving their relative order. */
export function orderValues(spec: FieldSpec, values: readonly string[]): string[] {
  const unique = [...new Set(values)];
  if (spec.values === null) {
    return unique.sort((a, b) => (a < b ? -1 : a > b ? 1 : 0));
  }
  const order = spec.values;
  return unique.sort((a, b) => {
    const ia = order.indexOf(a);
    const ib = order.indexOf(b);
    return (ia === -1 ? order.length : ia) - (ib === -1 ? order.length : ib);
  });
}
