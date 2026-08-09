export const STATUSES = ["draft", "todo", "in-progress", "deferred", "completed", "scrapped"] as const;

// The order status *choosers* list in — the transition order, the path work
// takes from draft to a closed state. STATUSES above is the ordering everything
// else uses: the status-column sort in tableSort.ts, the facet checkboxes and
// the query-language value lists, where the closed statuses read best in the
// same sequence the Go side ranks them.
//
// Only the two closed statuses differ between the lists (completed before
// deferred here), which is exactly why this is a separate constant rather than
// a reorder of STATUSES: swapping them in STATUSES would quietly re-sort the
// status column.
//
// STATUS_WORKFLOW_ORDER is the literal sequence, pinned name-for-name and
// position-for-position against Go's config.workflowStatusOrder by
// TestWebConstantsMatchConfig — a stricter pin than the membership-only one it
// applies to STATUSES, whose order differs from Go's on purpose.
//
// STATUS_WORKFLOW is what components read, and it is built from STATUSES rather
// than used verbatim: a status the sequence forgets is appended instead of
// dropped, so an ordering mistake makes a picker read oddly and never hides a
// status a nib can be set to. Same fail-safe as config.orderStatusesBy on the
// Go side.
export const STATUS_WORKFLOW_ORDER = [
  "draft",
  "todo",
  "in-progress",
  "completed",
  "deferred",
  "scrapped",
] as const;

export const STATUS_WORKFLOW: readonly string[] = [
  ...STATUS_WORKFLOW_ORDER.filter((s) => (STATUSES as readonly string[]).includes(s)),
  ...STATUSES.filter((s) => !(STATUS_WORKFLOW_ORDER as readonly string[]).includes(s)),
];

// Closed statuses — a nib with one of these is off the board. `deferred` is one
// of them: setting work aside is a way of closing it, not a state of being
// open, so it is hidden by the Open preset alongside completed and scrapped.
//
// This is the single source of truth for "closed"; the State-facet preset below
// derives its include-list from the complement, so a NEW open status added to
// STATUSES automatically flows into the preset instead of being silently hidden
// (a hardcoded include-list goes stale the moment a status is added).
//
// These names and this membership are pinned against the Go configuration by
// TestWebConstantsMatchConfig — the vocabulary is duplicated here only because
// GraphQL does not serve it, so the guard is what keeps the two from drifting.
export const CLOSED_STATUSES = ["deferred", "completed", "scrapped"] as const;

// Quick State-facet preset that sets the `status` include-list in one click.
// The include-list is the single source of truth for status visibility, so this
// OVERWRITES the current selection.
//   Open → everything that is not closed
export const OPEN_STATUSES: readonly string[] = STATUSES.filter(
  (s) => !(CLOSED_STATUSES as readonly string[]).includes(s),
);

// Status groups: names accepted anywhere a concrete status is, standing for the
// set of statuses they contain. This mirrors the CLI, where `-s open` is legal
// wherever `-s todo` is (cmd/statusfilter.go), so one *value* vocabulary spans
// both surfaces: the word after `-s` on the CLI is the word after `status:` in
// the box. The token syntax is not shared — the box's `-` prefix is negation
// (`-status:closed`), so CLI flags do not parse here.
//
// Membership is DERIVED from the two lists above rather than restated, so a
// status added to STATUSES lands in exactly one group automatically. The group
// NAMES are the one thing that cannot be derived, so they are pinned against
// the Go constants by TestWebStatusGroupsMatchCLI.
//
// A Map, not an object literal: object lookup would resolve inherited keys, so
// `status:constructor` would read as a legal group.
//
// Only two groups, and every group has more than one member. A group over a
// single status would just be a second spelling of that status — which is why
// there is no `parked` group for {deferred}.
export const STATUS_GROUPS: ReadonlyMap<string, readonly string[]> = new Map([
  ["open", OPEN_STATUSES],
  ["closed", CLOSED_STATUSES as readonly string[]],
]);

export const TYPES = ["milestone", "epic", "bug", "feature", "task", "research"] as const;
export const PRIORITIES = ["critical", "high", "normal", "low"] as const;
export const ESTIMATES = ["s", "m", "l", "xl"] as const;

export const ESTIMATE_LABELS: Record<string, string> = {
  s: "Small",
  m: "Medium",
  l: "Large",
  xl: "Extra Large",
};
