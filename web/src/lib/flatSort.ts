// Client-side date sorting for the Flat view. Pure helpers, no Svelte state:
// the Flat view opts into a Created/Modified sort by clicking a header, and
// these compute the resulting order and the tri-state header cycle.

import type { FlatSort, FlatSortField } from "./types";

/** Epoch ms for an ISO string, or NaN for empty / unparseable input. */
function timeOf(iso: string): number {
  if (!iso) return NaN;
  return new Date(iso).getTime();
}

/**
 * Return a NEW array of `nibs` sorted by the given `FlatSort`, or the ORIGINAL
 * array unchanged when `sort` is null (off → keep the incoming manual order).
 *
 * `created` sorts on `createdAt`, `modified` on `updatedAt`. The sort is STABLE
 * (JS Array.sort is stable), so rows with equal timestamps keep their incoming
 * order as the tiebreak. Empty / unparseable timestamps sort LAST regardless of
 * direction.
 */
export function applyFlatSort<T extends { createdAt: string; updatedAt: string }>(
  nibs: T[],
  sort: FlatSort | null,
): T[] {
  if (!sort) return nibs;
  const key = sort.field === "created" ? "createdAt" : "updatedAt";
  const dir = sort.direction === "asc" ? 1 : -1;
  return [...nibs].sort((a, b) => {
    const ta = timeOf(a[key]);
    const tb = timeOf(b[key]);
    const aInvalid = Number.isNaN(ta);
    const bInvalid = Number.isNaN(tb);
    // Invalid values sink to the bottom in both directions; two invalids keep
    // input order (stable), so the comparator returns 0.
    if (aInvalid || bInvalid) {
      if (aInvalid && bInvalid) return 0;
      return aInvalid ? 1 : -1;
    }
    if (ta === tb) return 0; // stable tiebreak preserves input order
    return (ta - tb) * dir;
  });
}

/**
 * Tri-state header cycle for a Flat sort control. Clicking a field advances:
 *   off / other field → ascending
 *   same field asc     → descending
 *   same field desc    → off (null)
 */
export function nextFlatSort(current: FlatSort | null, field: FlatSortField): FlatSort | null {
  if (!current || current.field !== field) return { field, direction: "asc" };
  if (current.direction === "asc") return { field, direction: "desc" };
  return null;
}
