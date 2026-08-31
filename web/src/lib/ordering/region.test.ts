import { describe, it, expect } from "vitest";
// Vite `?raw` import: the module's own source as a string (typed via
// vite/client), following fouc-guard.test.ts — it keeps node built-ins out, so
// svelte-check stays clean without @types/node.
import regionSource from "./region.ts?raw";
import { BY_ID, commonRegion, describeRegion, sameRegion, scopeOf, spellId } from "./region";
import type { Region } from "./region";

const rootGroup: Region = { axis: "parent", parentId: null };
const underE1: Region = { axis: "parent", parentId: "E1" };
const underE2: Region = { axis: "parent", parentId: "E2" };
const queueM1: Region = { axis: "milestone", milestoneId: "M1" };
const queueM2: Region = { axis: "milestone", milestoneId: "M2" };

describe("scopeOf", () => {
  it("names the wire scope of each axis", () => {
    expect(scopeOf(rootGroup)).toBe("PARENT");
    expect(scopeOf(underE1)).toBe("PARENT");
    expect(scopeOf(queueM1)).toBe("MILESTONE");
  });
});

describe("sameRegion", () => {
  it("holds for two parent regions naming the same parent, including the root", () => {
    expect(sameRegion(underE1, { axis: "parent", parentId: "E1" })).toBe(true);
    expect(sameRegion(rootGroup, { axis: "parent", parentId: null })).toBe(true);
  });

  it("fails for two parent regions naming different parents", () => {
    expect(sameRegion(underE1, underE2)).toBe(false);
    expect(sameRegion(rootGroup, underE1)).toBe(false);
  });

  it("holds for two milestone regions naming the same queue, fails for different ones", () => {
    expect(sameRegion(queueM1, { axis: "milestone", milestoneId: "M1" })).toBe(true);
    expect(sameRegion(queueM1, queueM2)).toBe(false);
  });

  it("fails across axes even when the two ids coincide", () => {
    // Valid data cannot produce this pair — a milestone takes no children
    // (`VALID_CHILD_TYPES.milestone` is `[]`), so no `parentId` ever names one.
    // A hand-authored store can, and `order` and `milestoneOrder` are separate
    // keys either way, so comparing ids without the axis would call two lists one.
    expect(sameRegion({ axis: "parent", parentId: "M1" }, queueM1)).toBe(false);
  });

  it("treats null as no region at all, matching nothing — another null included", () => {
    expect(sameRegion(null, underE1)).toBe(false);
    expect(sameRegion(underE1, null)).toBe(false);
    // Two rows in no orderable list are not thereby in one together: a single
    // reorderNib can position neither, let alone both against each other.
    expect(sameRegion(null, null)).toBe(false);
  });
});

describe("commonRegion", () => {
  it("returns null for an empty input", () => {
    expect(commonRegion([])).toBeNull();
  });

  it("returns the region of a single input", () => {
    expect(commonRegion([queueM1])).toEqual(queueM1);
    expect(commonRegion([rootGroup])).toEqual(rootGroup);
  });

  it("returns the shared region when every input agrees", () => {
    expect(commonRegion([underE1, { axis: "parent", parentId: "E1" }, underE1])).toEqual(underE1);
  });

  it("returns null when the inputs differ, within an axis or across them", () => {
    expect(commonRegion([underE1, underE2])).toBeNull();
    expect(commonRegion([underE1, queueM1])).toBeNull();
  });

  it("returns null when any input has no region, wherever it sits", () => {
    expect(commonRegion([null])).toBeNull();
    expect(commonRegion([null, underE1])).toBeNull();
    expect(commonRegion([underE1, null])).toBeNull();
    expect(commonRegion([underE1, underE1, null])).toBeNull();
  });

  // Every case above puts its divergence at an end, so an implementation that
  // only compared the first and last input would satisfy all of them — and did,
  // when that mutant was run against this file. A multi-select drag is exactly
  // where the middle of the list matters.
  it("returns null when a middle input diverges though the ends agree", () => {
    expect(commonRegion([underE1, underE2, underE1])).toBeNull();
    expect(commonRegion([underE1, null, underE1])).toBeNull();
    expect(commonRegion([queueM1, queueM2, queueM1])).toBeNull();
  });
});

describe("describeRegion", () => {
  it("names each axis's list as a phrase that follows a verb", () => {
    expect(`Reorder in ${describeRegion(rootGroup, BY_ID)}`).toBe("Reorder in the top level");
    expect(`Reorder in ${describeRegion(underE1, BY_ID)}`).toBe("Reorder in the children of E1");
    expect(`Reorder in ${describeRegion(queueM1, BY_ID)}`).toBe("Reorder in the M1 queue");
  });

  const titles: Record<string, string> = { E1: "Epic one", M1: "Q3 Launch" };
  const nameOf = (id: string) => titles[id];

  it("spells an id with the namer's title where it has one", () => {
    expect(`Reorder in ${describeRegion(underE1, nameOf)}`).toBe("Reorder in the children of Epic one");
    expect(`Reorder in ${describeRegion(queueM1, nameOf)}`).toBe("Reorder in the Q3 Launch queue");
  });

  it("keeps the id for an id the namer cannot place, on either axis", () => {
    expect(describeRegion(underE2, nameOf)).toBe("the children of E2");
    expect(describeRegion(queueM2, nameOf)).toBe("the M2 queue");
  });

  it("keeps the id for an EMPTY title rather than leaving a hole in the phrase", () => {
    // A nib with no title is reachable — the field is a plain string on the wire
    // — and "the  queue" reads as a missing word rather than as an unnamed nib.
    expect(describeRegion(queueM1, () => "")).toBe("the M1 queue");
  });

  it("never asks the namer about the root group, which names no nib", () => {
    // `parentId: null` IS the group; there is no id to spell, so a namer that
    // throws on being called at all must still get a phrase back.
    expect(
      describeRegion(rootGroup, () => {
        throw new Error("the root group has no id to name");
      }),
    ).toBe("the top level");
  });
});

describe("spellId", () => {
  // The same fallback ladder describeRegion applies inside its phrases, exported
  // so a caller naming a nib BESIDE a region reads the namer identically.
  it("takes the title, and falls back to the id for missing, empty and BY_ID", () => {
    expect(spellId("E1", (id) => (id === "E1" ? "Epic one" : undefined))).toBe("Epic one");
    expect(spellId("E1", () => undefined)).toBe("E1");
    expect(spellId("E1", () => "")).toBe("E1");
    expect(spellId("E1", BY_ID)).toBe("E1");
  });
});

// describeRegion's title story is an injected `RegionNamer`, not an import, and
// that rests on the module having no nib to read a title off — a claim about its
// import list and nothing else. `web/` has no eslint, no dependency-cruiser and
// no import-boundary config, so the source is read here — the obvious way to add
// a title is to import the row type, and that would turn the doc into a false
// premise silently.
describe("region.ts import isolation", () => {
  it("imports exactly one module, the generated OrderScope", () => {
    const importLines = regionSource.split("\n").filter((line) => line.startsWith("import"));
    expect(importLines).toEqual(['import type { OrderScope } from "../gql/graphql";']);
    // A multi-line import still begins a line with `import`, so the equality
    // above is what catches it — its first line alone would not match. These
    // two cover what the line filter misses: a statement not starting a line,
    // and a dynamic `import(...)`.
    expect(regionSource.match(/\bfrom\s+"/g)).toHaveLength(1);
    expect(regionSource).not.toMatch(/\bimport\s*\(/);
  });
});
