/**
 * The milestone picker's choices for one subject. Presentation over
 * `membership.ts`'s rules, which it calls rather than restates. Pure: no Svelte,
 * no urql.
 */

import { milestoneAcceptsAssignment } from "./membership";

/** One assignable milestone, as the picker needs it. */
export interface MilestoneOption {
  readonly id: string;
  readonly title: string;
  /** Read by the assignment door: a released milestone takes no open work. */
  readonly status: string;
}

/** One entry in a rendered picker. */
export interface MilestoneChoice extends MilestoneOption {
  /** Why this one cannot be chosen, or null when it can. */
  readonly refusal: string | null;
}

/** The `Select` value for "no milestone", because a Select reads "" as nothing
 *  selected; `fromSelectValue` translates back. */
export const NO_MILESTONE = "__none__";

/** The stored assignment a picker value means: "" for the None sentinel. */
export function fromSelectValue(value: string): string {
  return value === NO_MILESTONE ? "" : value;
}

/** The picker value for a stored assignment: the None sentinel for "". */
export function toSelectValue(milestone: string): string {
  return milestone === "" ? NO_MILESTONE : milestone;
}

/**
 * The choices to offer a subject, in the order given. A milestone the assignment
 * door refuses carries a refusal instead of being dropped. The subject's current
 * assignment is never refused, whatever its status.
 */
export function milestoneChoices(
  milestones: readonly MilestoneOption[],
  subject: { readonly status: string; readonly milestone: string },
): MilestoneChoice[] {
  return milestones.map((m) => ({
    ...m,
    refusal:
      m.id === subject.milestone || milestoneAcceptsAssignment(m.status, subject.status)
        ? null
        : `${m.title} is ${m.status} — only closed work can be planned for it.`,
  }));
}
