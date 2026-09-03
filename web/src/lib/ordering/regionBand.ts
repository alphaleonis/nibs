import { sameRegion, type Region } from "./region";

/**
 * Which axis a boundary is a boundary OF, so the band can be drawn in the colors
 * of the list it closes. `null` where two rows continue one list.
 */
export type BandAxis = Region["axis"];

/**
 * The two facts a boundary is decided from. Structural rather than `RowData`, so
 * this rule stays independent of the table's row shape — `RowData` satisfies it
 * without being named here.
 */
export interface BandRow {
  readonly depth: number;
  readonly region: Region | null;
}

/**
 * Whether an axis takes the milestone-queue treatment — the cyan band, the cyan
 * drop indicator, the cyan badge border — wherever a surface colors by axis.
 *
 * A `Record` rather than an `axis === "milestone"` test repeated at each of
 * those surfaces: a third arm is then a compile error HERE until it says which
 * treatment it takes, instead of silently rendering in the parent axis's colors
 * at three places that never mentioned it. The same discipline `scopeOf` and
 * `regionKey` state in region.ts. It does not make a new axis render unstyled —
 * `false` still means parent-axis colors — it makes that an answer someone gave.
 */
const QUEUE_STYLED: Record<BandAxis, boolean> = { parent: false, milestone: true };

export function isQueueAxis(axis: BandAxis | null | undefined): boolean {
  return axis != null && QUEUE_STYLED[axis];
}

/**
 * The two facts a drop's treatment is decided from. Structural rather than
 * `AcceptedDrop`, so this rule stays independent of the drag state's shape —
 * `AcceptedDrop` satisfies it without being named here, the way `RowData`
 * satisfies `BandRow`.
 */
export type StyledDrop =
  | { readonly kind: "position"; readonly region: Region }
  | { readonly kind: "assign" };

/** How a surface colors an accepted drop. */
export type DropTreatment = "parent" | "queue" | "assign";

/**
 * Which treatment an accepted drop takes, or null when nothing is accepted.
 *
 * An exhaustive switch on the plan's KIND, not a read of `region?.axis`: a plan
 * carrying no region answers `false` to `isQueueAxis` and takes the parent
 * axis's colors, which is exactly the confusion an assignment must not be drawn
 * in — a sibling reorder is the gesture it sits on the same pixel as. `?.`
 * swallows an absence; a switch with no default arm cannot.
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
 * Which axis wins when the two sides of a seam are on different ones. The queue
 * outranks the parent axis for the reason `regionBandAt` gives: a seam a queue
 * is on either side of is a queue seam.
 *
 * A `Record` for the same reason as `QUEUE_STYLED` — a third arm cannot be added
 * without ranking it.
 */
const AXIS_RANK: Record<BandAxis, number> = { parent: 0, milestone: 1 };

/** The axis a seam takes when neither side names one — two rows in no region. */
const NEUTRAL_AXIS: BandAxis = "parent";

/**
 * Whether a region boundary runs ABOVE this row, and on which axis.
 *
 * Only the CLOSING side of a boundary is drawn. A run that opens by descending
 * into the row above it — a container's first child, a section header's first
 * member — is already marked by the indent, and banding it too would put a rule
 * under every parent in the table. What has no other marking is the step back
 * out: the row where one list has ended and a different one resumes at the same
 * level or shallower.
 *
 * A row in NO region (a container the view fabricated) is in no run either, so
 * it opens a boundary against whatever it follows — which is what puts the band
 * between a lens's sections.
 *
 * The axis comes from EITHER side: the band names the seam, and a seam a queue
 * is on either side of is a queue seam. Reading only the row below would leave a
 * queue's last boundary — the one where it ends — drawn as an ordinary one.
 *
 * PRECONDITION: `previous` is the row immediately above `row` in a depth-first
 * flatten, so `row.depth > previous.depth` can only mean `row` is inside
 * `previous`. `BandRow` accepts any `{depth, region}` pair, and passed a list
 * not in that shape — a windowed list where `previous` is not the visually
 * adjacent row, a lens emitting siblings at unequal depths — the guard
 * SUPPRESSES real boundaries rather than reporting them.
 *
 * Two consequences of closing-side-only are accepted rather than overlooked.
 * "No rule" means both "same list" and "a list that opened by descending", so a
 * run whose neighbor above is its own parent is unmarked; and a rule can fall in
 * the middle of what one reorder list spans, where a deeper run sits between two
 * of its members. Both are ordinary outline behavior, and the alternative —
 * banding the opening side too — puts a rule under every parent in the table.
 */
export function regionBandAt(row: BandRow, previous: BandRow | null): BandAxis | null {
  if (previous === null) return null;
  if (row.depth > previous.depth) return null;
  if (sameRegion(row.region, previous.region)) return null;
  // Seeded from the first named side rather than from the neutral value, so the
  // ranking is over the axes actually present. Seeding at `NEUTRAL_AXIS` would
  // make it a floor as well as a default: any axis ranked at or below it could
  // never win, which is not a property the rank table means to carry.
  let axis: BandAxis | null = null;
  for (const side of [row.region?.axis, previous.region?.axis]) {
    if (side !== undefined && (axis === null || AXIS_RANK[side] > AXIS_RANK[axis])) axis = side;
  }
  return axis ?? NEUTRAL_AXIS;
}
