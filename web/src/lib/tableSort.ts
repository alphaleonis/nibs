// Client-side column sorting for the table. Pure helpers, no Svelte state: a
// view opts into a per-column sort by clicking a header, and these compute the
// resulting order and the tri-state header cycle. The Flat view sorts the whole
// list; the Tree + grouping-lens views sort SIBLINGS (the caller sorts the array
// before the view tree is built, and the tree builders preserve sibling order).
//
// The comparator is a REGISTRY keyed by column kind (one key extractor per
// sortable field), replacing the original date-only body. Each extractor returns
// a comparable key — a number, a string, or `null` for an empty/missing/invalid
// value. `null` keys sink LAST in BOTH directions (the uniform empties rule,
// mirroring the original invalid-date sink); equal keys compare 0, so JS
// Array.sort's stability keeps the incoming manual `order` sequence as the
// tiebreak.

import type { TableSort, SortField } from "./types";
import { STATUSES, TYPES, ESTIMATES } from "./constants";

// The exact per-row fields the comparators read — a structural subset of
// TreeTableNib (priority is not sortable, so it is omitted). Keeping it local
// keeps this module a pure, jsdom-free unit and lets tests build minimal rows.
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

// A resolved key is a number (dates, enum ranks, relation counts), a string
// (title / id / tags / parent title), or null (empty → sorts last).
type SortValue = number | string | null;

// One extractor per sortable column. `byId` resolves a nib's parent title (the
// parent column sorts by the parent nib's title). Reuses the existing rank
// sources — TYPES / STATUSES / ESTIMATES canonical order — never string-sorts
// enums. Relation columns sort by COUNT (0 is a real value, not empty).
const KEY_EXTRACTORS: Record<SortField, (nib: SortableRow, byId: Map<string, SortableRow>) => SortValue> = {
  title: (n) => textKey(n.title),
  // ids are lexicographic (case-sensitive), unlike the case-folded text sorts.
  id: (n) => n.id || null,
  type: (n) => orderKey(TYPES, n.type),
  state: (n) => orderKey(STATUSES, n.status),
  effort: (n) => orderKey(ESTIMATES, n.estimate?.trim().toLowerCase() ?? ""),
  created: (n) => dateKey(n.createdAt),
  modified: (n) => dateKey(n.updatedAt),
  blocking: (n) => n.blockingIds.length,
  blockedBy: (n) => n.blockedByIds.length,
  tags: (n) => textKey(n.tags[0]),
  parent: (n, byId) => {
    const p = n.parentId ? byId.get(n.parentId) : undefined;
    return p ? textKey(p.title) : null;
  },
};

/**
 * Compare two resolved keys under direction `dir` (1 asc, -1 desc). Empty (null)
 * keys sink LAST in both directions; equal keys return 0 so the sort stays
 * stable (incoming manual `order` is the tiebreak).
 */
function compareKeys(a: SortValue, b: SortValue, dir: number): number {
  const aEmpty = a === null;
  const bEmpty = b === null;
  if (aEmpty || bEmpty) {
    if (aEmpty && bEmpty) return 0;
    return aEmpty ? 1 : -1; // NOT multiplied by dir — empties are always last
  }
  if (a === b) return 0; // stable tiebreak preserves input order
  if (typeof a === "number" && typeof b === "number") return (a - b) * dir;
  return (a < b ? -1 : 1) * dir;
}

/**
 * Return a NEW array of `nibs` sorted by the given `TableSort`, or the ORIGINAL
 * array unchanged when `sort` is null (off → keep the incoming manual order).
 *
 * The comparator is selected from KEY_EXTRACTORS by `sort.field`. The sort is
 * STABLE (JS Array.sort is stable), so rows with equal keys keep their incoming
 * order. Empty / missing / invalid keys sort LAST regardless of direction.
 *
 * In the nested views the caller sorts `allNibs` up front and lets the tree
 * builders (which preserve sibling input order) nest the result — so this one
 * transform yields a flat sorted list in Flat and sibling-sort everywhere else.
 */
export function applySort<T extends SortableRow>(nibs: T[], sort: TableSort | null): T[] {
  if (!sort) return nibs;
  const dir = sort.direction === "asc" ? 1 : -1;
  const extract = KEY_EXTRACTORS[sort.field];
  // Only the parent sort needs an id→nib index (to read the parent's title);
  // building it for every sort would be wasted work.
  const byId = sort.field === "parent" ? new Map(nibs.map((n) => [n.id, n])) : EMPTY_BY_ID;
  return [...nibs].sort((a, b) => compareKeys(extract(a, byId), extract(b, byId), dir));
}

const EMPTY_BY_ID: Map<string, SortableRow> = new Map();

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
