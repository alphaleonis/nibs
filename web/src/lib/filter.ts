import type { NibSummary, NibFilter } from "./types";

/** Fields that are filtered client-side (not supported by the GraphQL server). */
const CLIENT_FIELDS = ["type", "priority", "estimate", "tags", "status"] as const;
type ClientField = (typeof CLIENT_FIELDS)[number];

export interface PreparedFilter {
  serverFilter: Omit<NibFilter, ClientField>;
  clientFiltersActive: boolean;
  matchesClient: (nib: NibSummary) => boolean;
}

/**
 * Returns true if nib matches all active client filter criteria (type, priority, status, estimate, tags).
 * Empty/undefined filter arrays are ignored (match everything).
 * Multiple filter fields use AND logic — the nib must match all active filters.
 * Tags use OR logic within the group — nib must have at least one matching tag.
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
 * Search and excludeStatus are not considered client filters.
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

  const { type, priority, estimate, tags, status, ...serverFilter } = resolved;

  return {
    serverFilter,
    clientFiltersActive: true,
    matchesClient: (nib: NibSummary) => matchesFilter(nib, resolved),
  };
}

/**
 * Returns a new filter with all client-side fields removed,
 * preserving only server-side fields (search, excludeStatus, etc.).
 */
export function clearClientFilters(filter: NibFilter): NibFilter {
  const { type, priority, estimate, tags, status, ...rest } = filter;
  return rest;
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

/** Returns true when drag-and-drop reordering is safe (no search/filters distorting row order). */
export function isDragAllowed(filter: NibFilter): boolean {
  return !filter.search && !hasClientFilters(filter);
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
