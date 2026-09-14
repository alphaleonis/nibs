// Client-side column sorting for the table. Flat sorts the whole list; the
// nested views sort siblings, because the caller sorts before the view tree is
// built and the tree builders keep sibling order.
//
// Each sort field has a key extractor returning a number, a string, or null for
// an empty or invalid value. Null keys sort last in both directions; equal keys
// compare 0, so the stable sort keeps the incoming manual order as tiebreak.

import type { TableSort, SortField } from "./types";
import { STATUSES, TYPES, ESTIMATES } from "./constants";

// The TreeTableNib fields the comparators read, so tests can build minimal rows.
export interface SortableRow {
  id: string;
  title: string;
  status: string;
  type: string;
  estimate: string;
  tags: string[];
  createdAt: string;
  updatedAt: string;
  parentId: string | null;
  blockingIds: string[];
  blockedByIds: string[];
  /** Milestone assignment as stored; "" when unassigned. Resolved through
   *  `byId` to sort by the milestone's title, the way `parentId` is. */
  milestone: string;
  /** Area path as stored; "" when unassigned. Sorts as text — an area is a
   *  declared path, not a nib, so there is nothing to resolve. */
  area: string;
}

/** Epoch ms for an ISO string, or null for empty / unparseable input. */
function dateKey(iso: string): number | null {
  if (!iso) return null;
  const t = new Date(iso).getTime();
  return Number.isNaN(t) ? null : t;
}

/** Trimmed, case-folded string for case-insensitive text sorts; null if blank. */
function textKey(s: string | undefined): string | null {
  const t = s?.trim();
  return t ? t.toLowerCase() : null;
}

/** Index of `value` in a canonical order (enum rank); null when not present. */
function orderKey(order: readonly string[], value: string): number | null {
  const i = order.indexOf(value);
  return i === -1 ? null : i;
}

// null means empty.
type SortValue = number | string | null;

// Enums sort by canonical rank, never as strings; relations by count, where 0
// is a value, not empty. `byId` resolves the parent and milestone titles.
const KEY_EXTRACTORS: Record<SortField, (nib: SortableRow, byId: ReadonlyMap<string, SortableRow>) => SortValue> = {
  title: (n) => textKey(n.title),
  // ids are lexicographic (case-sensitive), unlike the case-folded text sorts.
  id: (n) => n.id || null,
  type: (n) => orderKey(TYPES, n.type),
  status: (n) => orderKey(STATUSES, n.status),
  estimate: (n) => orderKey(ESTIMATES, n.estimate?.trim().toLowerCase() ?? ""),
  created: (n) => dateKey(n.createdAt),
  modified: (n) => dateKey(n.updatedAt),
  blocking: (n) => n.blockingIds.length,
  blockedBy: (n) => n.blockedByIds.length,
  tags: (n) => textKey(n.tags[0]),
  parent: (n, byId) => {
    const p = n.parentId ? byId.get(n.parentId) : undefined;
    return p ? textKey(p.title) : null;
  },
  // By the milestone's title, like `parent`. An assignment naming a nib not in
  // `byId` sorts as empty.
  milestone: (n, byId) => {
    const m = n.milestone ? byId.get(n.milestone) : undefined;
    return m ? textKey(m.title) : null;
  },
  area: (n) => textKey(n.area),
};

/** Compare two keys under `dir` (1 asc, -1 desc). Null sorts last either way. */
function compareKeys(a: SortValue, b: SortValue, dir: number): number {
  const aEmpty = a === null;
  const bEmpty = b === null;
  if (aEmpty || bEmpty) {
    if (aEmpty && bEmpty) return 0;
    return aEmpty ? 1 : -1; // NOT multiplied by dir — empties are always last
  }
  if (a === b) return 0;
  if (typeof a === "number" && typeof b === "number") return (a - b) * dir;
  return (a < b ? -1 : 1) * dir;
}

/**
 * The row comparator for `sort`, shared by `applySort` and the grouped view
 * tree's node ordering. `byId` is read only by fields that resolve another row.
 */
export function makeNibComparator<T extends SortableRow>(
  sort: TableSort,
  byId: ReadonlyMap<string, T>,
): (a: T, b: T) => number {
  const dir = sort.direction === "asc" ? 1 : -1;
  const extract = KEY_EXTRACTORS[sort.field];
  return (a, b) => compareKeys(extract(a, byId), extract(b, byId), dir);
}

/** A new array sorted by `sort`, or `nibs` itself when `sort` is null. */
export function applySort<T extends SortableRow>(nibs: T[], sort: TableSort | null): T[] {
  if (!sort) return nibs;
  const byId: ReadonlyMap<string, T> =
    RESOLVING_SORT_FIELDS.has(sort.field) ? new Map<string, T>(nibs.map((n) => [n.id, n])) : EMPTY_BY_ID;
  return [...nibs].sort(makeNibComparator(sort, byId));
}

// The fields whose extractor reads `byId`. A resolving field missing here sorts
// every row as empty.
const RESOLVING_SORT_FIELDS: ReadonlySet<SortField> = new Set<SortField>(["parent", "milestone"]);

// `never` values make this assignable to `ReadonlyMap<string, T>` for any row type.
const EMPTY_BY_ID: ReadonlyMap<string, never> = new Map<string, never>();

/**
 * Tri-state header cycle for a table sort control. Clicking a field advances:
 *   off / other field → ascending
 *   same field asc     → descending
 *   same field desc    → off (null)
 */
export function nextTableSort(current: TableSort | null, field: SortField): TableSort | null {
  if (!current || current.field !== field) return { field, direction: "asc" };
  if (current.direction === "asc") return { field, direction: "desc" };
  return null;
}
