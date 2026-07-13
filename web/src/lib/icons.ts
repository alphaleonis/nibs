import {
  Flag,
  Layers,
  Bug,
  Sparkles,
  SquareCheck,
  FlaskConical,
  CircleDashed,
  Circle,
  CirclePlay,
  CirclePause,
  CircleCheck,
  CircleX,
} from "@lucide/svelte";
import type { LucideIcon } from "@lucide/svelte";

export interface TypeIconInfo {
  icon: LucideIcon;
  color: string;
}

export const typeIcons: Record<string, TypeIconInfo> = {
  milestone: { icon: Flag, color: "var(--type-milestone)" },
  epic: { icon: Layers, color: "var(--type-epic)" },
  bug: { icon: Bug, color: "var(--type-bug)" },
  feature: { icon: Sparkles, color: "var(--type-feature)" },
  task: { icon: SquareCheck, color: "var(--type-task)" },
  research: { icon: FlaskConical, color: "var(--type-research)" },
};

// Per-status glyphs. Colors are NOT stored here — StatusIcon tints each glyph
// via statusDotColors (badges.ts), the single source of truth for status color.
export const statusIcons: Record<string, LucideIcon> = {
  "draft": CircleDashed,
  "todo": Circle,
  "in-progress": CirclePlay,
  "deferred": CirclePause,
  "completed": CircleCheck,
  "scrapped": CircleX,
};
