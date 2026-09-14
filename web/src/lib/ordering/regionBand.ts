import { sameRegion, type Region } from "./region";

/** The axis a region boundary belongs to, so its band takes that list's colors. */
export type BandAxis = Region["axis"];

/** The two facts a boundary is decided from. `RowData` satisfies it structurally. */
export interface BandRow {
  readonly depth: number;
  readonly region: Region | null;
}

/**
 * Whether an axis takes the milestone-queue styling wherever a surface colors
 * by axis. A `Record`, so a new axis fails to compile here until it picks one.
 */
const QUEUE_STYLED: Record<BandAxis, boolean> = { parent: false, milestone: true };

export function isQueueAxis(axis: BandAxis | null | undefined): boolean {
  return axis != null && QUEUE_STYLED[axis];
}

/** The two facts a drop's treatment is decided from. `AcceptedDrop` satisfies it structurally. */
export type StyledDrop =
  | { readonly kind: "position"; readonly region: Region }
  | { readonly kind: "assign" };

/** How a surface colors an accepted drop. */
export type DropTreatment = "parent" | "queue" | "assign";

/**
 * Which treatment an accepted drop takes, or null when nothing is accepted.
 * Switches on the plan's kind: an assignment carries no region, and
 * `isQueueAxis(region?.axis)` would color it as a parent-axis reorder.
 */
export function dropTreatment(drop: StyledDrop | null | undefined): DropTreatment | null {
  if (drop == null) return null;
  switch (drop.kind) {
    case "position":
      return isQueueAxis(drop.region.axis) ? "queue" : "parent";
    case "assign":
      return "assign";
  }
}

/**
 * Which axis wins when the two sides of a seam are on different ones: a seam
 * with a queue on either side is a queue seam. A `Record`, so a new axis must be
 * ranked.
 */
const AXIS_RANK: Record<BandAxis, number> = { parent: 0, milestone: 1 };

/** The axis a seam takes when neither side names one — two rows in no region. */
const NEUTRAL_AXIS: BandAxis = "parent";

/**
 * Whether a region boundary runs ABOVE this row, and on which axis, or null
 * where the two rows continue one list.
 *
 * Only the closing side of a boundary is drawn: a run opened by descending into
 * the row above is already marked by the indent. So a run under its own parent
 * is unmarked, and a rule can fall inside one list where a deeper run sits
 * between two of its members. A row in no region (a fabricated container) opens
 * a boundary against whatever it follows, which bands between a lens's sections.
 *
 * The axis comes from either side, so the end of a queue is a queue seam.
 *
 * PRECONDITION: `previous` is the row immediately above `row` in a depth-first
 * flatten; otherwise the depth guard suppresses real boundaries.
 */
export function regionBandAt(row: BandRow, previous: BandRow | null): BandAxis | null {
  if (previous === null) return null;
  if (row.depth > previous.depth) return null;
  if (sameRegion(row.region, previous.region)) return null;
  // Seeded from the first present axis, so `NEUTRAL_AXIS` is a default and not a
  // floor on the ranking.
  let axis: BandAxis | null = null;
  for (const side of [row.region?.axis, previous.region?.axis]) {
    if (side !== undefined && (axis === null || AXIS_RANK[side] > AXIS_RANK[axis])) axis = side;
  }
  return axis ?? NEUTRAL_AXIS;
}
