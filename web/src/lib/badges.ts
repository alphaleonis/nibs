// statusDotColors is the per-status color for both the status glyph
// (StatusIcon) and inline status text. A new status needs an entry here AND in
// statusIcons (icons.ts).

export const priorityIndicators: Record<string, { symbol: string; color: string } | null> = {
  "critical": { symbol: "\u203C", color: "var(--priority-critical)" },
  "high": { symbol: "!", color: "var(--priority-high)" },
  "normal": null,
  "low": { symbol: "\u2193", color: "var(--priority-low)" },
};

export const statusDotColors: Record<string, string> = {
  "draft": "var(--status-draft-text)",
  "todo": "var(--status-todo-text)",
  "in-progress": "var(--status-in-progress-text)",
  "deferred": "var(--status-deferred-text)",
  "completed": "var(--status-completed-text)",
  "scrapped": "var(--status-scrapped-text)",
};

