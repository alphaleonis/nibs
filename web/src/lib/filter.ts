import type { NibSummary, NibFilter } from "./types";
import type { AreaVocabulary } from "./areas";

/**
 * Fields applied client-side although the server supports them, so a
 * non-matching ancestor of visible rows is dimmed (tableData.ts, Stage 4)
 * instead of dropped by the server with its children orphaned.
 */
const CLIENT_FIELDS = [
  "type",
  "priority",
  "estimate",
  "tags",
  "status",
  "excludeType",
  "excludePriority",
  "excludeEstimate",
  "excludeTags",
  "excludeStatus",
] as const;
type ClientField = (typeof CLIENT_FIELDS)[number];

export interface PreparedFilter {
  serverFilter: Omit<NibFilter, ClientField>;
  clientFiltersActive: boolean;
  matchesClient: (nib: NibSummary) => boolean;
}

/**
 * Whether `nib` matches every active include-list and no active `exclude*` list.
 * Empty lists are ignored. A tag list matches a nib carrying any listed tag.
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
  if (filter.excludeType?.length && filter.excludeType.includes(nib.type)) {
    return false;
  }
  if (filter.excludePriority?.length && filter.excludePriority.includes(nib.priority)) {
    return false;
  }
  if (filter.excludeStatus?.length && filter.excludeStatus.includes(nib.status)) {
    return false;
  }
  if (filter.excludeEstimate?.length && filter.excludeEstimate.includes(nib.estimate)) {
    return false;
  }
  if (filter.excludeTags?.length && nib.tags.some((tag) => filter.excludeTags!.includes(tag))) {
    return false;
  }
  return true;
}

/** Whether any client-side field is set. */
export function hasClientFilters(filter: NibFilter): boolean {
  return !!(
    filter.type?.length ||
    filter.priority?.length ||
    filter.status?.length ||
    filter.estimate?.length ||
    filter.tags?.length ||
    filter.excludeType?.length ||
    filter.excludePriority?.length ||
    filter.excludeStatus?.length ||
    filter.excludeEstimate?.length ||
    filter.excludeTags?.length
  );
}

/** The ownership keys, both checked against the vocabulary before they are sent. */
const AREA_FIELDS = ["area", "areaExact"] as const;

/**
 * `filter` with each ownership path withheld unless the vocabulary answers
 * "declared" for it.
 *
 * The server refuses an undeclared area with `FilterAreaError`
 * (`refuseUndeclaredArea`, internal/graph/filters.go), which is not tagged
 * NOT_FOUND, so the table would show an error instead of its inline empty state.
 * Filters are restored from localStorage and `?q=` before any vocabulary exists,
 * so a retired area gets past the parse-time check in query/area.ts.
 *
 * - "unknown" (LOADING_AREAS, UNAVAILABLE_AREAS) is withheld, widening the result
 *   until the config query succeeds. While it keeps failing, only the Areas view
 *   says so.
 * - "undeclared" is withheld; the query box renders the warning.
 * - The empty string is sent so the server refuses it; withholding it would widen
 *   the query to the whole store. The query box cannot produce one.
 */
function withSendableAreas(filter: NibFilter, areas: AreaVocabulary): NibFilter {
  let sendable = filter;
  for (const field of AREA_FIELDS) {
    const path = sendable[field];
    if (typeof path !== "string" || path === "") continue;
    if (areas.validity(path) === "declared") continue;
    const next = { ...sendable };
    delete next[field];
    sendable = next;
  }
  return sendable;
}

/**
 * Splits a filter into server-side and client-side parts, with a fast path when
 * no client filters are active. `areas` has no default: without it `area` cannot
 * be withheld.
 */
export function prepareFilter(filter: NibFilter, areas: AreaVocabulary): PreparedFilter {
  const sendable = withSendableAreas(filter, areas);

  if (!hasClientFilters(filter)) {
    return {
      serverFilter: sendable,
      clientFiltersActive: false,
      matchesClient: () => true,
    };
  }

  const {
    type,
    priority,
    estimate,
    tags,
    status,
    excludeType,
    excludePriority,
    excludeEstimate,
    excludeTags,
    excludeStatus,
    ...serverFilter
  } = sendable;

  return {
    serverFilter,
    clientFiltersActive: true,
    matchesClient: (nib: NibSummary) => matchesFilter(nib, filter),
  };
}

/**
 * Whether drag-and-drop reordering is allowed. Only search blocks it: search
 * flattens tree order, so a before/after anchor has no sibling meaning. Client
 * filters keep tree order, and reorderNib anchors on real siblings, so a drop
 * beside a visible row can land next to a hidden one.
 */
export function isDragAllowed(filter: NibFilter): boolean {
  return !filter.search;
}
