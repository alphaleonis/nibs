import { describe, it, expect } from "vitest";
import { dropTreatment, regionBandAt, type BandRow } from "./regionBand";
import type { Region } from "./region";

const TOP: Region = { axis: "parent", parentId: null };
const UNDER_E1: Region = { axis: "parent", parentId: "E1" };
const UNDER_E2: Region = { axis: "parent", parentId: "E2" };
const QUEUE_M1: Region = { axis: "milestone", milestoneId: "M1" };
const QUEUE_M2: Region = { axis: "milestone", milestoneId: "M2" };

const row = (depth: number, region: Region | null): BandRow => ({ depth, region });

describe("regionBandAt", () => {
  it("draws nothing above the first row, which borders nothing", () => {
    expect(regionBandAt(row(0, TOP), null)).toBeNull();
  });

  it("draws nothing between two rows of one list", () => {
    expect(regionBandAt(row(1, UNDER_E1), row(1, UNDER_E1))).toBeNull();
    expect(regionBandAt(row(0, TOP), row(0, TOP))).toBeNull();
    expect(regionBandAt(row(2, QUEUE_M1), row(2, QUEUE_M1))).toBeNull();
  });

  it("draws nothing where a new list OPENS by descending — the indent says it", () => {
    // A container's first child, and a section header's first queue member. Both
    // start a different region one level deeper, and banding them would put a
    // rule under every parent in the table.
    expect(regionBandAt(row(1, UNDER_E1), row(0, TOP))).toBeNull();
    expect(regionBandAt(row(1, QUEUE_M1), row(0, TOP))).toBeNull();
  });

  it("draws a band where a list CLOSES and another resumes at or above its level", () => {
    // Stepping back out of a subtree: the row below is a sibling of the closed
    // run's container, so nothing else marks the change of list.
    expect(regionBandAt(row(1, UNDER_E1), row(2, UNDER_E2))).toBe("parent");
    expect(regionBandAt(row(0, TOP), row(1, UNDER_E1))).toBe("parent");
    // And two neighbors at ONE level in different lists — a promoted header
    // beside a loose row.
    expect(regionBandAt(row(1, UNDER_E1), row(1, UNDER_E2))).toBe("parent");
  });

  it("names the band for the queue on EITHER side of the seam", () => {
    // The row below is a queue member and the run above was not...
    expect(regionBandAt(row(0, QUEUE_M1), row(1, UNDER_E1))).toBe("milestone");
    // ...and the case that matters more, since it is where a queue ENDS: the run
    // above was the queue and the row below is not in it. Reading only the row
    // below would draw a queue's last boundary as an ordinary one.
    expect(regionBandAt(row(0, TOP), row(1, QUEUE_M1))).toBe("milestone");
    // Two queues meeting is a queue seam from both directions.
    expect(regionBandAt(row(1, QUEUE_M2), row(1, QUEUE_M1))).toBe("milestone");
  });

  it("bands a row in NO region against whatever it follows, and the reverse", () => {
    // A container the view fabricated is a member of nothing, so it continues no
    // run — which is what puts a band above a lens's section rows.
    expect(regionBandAt(row(0, null), row(0, TOP))).toBe("parent");
    expect(regionBandAt(row(0, TOP), row(0, null))).toBe("parent");
    // Two of them in a row are still two different sections (`sameRegion` holds
    // for no pair of nulls).
    expect(regionBandAt(row(0, null), row(0, null))).toBe("parent");
  });

  it("does not band a row that descends INTO a fabricated container", () => {
    expect(regionBandAt(row(1, TOP), row(0, null))).toBeNull();
  });
});

describe("dropTreatment", () => {
  it("colors a position by its axis", () => {
    expect(dropTreatment({ kind: "position", region: TOP })).toBe("parent");
    expect(dropTreatment({ kind: "position", region: UNDER_E1 })).toBe("parent");
    expect(dropTreatment({ kind: "position", region: QUEUE_M1 })).toBe("queue");
  });

  it("gives an assignment a treatment of its own", () => {
    // The whole point of the switch: an assignment carries no region, so an
    // axis read would answer for it by default, and the default is the parent
    // axis — the sibling reorder it sits on the same pixel as.
    expect(dropTreatment({ kind: "assign" })).toBe("assign");
  });

  it("colors nothing when nothing is accepted", () => {
    expect(dropTreatment(null)).toBeNull();
    expect(dropTreatment(undefined)).toBeNull();
  });
});
