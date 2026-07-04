import { Flag, Layers, Bug, Sparkles, SquareCheck, FlaskConical } from "@lucide/svelte";
import type { Component } from "svelte";

export interface TypeIconInfo {
  icon: Component;
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
