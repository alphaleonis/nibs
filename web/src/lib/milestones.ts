/**
 * The milestone picker's vocabulary: which waypoints exist, and which of them a
 * given nib may be planned for.
 *
 * Separate from `membership.ts`, which mirrors the server's RULES, because this
 * module answers a presentation question the rules do not have: what a list of
 * choices looks like for one subject. It calls the rule rather than restating
 * it.
 *
 * Pure — no Svelte, no urql. The App builds the list from `MILESTONES_QUERY`; a
 * test builds one from literals.
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

/**
 * The value standing for "no milestone" inside a `Select`.
 *
 * A sentinel rather than the empty string the field actually carries: a Select
 * reads "" as "nothing is selected", so a None item valued "" cannot be chosen
 * — its change event is indistinguishable from the component clearing itself.
 * `fromSelectValue` is the one place that translates back.
 */
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
 * The choices to offer a subject, in the order the milestones were given (their
 * ORDER key — the sequence the waves are planned in).
 *
 * A milestone the assignment door would refuse is LISTED AND DISABLED rather
 * than dropped: the picker is also the only place the axis is displayed, and a
 * silently shorter list cannot be told from a store with fewer milestones.
 *
 * The subject's CURRENT assignment is never refused, whatever its status. That
 * pairing is reachable and legitimate — a milestone completed while open work
 * still pointed at it, or work retro-assigned to a finished wave — and refusing
 * it here would draw the value the nib actually carries as an illegal one.
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
