export const STATUSES = ["draft", "todo", "in-progress", "deferred", "completed", "scrapped"] as const;
export const TERMINAL_STATUSES = ["completed", "scrapped"] as const;
export const TYPES = ["milestone", "epic", "bug", "feature", "task"] as const;
export const PRIORITIES = ["critical", "high", "normal", "low"] as const;
export const ESTIMATES = ["s", "m", "l", "xl"] as const;

export const ESTIMATE_LABELS: Record<string, string> = {
  s: "Small",
  m: "Medium",
  l: "Large",
  xl: "Extra Large",
};
