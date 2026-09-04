import type { NibSummary, NibFilter } from "./types";
import type { AreaVocabulary } from "./areas";

/**
 * Fields applied client-side even though the GraphQL server also supports them
 * (see internal/graph/filters.go). Both the positive include-lists and their
 * `exclude*` negations are filtered here so a non-matching ancestor of visible
 * children is kept and dimmed in place (tableData.ts Stage 4) rather than dropped
 * by the server. status/excludeStatus in particular are applied here so a
 * completed parent of active children dims instead of vanishing — a server-side
 * exclusion would drop the ancestor and detach its now-orphaned children.
 * A filtered-out leaf with no visible descendants is still removed from the rows.
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
 * Returns true if nib matches all active client filter criteria (type, priority,
 * status, estimate, tags) and none of the active `exclude*` negations.
 * Empty/undefined filter arrays are ignored (match everything).
 * Multiple filter fields use AND logic — the nib must match all active includes.
 * Tags use OR logic within the group — nib must have at least one matching tag.
 * status is an include-list — a nib whose status is NOT listed is a non-match.
 * An `exclude*` list always removes: a nib whose field value is in the list — or,
 * for excludeTags, that carries ANY listed tag — is a non-match, ANDed with the
 * positive include-lists.
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

/**
 * Returns true if filter has any active client-side filter criteria.
 * Search is not considered a client filter. The metadata includes and their
 * `exclude*` negations ARE client filters so a filtered-out ancestor of visible
 * children is dimmed in place (Stage 4 in tableData.ts) rather than dropped
 * server-side; a filtered-out leaf with no visible descendants is still removed
 * from the rows.
 */
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

/**
 * The filter as the server may be asked it: `area` withheld unless the
 * vocabulary answers "declared" for it.
 *
 * A bad `area` fails the WHOLE query instead of narrowing it: the server refuses
 * a path it does not declare rather than answering with the empty set, which
 * would read as "no work is in this area" for a path that names no area at all
 * (`refuseUndeclaredArea`, internal/graph/filters.go). `area` is not alone in
 * that — most id-valued fields fail the same way, and `milestone:` is typeable
 * in the query box today with no equivalent guard (nibs-f1sj). What singles it
 * out is where the refusal LANDS and what this side can do about it.
 * `FilterAreaError` implements no Unwrap — the only one in
 * internal/graph/filter_errors.go is `FilterTargetNotFoundError`'s, to
 * `nib.ErrNotFound` — so cmd/serve.go cannot tag it NOT_FOUND, and it misses the
 * calm inline branch TreeTable routes that code to, blanking the table with a
 * red error instead. And an area is the one such value the client can pre-check
 * at all, because it holds the vocabulary.
 *
 * Pre-checking is right rather than merely possible because this client RESTORES
 * filters: the filter is rebuilt from localStorage and `?q=` on load, so a value
 * that was declared when it was saved arrives after the area was retired, at a
 * moment the user did not act. A CLI invocation, where the value was just typed,
 * has no such moment. The rule sits on the filter rather than on a token, so it
 * holds for every way a value can arrive — including the `area:` token
 * (query/area.ts), which refuses an undeclared path at PARSE time but only when a
 * vocabulary was there to ask, and a restore happens before one is.
 *
 * So "declared" is sent, and the other two answers are held back at prices that
 * differ by which vocabulary gave them:
 *   - "unknown" from LOADING_AREAS costs a superset until the config query
 *     answers; re-deriving over the spine then asks again and gets "declared".
 *   - "unknown" from UNAVAILABLE_AREAS is the config query having FAILED. The
 *     superset it costs lasts until a re-ask succeeds: `useLiveConfig` re-asks
 *     on a growing backoff and again on socket recovery, so an outage that ends
 *     clears it without the reader doing anything. A failure that outlives the
 *     backoff does not clear itself, and while it stands the table answers with
 *     every nib in the store while the filter box still reads `area:…` — the
 *     Areas view says so and offers a retry, but no other view does, because
 *     nothing else reads `AreaVocabulary.status`.
 *   - "undeclared" is the drop half of the query box's warn-and-drop. The
 *     warning is the box's to render: it holds the token text, and this sees
 *     only the value.
 *
 * The EMPTY STRING is sent rather than withheld, so the server's refusal of it
 * stays loud. That refusal is deliberate and separate from an undeclared path:
 * read as "unset" the branch would be dropped and the query would widen to the
 * whole store. An empty-valued id field reaches the same verdict
 * (`FilterTargetEmptyError`), and the query box cannot produce either — a
 * relationship token with no value is not recognized (query/relations.ts) and a
 * metadata one becomes free text (query/parse.ts) — so client code is the only
 * thing that can set one. Withholding it here would perform exactly the widening
 * the server refuses to perform.
 */
function withSendableArea(filter: NibFilter, areas: AreaVocabulary): NibFilter {
  if (typeof filter.area !== "string") return filter;
  if (filter.area === "") return filter;
  if (areas.validity(filter.area) === "declared") return filter;
  const { area, ...rest } = filter;
  return rest;
}

/**
 * Splits a filter into server-side and client-side parts.
 * Returns a fast-path (matchesClient always true) when no client filters are active.
 *
 * `areas` has no default because there is no safe one: a caller without a
 * vocabulary would send whatever `area` it holds, which is the value this
 * parameter is here to withhold.
 */
export function prepareFilter(filter: NibFilter, areas: AreaVocabulary): PreparedFilter {
  const sendable = withSendableArea(filter, areas);

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
 * Returns true when drag-and-drop reordering is safe.
 *
 * Only search blocks drag: it flattens results out of tree order, so a "drop
 * before/after this anchor" gesture has no sibling meaning. Hide-filters
 * (type/priority/status/estimate/tags and their `exclude*` negations) never
 * reorder rows — matching nibs keep their tree order, ancestors are dimmed in
 * place, and only non-matching leaves are removed — and reorder-on-drop is
 * anchor-based (reorderNib against the dragged item's real siblings on the
 * backend), so it stays well-defined even when other rows are hidden.
 *
 * Accepted caveat: dropping relative to a visible anchor while sibling leaves are
 * hidden lands the item in a well-defined but possibly-surprising spot (it may end
 * up adjacent to rows the filter currently hides).
 */
export function isDragAllowed(filter: NibFilter): boolean {
  return !filter.search;
}
