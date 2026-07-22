import { Link, Lock } from "@lucide/svelte";

/** The two directed dependency relations a nib can surface in the UI. */
export type RelationKind = "blocked" | "blocking";

export interface RelationConfig {
  /** Lucide icon component for this relation. */
  icon: typeof Lock;
  /** Pill label. */
  label: string;
  /** Tailwind utilities for the pill's tint + label color. */
  pillClasses: string;
  /** Foreground color for the bare icon (badge icon variant and the count columns). */
  iconColor: string;
  /** Tooltip text given the related-nib count. */
  title: (count: number) => string;
}

/**
 * Single source of truth for how each relation is drawn (icon, color, label).
 * Consumed by RelationBadge (pill + bare-icon variants) and by the opt-in
 * Blocking / Blocked-by count columns in TreeTableRow, so the two cannot drift.
 */
export const RELATION_CONFIG: Record<RelationKind, RelationConfig> = {
  blocked: {
    icon: Lock,
    label: "Blocked",
    pillClasses: "bg-blocked-bg text-blocked",
    iconColor: "var(--blocked)",
    title: (count) => `Blocked by ${count} nib(s)`,
  },
  blocking: {
    icon: Link,
    label: "Blocking",
    pillClasses: "bg-blocking-bg text-blocking",
    iconColor: "var(--blocking)",
    title: (count) => `Blocking ${count} nib(s)`,
  },
};
