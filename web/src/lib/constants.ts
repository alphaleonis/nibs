export const STATUSES = ["draft", "todo", "in-progress", "deferred", "completed", "scrapped"] as const;

// Terminal statuses — a nib with one of these has no active work left. This is
// the single source of truth for "done"; the State-facet presets below derive
// their include-lists from its complement, so a NEW active status added to
// STATUSES automatically flows into the presets instead of being silently
// hidden (a hardcoded include-list goes stale the moment a status is added).
export const TERMINAL_STATUSES = ["completed", "scrapped"] as const;

// Quick State-facet presets that set the `status` include-list in one click.
// The include-list is the single source of truth for status
// visibility, so these OVERWRITE the current selection.
//   Open + deferred → everything except completed + scrapped
//   Open           → active work only (also hides deferred)
export const OPEN_PLUS_DEFERRED_STATUSES: readonly string[] = STATUSES.filter(
  (s) => !(TERMINAL_STATUSES as readonly string[]).includes(s),
);
export const OPEN_STATUSES: readonly string[] = OPEN_PLUS_DEFERRED_STATUSES.filter(
  (s) => s !== "deferred",
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
