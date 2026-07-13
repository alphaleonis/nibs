import type { NibSummary, NibFilter } from "./types";

/**
 * Fields applied client-side even though the GraphQL server also supports them
 * (see internal/graph/filters.go). They are filtered here so a non-matching
 * ancestor of visible children is kept and dimmed in place (tableData.ts Stage 4)
 * rather than dropped by the server. excludeStatus in particular was moved here so
 * a completed parent of active children dims instead of vanishing (nibs-3up4).
 * A filtered-out leaf with no visible descendants is still removed from the rows.
 */
const CLIENT_FIELDS = ["type", "priority", "estimate", "tags", "status", "excludeStatus"] as const;
type ClientField = (typeof CLIENT_FIELDS)[number];

export interface PreparedFilter {
  serverFilter: Omit<NibFilter, ClientField>;
  clientFiltersActive: boolean;
  matchesClient: (nib: NibSummary) => boolean;
}

/**
 * Returns true if nib matches all active client filter criteria (type, priority,
 * status, excludeStatus, estimate, tags).
 * Empty/undefined filter arrays are ignored (match everything).
 * Multiple filter fields use AND logic — the nib must match all active filters.
 * Tags use OR logic within the group — nib must have at least one matching tag.
 * excludeStatus is a negative filter — a nib whose status is listed is a non-match.
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
  if (filter.excludeStatus?.length && filter.excludeStatus.includes(nib.status)) {
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
 * Search is not considered a client filter. excludeStatus IS a client filter
 * so a filtered-out ancestor of visible children is dimmed in place (Stage 4 in
 * tableData.ts) rather than dropped server-side; a filtered-out leaf with no
 * visible descendants is still removed from the rows.
 */
export function hasClientFilters(filter: NibFilter): boolean {
  return !!(
    filter.type?.length ||
    filter.priority?.length ||
    filter.status?.length ||
    filter.excludeStatus?.length ||
    filter.estimate?.length ||
    filter.tags?.length
  );
}

/**
 * Splits a filter into server-side and client-side parts.
 * Automatically resolves status conflicts before splitting so conflicting
 * filters are never sent to the server.
 * Returns a fast-path (matchesClient always true) when no client filters are active.
 */
export function prepareFilter(filter: NibFilter): PreparedFilter {
  // Resolve status conflicts defensively — callers may have already resolved,
  // but this ensures correctness regardless of call path.
  const resolved = resolveStatusConflicts(filter);

  if (!hasClientFilters(resolved)) {
    return {
      serverFilter: resolved,
      clientFiltersActive: false,
      matchesClient: () => true,
    };
  }

  const { type, priority, estimate, tags, status, excludeStatus, ...serverFilter } = resolved;

  return {
    serverFilter,
    clientFiltersActive: true,
    matchesClient: (nib: NibSummary) => matchesFilter(nib, resolved),
  };
}

/**
 * Returns an array of status values that appear in both filter.status and filter.excludeStatus.
 * Returns empty array if there's no conflict.
 */
export function getStatusConflicts(filter: NibFilter): string[] {
  const status = filter.status;
  const excludeStatus = filter.excludeStatus;
  if (!status || !excludeStatus) return [];
  return status.filter((s) => excludeStatus.includes(s));
}

/**
 * Returns true when drag-and-drop reordering is safe.
 *
 * Only search blocks drag: it flattens results out of tree order, so a "drop
 * before/after this anchor" gesture has no sibling meaning. Hide-filters
 * (type/priority/status/estimate/tags/excludeStatus) never reorder rows — matching
 * nibs keep their tree order, ancestors are dimmed in place, and only non-matching
 * leaves are removed — and reorder-on-drop is anchor-based (reorderNib against the
 * dragged item's real siblings on the backend), so it stays well-defined even when
 * other rows are hidden.
 *
 * Accepted caveat: dropping relative to a visible anchor while sibling leaves are
 * hidden lands the item in a well-defined but possibly-surprising spot (it may end
 * up adjacent to rows the filter currently hides).
 */
export function isDragAllowed(filter: NibFilter): boolean {
  return !filter.search;
}

/**
 * Returns a new filter with any conflicting status values removed from the include list.
 * If all status values conflict, the status field is removed entirely.
 * Returns the filter unchanged if there are no conflicts.
 */
export function resolveStatusConflicts(filter: NibFilter): NibFilter {
  const conflicts = getStatusConflicts(filter);
  if (conflicts.length === 0) return filter;
  const remaining = filter.status!.filter((s) => !conflicts.includes(s));
  const updated = { ...filter };
  if (remaining.length > 0) {
    updated.status = remaining;
  } else {
    delete updated.status;
  }
  return updated;
}
