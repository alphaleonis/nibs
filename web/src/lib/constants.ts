export const STATUSES = ["draft", "todo", "in-progress", "deferred", "completed", "scrapped"] as const;

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

export const TYPES = ["milestone", "epic", "bug", "feature", "task", "research"] as const;
export const PRIORITIES = ["critical", "high", "normal", "low"] as const;
export const ESTIMATES = ["s", "m", "l", "xl"] as const;

export const ESTIMATE_LABELS: Record<string, string> = {
  s: "Small",
  m: "Medium",
  l: "Large",
  xl: "Extra Large",
};
