import type { NibFilter } from "../types";
import { TYPES, STATUSES, PRIORITIES, ESTIMATES, STATUS_GROUPS } from "../constants";

// The slice of NibFilter the query box reads and writes. A full NibFilter is
// assignable to it. Add a new key to Toolbar's `BOX_FIELD_KEYS` too; that is the
// list `emitFromText` copies onto the filter.
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
  | "parentId"
  | "ancestorId"
  | "descendantId"
  | "siblingId"
  | "blockingId"
  | "blockedById"
  | "mentionsId"
  | "mentionedById"
  | "milestone"
  | "area"
  | "hasParent"
  | "hasBlocking"
  | "hasBlockedBy"
  | "isBlocked"
  | "noMilestone"
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
  /** Group names standing for sets of `values` (`status:open`): legal wherever a
   *  value is, expanded on parse, collapsed on serialize. */
  groups?: ReadonlyMap<string, readonly string[]>;
}

// Same pattern as TAG_REGEX in markdown.ts, copied so this module does not pull
// in marked and DOMPurify.
const TAG_VALUE_PATTERN = /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/;

// Canonical field order, also the serialization order; matches the toolbar's
// facet dropdowns.
export const FIELD_SPECS: readonly FieldSpec[] = [
  { name: "type", filterKey: "type", excludeKey: "excludeType", values: TYPES },
  { name: "priority", filterKey: "priority", excludeKey: "excludePriority", values: PRIORITIES },
  { name: "status", filterKey: "status", excludeKey: "excludeStatus", values: STATUSES, groups: STATUS_GROUPS },
  { name: "estimate", filterKey: "estimate", excludeKey: "excludeEstimate", values: ESTIMATES },
  { name: "tags", filterKey: "tags", excludeKey: "excludeTags", values: null },
];

const SPEC_BY_NAME = new Map(FIELD_SPECS.map((spec) => [spec.name, spec]));

/** The metadata field spec for a case-insensitive name. */
export function fieldSpec(name: string): FieldSpec | undefined {
  return SPEC_BY_NAME.get(name.toLowerCase());
}

/** Whether lowercased `value` is legal for the field: an enum member, a tag
 *  matching the pattern, or a group name. `parseQuery` and `tokenizeSpans` both
 *  ask it. */
export function isValidValue(spec: FieldSpec, value: string): boolean {
  if (spec.values === null) return TAG_VALUE_PATTERN.test(value);
  return spec.values.includes(value) || spec.groups?.has(value) === true;
}

/** The concrete values a legal value stands for: a group's members, or itself.
 *  Group names never reach NibFilter. */
export function expandValue(spec: FieldSpec, value: string): readonly string[] {
  return spec.groups?.get(value) ?? [value];
}

/**
 * Render `values` as the field's canonical token list, the inverse of
 * `expandValue`: each group whose members are all present becomes the group name,
 * beside the leftover values (`status:open,deferred`).
 *
 * Tokens sort by the lowest declaration index they cover. Groups collapse greedily
 * in declaration order, which round-trips even for overlapping groups because a
 * group name expands to exactly the members it consumed.
 */
export function collapseToTokens(spec: FieldSpec, values: readonly string[]): string[] {
  const remaining = new Set(values);
  const collapsed: { token: string; rank: number }[] = [];

  if (spec.groups) {
    for (const [name, members] of spec.groups) {
      // Skip an empty group, which would match vacuously.
      if (members.length === 0 || !members.every((m) => remaining.has(m))) continue;
      collapsed.push({ token: name, rank: rankOf(spec, members) });
      for (const m of members) remaining.delete(m);
    }
  }

  const rest = orderValues(spec, [...remaining]).map((v) => ({ token: v, rank: rankOf(spec, [v]) }));
  // The sort is stable: tags all share the sentinel rank and keep `orderValues`'
  // alphabetical order.
  return [...collapsed, ...rest].sort((a, b) => a.rank - b.rank).map((t) => t.token);
}

/** The lowest declared index among `members`; tags and unknown values get the
 *  sentinel and sort last. */
function rankOf(spec: FieldSpec, members: readonly string[]): number {
  if (spec.values === null) return Number.MAX_SAFE_INTEGER;
  const order = spec.values;
  return members.reduce((lowest, m) => {
    const i = order.indexOf(m);
    return i === -1 ? lowest : Math.min(lowest, i);
  }, Number.MAX_SAFE_INTEGER);
}

/** Completion values: group names, then values in canonical order. Empty for
 *  tags. */
export function completionValues(spec: FieldSpec): readonly string[] {
  if (spec.values === null) return [];
  if (!spec.groups) return spec.values;
  return [...spec.groups.keys(), ...spec.values];
}

/** Deduplicated values in canonical order: declaration order for enums,
 *  alphabetical for tags. Unknown values sort last. */
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
