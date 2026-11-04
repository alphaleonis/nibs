import type { MembershipNib } from "./membership";
import type { DropPlan } from "./ordering/dropPlan";

/**
 * Plans a drop of the dragged nibs onto the mapping-panel target `targetKey`.
 * `draggedNib` resolves a dragged id to its nib at grab time, or undefined when
 * the view does not show it.
 */
export type MappingPlanner = (
  targetKey: string,
  draggedIds: readonly string[],
  draggedNib: (id: string) => MembershipNib | undefined,
) => DropPlan;

/**
 * Whether mapping mode is on, and the planner of the mounted mapping panel.
 * Not persisted, so a reload starts with mapping mode off.
 */
export class MappingModeState {
  on: boolean = $state(false);
  /** Set by the mounted panel, which holds the whole-store lookup; null while none is mounted. */
  planner: MappingPlanner | null = $state(null);

  toggle(): void {
    this.on = !this.on;
  }
}
