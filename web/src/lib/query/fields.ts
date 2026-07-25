import type { NibFilter } from "../types";
import { TYPES, STATUSES, PRIORITIES, ESTIMATES } from "../constants";

// The box-owned slice of a NibFilter: the five metadata facets, each with its
// positive include-list and negative exclude-list, plus the free-text `search`.
// A full NibFilter is assignable to this (it is a superset), so the Toolbar can
// hand its canonical filter straight to `serializeQuery`; relationship/existence
// fields on NibFilter are simply ignored by the query language (phase 2 scope).
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
  { name: "status", filterKey: "status", excludeKey: "excludeStatus", values: STATUSES },
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
 *  membership for the four enums, tag-pattern for tags. */
export function isValidValue(spec: FieldSpec, value: string): boolean {
  if (spec.values === null) return TAG_VALUE_PATTERN.test(value);
  return spec.values.includes(value);
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
