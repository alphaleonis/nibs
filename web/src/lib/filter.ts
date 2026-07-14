import type { NibSummary, NibFilter } from "./types";

/**
 * Fields applied client-side even though the GraphQL server also supports them
 * (see internal/graph/filters.go). They are filtered here so a non-matching
 * ancestor of visible children is kept and dimmed in place (tableData.ts Stage 4)
 * rather than dropped by the server. status in particular is applied here so a
 * completed parent of active children dims instead of vanishing.
 * A filtered-out leaf with no visible descendants is still removed from the rows.
 */
const CLIENT_FIELDS = ["type", "priority", "estimate", "tags", "status"] as const;
type ClientField = (typeof CLIENT_FIELDS)[number];

export interface PreparedFilter {
  serverFilter: Omit<NibFilter, ClientField>;
  clientFiltersActive: boolean;
  matchesClient: (nib: NibSummary) => boolean;
}

/**
 * Returns true if nib matches all active client filter criteria (type, priority,
 * status, estimate, tags).
 * Empty/undefined filter arrays are ignored (match everything).
 * Multiple filter fields use AND logic — the nib must match all active filters.
 * Tags use OR logic within the group — nib must have at least one matching tag.
 * status is an include-list — a nib whose status is NOT listed is a non-match.
 */
export function matchesFilter(nib: NibSummary, filter: NibFilter): boolean {
  if (filter.type?.length && !filter.type.includes(nib.type)) {
    return false;
  }
  if (filter.priority?.length && !filter.priority.includes(nib.priority)) {
    return false;
  }
  if (filter.status?.length && !filter.status.includes(nib.status)) {
    return false;
  }
  if (filter.estimate?.length && !filter.estimate.includes(nib.estimate)) {
    return false;
  }
  if (filter.tags?.length) {
    if (!nib.tags.some((tag) => filter.tags!.includes(tag))) {
      return false;
    }
  }
  return true;
}

/**
 * Returns true if filter has any active client-side filter criteria.
 * Search is not considered a client filter. status IS a client filter so a
 * filtered-out ancestor of visible children is dimmed in place (Stage 4 in
 * tableData.ts) rather than dropped server-side; a filtered-out leaf with no
 * visible descendants is still removed from the rows.
 */
export function hasClientFilters(filter: NibFilter): boolean {
  return !!(
    filter.type?.length ||
    filter.priority?.length ||
    filter.status?.length ||
    filter.estimate?.length ||
    filter.tags?.length
  );
}

/**
 * Splits a filter into server-side and client-side parts.
 * Returns a fast-path (matchesClient always true) when no client filters are active.
 */
export function prepareFilter(filter: NibFilter): PreparedFilter {
  if (!hasClientFilters(filter)) {
    return {
      serverFilter: filter,
      clientFiltersActive: false,
      matchesClient: () => true,
    };
  }

  const { type, priority, estimate, tags, status, ...serverFilter } = filter;

  return {
    serverFilter,
    clientFiltersActive: true,
    matchesClient: (nib: NibSummary) => matchesFilter(nib, filter),
  };
}

/**
 * Returns true when drag-and-drop reordering is safe.
 *
 * Only search blocks drag: it flattens results out of tree order, so a "drop
 * before/after this anchor" gesture has no sibling meaning. Hide-filters
 * (type/priority/status/estimate/tags) never reorder rows — matching nibs keep
 * their tree order, ancestors are dimmed in place, and only non-matching leaves
 * are removed — and reorder-on-drop is anchor-based (reorderNib against the
 * dragged item's real siblings on the backend), so it stays well-defined even
 * when other rows are hidden.
 *
 * Accepted caveat: dropping relative to a visible anchor while sibling leaves are
 * hidden lands the item in a well-defined but possibly-surprising spot (it may end
 * up adjacent to rows the filter currently hides).
 */
export function isDragAllowed(filter: NibFilter): boolean {
  return !filter.search;
}
