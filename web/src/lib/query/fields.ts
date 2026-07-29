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
   *  parse, and is re-collapsed on serialize when a value list matches one
   *  exactly. Absent for fields with no groups. */
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

/** The name of the group whose members are exactly `values` as a set, or
 *  undefined when none matches. This is the serialize-side inverse of
 *  `expandValue`: it is what lets `status:open` survive a round-trip through
 *  the box instead of expanding into its members on blur. */
export function matchingGroup(spec: FieldSpec, values: readonly string[]): string | undefined {
  if (!spec.groups) return undefined;
  const unique = new Set(values);
  for (const [name, members] of spec.groups) {
    // Both sides are deduplicated before the size comparison, so this is set
    // equality by construction rather than by a precondition on the group
    // declaration: a repeated member would otherwise inflate the member count
    // and let a strict SUPERSET of the group satisfy both halves of the check,
    // labeling it with a group name it does not have.
    const uniqueMembers = new Set(members);
    if (uniqueMembers.size === unique.size && members.every((m) => unique.has(m))) return name;
  }
  return undefined;
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
