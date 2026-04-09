// --- CSS custom property-based style maps (used by TreeTableRow) ---
// Status display uses statusDotColors + inline text; type display uses icons.ts

export const priorityIndicators: Record<string, { symbol: string; color: string } | null> = {
  "critical": { symbol: "\u203C", color: "var(--priority-critical)" },
  "high": { symbol: "!", color: "var(--priority-high)" },
  "normal": null,
  "low": { symbol: "\u2193", color: "var(--priority-low)" },
  "deferred": { symbol: "\u21CA", color: "var(--priority-deferred)" },
};

export const statusDotColors: Record<string, string> = {
  "draft": "var(--status-draft-text)",
  "todo": "var(--status-todo-text)",
  "in-progress": "var(--status-in-progress-text)",
  "completed": "var(--status-completed-text)",
  "scrapped": "var(--status-scrapped-text)",
};

