import { describe, it, expect } from "vitest";
import { VIEW_SHAPE_CONTRACT } from "./generated/viewShapeContract";
import type { ShapeCase, ShapeCaseGroup, ShapeCaseNib, ShapeCaseRow } from "./generated/viewShapeContract";
import { makeViewSpine } from "./viewSpine";
import { createAreaVocabulary } from "./areas";
import { isSyntheticRowId } from "./tree";
import type { RowData, TableData } from "./tableData";
import { sameRegion } from "./ordering/region";
import type { Region } from "./ordering/region";
import { planDrop } from "./ordering/dropPlan";
import { collectDescendantIds } from "./dropZone";
import { VIEW_LEVELS } from "./types";
import type { NibFilter, TreeTableNib, ViewLevel } from "./types";

function tableNib(n: ShapeCaseNib): TreeTableNib {
  return {
    ...n,
    tags: [...n.tags],
    createdAt: "",
    updatedAt: "",
    blockingIds: [],
    blockedByIds: [],
    etag: "",
  };
}

function nibFilter(c: ShapeCase): NibFilter {
  const filter: NibFilter = {};
  for (const [key, values] of Object.entries(c.filter)) {
    filter[key as keyof typeof c.filter] = [...values];
  }
  return filter;
}

function levelOf(c: ShapeCase): ViewLevel {
  const level = VIEW_LEVELS.find((l) => l === c.level);
  if (level === undefined) throw new Error(`case ${JSON.stringify(c.name)} names no ViewLevel: ${c.level}`);
  return level;
}

function groupOf(region: Region | null): ShapeCaseGroup | null {
  if (region === null) return null;
  switch (region.axis) {
    case "parent":
      return { axis: "parent", id: region.parentId ?? "" };
    case "milestone":
      return { axis: "milestone", id: region.milestoneId };
  }
}

/** Projects a web row onto the fields a case states. */
function caseRow(row: RowData): ShapeCaseRow {
  return {
    id: isSyntheticRowId(row.nib.id) ? null : row.nib.id,
    depth: row.depth,
    section: row.drawsSection?.key ?? row.section?.key ?? null,
    header: row.drawsSection !== null,
    dimmed: row.dimmed,
    hasChildren: row.hasChildren,
    group: groupOf(row.region),
  };
}

/**
 * Whether a drop of `from` before `to` is a position write within `from`'s own
 * ordering group. The cases' `reorders` hold viewshape.StaysInGroup to these
 * answers for the pairs they list.
 */
function staysInGroup(data: TableData, fromId: string, toId: string): boolean {
  const rowsById = new Map(data.rows.map((r) => [r.nib.id, r] as const));
  const from = rowsById.get(fromId);
  const to = rowsById.get(toId);
  if (from === undefined || to === undefined) throw new Error(`no row shows ${from === undefined ? fromId : toId}`);
  const plan = planDrop({
    draggedIds: [fromId],
    rowsById,
    draggedRowsById: rowsById,
    target: to,
    zone: "before",
    descendantIds: collectDescendantIds([fromId], data.rows),
    containment: data.containment,
  });
  return plan.ok && plan.kind === "position" && sameRegion(plan.region, from.region);
}

/**
 * The web half of the view-shaping parity table; the Go half is
 * internal/viewshapecontract. It is the only consumer of the generated cases.
 */
describe("Go↔TS parity for view shaping", () => {
  it.each(VIEW_SHAPE_CONTRACT.map((c) => [c.name, c] as const))("%s", (_name, c) => {
    const data = makeViewSpine(createAreaVocabulary(c.areas)).buildTableData(
      c.nibs.map(tableNib),
      nibFilter(c),
      levelOf(c),
      new Set(c.collapsed),
      null,
    );
    expect(data.rows.map(caseRow)).toEqual(c.rows);
    expect(c.reorders.map((r) => ({ ...r, stays: staysInGroup(data, r.from, r.to) }))).toEqual(c.reorders);
  });
});
