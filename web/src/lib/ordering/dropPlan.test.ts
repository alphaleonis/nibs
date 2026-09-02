import { describe, it, expect } from "vitest";
// Vite `?raw` import: the module's own source as a string (typed via
// vite/client), following region.test.ts — it keeps node built-ins out, so
// svelte-check stays clean without @types/node.
import dropPlanSource from "./dropPlan.ts?raw";
import {
  entryRegionOf,
  planDrop,
  refusalAction,
  type DropIndicator,
  type DropPlan,
  type DropRefusalReason,
} from "./dropPlan";
import type { Region } from "./region";
import { buildContainmentIndex } from "../containment";
import { collectDescendantIds } from "../dropZone";
import type { DropZone } from "../drag.svelte";
import { batch, reorderChain, reorderNib, reparentAndReorder, sequence, setParent, updateNib } from "../mutations/commands";
import type { AnyCommand, CommandResult } from "../mutations/types";
import { buildShapedTableData, rowRegion, type RowData, type RowSection } from "../tableData";
import { viewShapeFor } from "../tree";
import { canHaveChildren } from "../typeHierarchy";
import type { TreeNode, TreeTableNib } from "../types";

function makeNib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    id: "nib-001",
    title: "Test",
    status: "todo",
    type: "task",
    priority: "normal",
    estimate: "",
    tags: [],
    createdAt: "",
    updatedAt: "",
    parentId: null,
    milestone: "",
    milestoneOrder: "",
    area: "",
    blockingIds: [],
    blockedByIds: [],
    etag: "etag-test",
    ...overrides,
  };
}

const QUEUE_M1: Region = { axis: "milestone", milestoneId: "M1" };
const QUEUE_M2: Region = { axis: "milestone", milestoneId: "M2" };
const TOP_LEVEL: Region = { axis: "parent", parentId: null };
const BACKLOG_ID = "/__backlog__";

/**
 * One row, with `region` computed by production's own rule rather than restated:
 * `enclosing` is the declaration the row's container passed down, exactly as
 * `flatten` threads it.
 */
function makeRow(
  nib: TreeTableNib,
  opts: {
    enclosing?: Region | null;
    drawsSection?: RowSection | null;
    section?: RowSection | null;
    parentNib?: TreeTableNib | null;
    displayParentId?: string | null;
  } = {},
): RowData {
  return {
    nib,
    depth: 0,
    hasChildren: false,
    dimmed: false,
    parentNib: opts.parentNib ?? null,
    displayParentId: opts.displayParentId ?? null,
    region: rowRegion(nib.id, nib.parentId, opts.enclosing ?? null),
    drawsSection: opts.drawsSection ?? null,
    section: opts.section ?? null,
  };
}

/** A section whose rows order in one queue, in the shape the milestone lens's
 *  `meaning` returns: one region answering both halves. */
function queueSection(key: string, region: Region): RowSection {
  return { key, onEnter: { kind: "region", region } };
}

/**
 * A grouped view in the shape Phase 8 needs: milestone sections whose headers
 * declare their queue, a fabricated Backlog section whose members fall back to
 * their own parent groups, and two sections that DECLARE a parent-axis group —
 * the case in which a row's region and its own `nib.parentId` come apart.
 *
 *   M1 (header, declares the M1 queue)
 *     E1  epic   ─ queue member          T1  task ─ child of E1
 *     E2  epic   ─ queue member
 *     E4  epic   ─ queue member
 *     QT  task   ─ queue member
 *   M2 (header, declares the M2 queue)
 *     E3  epic   ─ queue member
 *   Backlog (fabricated container, declares nothing)
 *     B1, B2  tasks at the top level
 *     B3      epic at the top level
 *     FT      feature at the top level    B5  task ─ child of FT
 *     PH      feature promoted from the hidden container E9, which its row
 *             carries as `parentNib` — the population cross-parent drops unlock
 *     PT      task promoted from the hidden container F9; F9 has no row, so
 *             only `parentNib` can answer its type check
 *     NP      task whose real parent H9 has no row AND is not carried as a
 *             `parentNib`, so nothing here can answer for it at all
 *     CY1     epic, real parent CY2      CY2  epic ─ real parent CY1, drawn
 *                                             as CY1's own child (a severed
 *                                             cycle: the container is INSIDE
 *                                             the target's subtree)
 *     SC      epic whose real parent is ITSELF — the one-member cycle, severed
 *             the same way, so its own children group is the row itself and no
 *             containment hop reaches it
 *   RS (header, declares the ROOT group for its rows)
 *     DR1  task whose real parent is null, so declaration and parent agree
 *     DR2  task whose real parent is B3, so they do NOT — one region, two
 *          server groups
 *   HS (header, declares the children of E9, which has no row here)
 */
const E9 = makeNib({ id: "E9", type: "epic", title: "Hidden container" });
const F9 = makeNib({ id: "F9", type: "feature", title: "Hidden feature" });
const FT_NIB = makeNib({ id: "FT", type: "feature", title: "Feature" });
const E1_NIB = makeNib({ id: "E1", type: "epic", title: "Epic one", milestone: "M1" });
const B3_NIB = makeNib({ id: "B3", type: "epic", title: "Backlog epic" });
const CY1_NIB = makeNib({ id: "CY1", type: "epic", title: "Cycle one", parentId: "CY2" });
const CY2_NIB = makeNib({ id: "CY2", type: "epic", title: "Cycle two", parentId: "CY1" });
const SC_NIB = makeNib({ id: "SC", type: "epic", title: "Cycle self", parentId: "SC" });
const HIDDEN_CHILDREN: Region = { axis: "parent", parentId: "E9" };

const ROWS: RowData[] = [
  makeRow(makeNib({ id: "M1", type: "milestone", title: "v1.0" }), { drawsSection: queueSection("M1", QUEUE_M1) }),
  makeRow(E1_NIB, { enclosing: QUEUE_M1 }),
  makeRow(makeNib({ id: "T1", type: "task", title: "Task one", parentId: "E1" }), { parentNib: E1_NIB }),
  makeRow(makeNib({ id: "E2", type: "epic", title: "Epic two", milestone: "M1" }), { enclosing: QUEUE_M1 }),
  makeRow(makeNib({ id: "E4", type: "epic", title: "Epic four", milestone: "M1" }), { enclosing: QUEUE_M1 }),
  makeRow(makeNib({ id: "QT", type: "task", title: "Queued task", milestone: "M1" }), { enclosing: QUEUE_M1 }),
  makeRow(makeNib({ id: "M2", type: "milestone", title: "v2.0" }), { drawsSection: queueSection("M2", QUEUE_M2) }),
  makeRow(makeNib({ id: "E3", type: "epic", title: "Epic three", milestone: "M2" }), { enclosing: QUEUE_M2 }),
  makeRow(makeNib({ id: BACKLOG_ID, type: "", title: "Backlog" }), {
    drawsSection: { key: BACKLOG_ID, onEnter: { kind: "byRow" } },
  }),
  makeRow(makeNib({ id: "B1", type: "task", title: "Backlog one" })),
  makeRow(makeNib({ id: "B2", type: "task", title: "Backlog two" })),
  makeRow(B3_NIB),
  makeRow(FT_NIB),
  makeRow(makeNib({ id: "B5", type: "task", title: "Under the feature", parentId: "FT" }), { parentNib: FT_NIB }),
  makeRow(makeNib({ id: "PH", type: "feature", title: "Promoted header", parentId: "E9" }), { parentNib: E9 }),
  makeRow(makeNib({ id: "PT", type: "task", title: "Promoted task", parentId: "F9" }), { parentNib: F9 }),
  makeRow(makeNib({ id: "NP", type: "task", title: "No parent nib", parentId: "H9" })),
  makeRow(CY1_NIB, { parentNib: CY2_NIB }),
  makeRow(CY2_NIB, { parentNib: CY1_NIB, displayParentId: "CY1" }),
  makeRow(SC_NIB, { parentNib: SC_NIB }),
  makeRow(makeNib({ id: "RS", type: "milestone", title: "Root section" }), { drawsSection: queueSection("RS", TOP_LEVEL) }),
  makeRow(makeNib({ id: "DR1", type: "task", title: "Declared one" }), { enclosing: TOP_LEVEL }),
  makeRow(makeNib({ id: "DR2", type: "task", title: "Declared two", parentId: "B3" }), {
    enclosing: TOP_LEVEL,
    parentNib: B3_NIB,
  }),
  makeRow(makeNib({ id: "HS", type: "milestone", title: "Hidden section" }), { drawsSection: queueSection("HS", HIDDEN_CHILDREN) }),
];

const ROWS_BY_ID = new Map(ROWS.map((r) => [r.nib.id, r]));

/**
 * The same view, as the forest the table was flattened FROM — which the rows
 * above cannot supply, since a section holds its members by arrangement and no
 * row records that. Stated once here and asserted total against `ROWS` below, so
 * a row added to the fixture without a place in it fails rather than quietly
 * dropping out of the index.
 */
const CONTAINS: Record<string, string[]> = {
  M1: ["E1", "E2", "E4", "QT"],
  E1: ["T1"],
  M2: ["E3"],
  [BACKLOG_ID]: ["B1", "B2", "B3", "FT", "PH", "PT", "NP", "CY1", "SC"],
  FT: ["B5"],
  CY1: ["CY2"],
  RS: ["DR1", "DR2"],
};
const ROOT_IDS = ["M1", "M2", BACKLOG_ID, "RS", "HS"];

function fixtureNode(id: string): TreeNode<TreeTableNib> {
  const row = ROWS_BY_ID.get(id);
  if (row === undefined) throw new Error(`no fixture row ${id}`);
  return { nib: row.nib, children: (CONTAINS[id] ?? []).map(fixtureNode), depth: 0 };
}

const CONTAINMENT = buildContainmentIndex(ROOT_IDS.map(fixtureNode));

function planFor(draggedIds: string[], targetId: string, zone: DropZone): DropPlan {
  const target = ROWS_BY_ID.get(targetId);
  if (target === undefined) throw new Error(`no fixture row ${targetId}`);
  return planDrop({
    draggedIds,
    rowsById: ROWS_BY_ID,
    // Same map: these cases vary the drag, not what the view did mid-gesture.
    // The one case that pulls them apart builds its own pair.
    draggedRowsById: ROWS_BY_ID,
    target,
    zone,
    // Production's own collector, over the fixture rows.
    descendantIds: collectDescendantIds(draggedIds, ROWS),
    containment: CONTAINMENT,
  });
}

/**
 * A sequence's chained steps are closures, which compare by reference. Resolving
 * each against a stub previous result makes the whole command comparable — and
 * asserts that a chained step carries its axis, which is the half a `kind`
 * check would miss.
 */
function resolveSteps(command: AnyCommand): AnyCommand {
  if (command.kind !== "sequence") return command;
  return {
    kind: "sequence",
    steps: command.steps.map((step, i) =>
      typeof step === "function"
        ? step({ ok: true, data: { reorderNib: { id: `PREV${i}` } } } satisfies CommandResult)
        : step,
    ),
  };
}

/** `resolveSteps` over a slot that may be empty — a refusal offering no write. */
function resolveOptionalSteps(command: AnyCommand | undefined): AnyCommand | undefined {
  return command === undefined ? undefined : resolveSteps(command);
}

type Expected =
  | { ok: true; region: Region; indicator: DropIndicator; command: AnyCommand }
  // `region`, `actionLabel` and `actionCommand` are asserted for every refusal,
  // present or absent: without a slot here the table cannot see them, and
  // deleting `region` from a refusal site left the whole suite green.
  | {
      ok: false;
      reason: DropRefusalReason;
      actionLabel?: string;
      actionCommand?: AnyCommand;
      region?: Region;
    };

interface Case {
  name: string;
  drag: string[];
  target: string;
  zone: DropZone;
  expected: Expected;
}

const CASES: Case[] = [
  // --- Parent axis, one region: today's sibling reorder, unchanged. ---
  {
    name: "single row before a sibling at the top level",
    drag: ["B1"],
    target: "B2",
    zone: "before",
    expected: { ok: true, region: TOP_LEVEL, indicator: "before", command: reorderNib("B1", { beforeId: "B2" }) },
  },
  {
    name: "several rows before a sibling chain after one another",
    drag: ["B1", "B2"],
    target: "B3",
    zone: "before",
    expected: { ok: true, region: TOP_LEVEL, indicator: "before", command: reorderChain(["B1", "B2"], "B3", "before") },
  },
  {
    name: "the bottom edge of a leaf still means after it",
    drag: ["B1"],
    target: "B2",
    zone: "after",
    expected: { ok: true, region: TOP_LEVEL, indicator: "after", command: reorderNib("B1", { afterId: "B2" }) },
  },

  // --- Parent axis, container entry. ---
  {
    name: "the bottom edge of an epic enters it",
    drag: ["B1"],
    target: "B3",
    zone: "after",
    expected: {
      ok: true,
      region: { axis: "parent", parentId: "B3" },
      indicator: "into",
      command: batch([setParent("B1", "B3")]),
    },
  },
  {
    name: "the middle of an epic enters it too",
    drag: ["B1"],
    target: "B3",
    zone: "reparent",
    expected: {
      ok: true,
      region: { axis: "parent", parentId: "B3" },
      indicator: "into",
      command: batch([setParent("B1", "B3")]),
    },
  },
  {
    name: "a container declaring the root group takes its rows to the top level",
    drag: ["B1"],
    target: "RS",
    zone: "reparent",
    expected: { ok: true, region: TOP_LEVEL, indicator: "into", command: batch([setParent("B1", null)]) },
  },
  {
    name: "several rows entering a container reparent together",
    drag: ["B1", "B2"],
    target: "B3",
    zone: "after",
    expected: {
      ok: true,
      region: { axis: "parent", parentId: "B3" },
      indicator: "into",
      command: batch([setParent("B1", "B3"), setParent("B2", "B3")]),
    },
  },
  {
    name: "the middle of a leaf is refused",
    drag: ["B1"],
    target: "B2",
    zone: "reparent",
    expected: { ok: false, reason: "invalid-parent-type" },
  },
  {
    name: "entering a container the type hierarchy refuses",
    drag: ["B3"],
    target: "FT",
    zone: "reparent",
    expected: { ok: false, reason: "invalid-parent-type", region: { axis: "parent", parentId: "FT" } },
  },

  // --- Parent axis, two regions: a reparent expresses the move. ---
  {
    name: "across parent groups the drop reparents and positions",
    drag: ["B1"],
    target: "T1",
    zone: "before",
    expected: {
      ok: true,
      region: { axis: "parent", parentId: "E1" },
      indicator: "before",
      command: reparentAndReorder(["B1"], "E1", "T1", "before"),
    },
  },
  {
    name: "a promoted header's region names the container the lens hid",
    drag: ["B1"],
    target: "PH",
    zone: "before",
    expected: {
      ok: true,
      region: { axis: "parent", parentId: "E9" },
      indicator: "before",
      command: reparentAndReorder(["B1"], "E9", "PH", "before"),
    },
  },
  {
    name: "across parent groups the destination's type still decides",
    drag: ["B3"],
    target: "B5",
    zone: "before",
    expected: { ok: false, reason: "invalid-parent-type", region: { axis: "parent", parentId: "FT" } },
  },
  {
    // The container has no row here, so only `parentNib` can answer for it —
    // and it has to: reading it as unconstrained would let this drop through.
    name: "a destination container the lens hid still refuses on its type",
    drag: ["B3"],
    target: "PT",
    zone: "before",
    expected: { ok: false, reason: "invalid-parent-type", region: { axis: "parent", parentId: "F9" } },
  },
  {
    name: "several rows across parent groups chain after the first",
    drag: ["B1", "B2", "DR1"],
    target: "T1",
    zone: "before",
    expected: {
      ok: true,
      region: { axis: "parent", parentId: "E1" },
      indicator: "before",
      command: reparentAndReorder(["B1", "B2", "DR1"], "E1", "T1", "before"),
    },
  },
  {
    name: "a child dragged before a root-level row reparents to the root",
    drag: ["T1"],
    target: "B1",
    zone: "before",
    expected: {
      ok: true,
      region: TOP_LEVEL,
      indicator: "before",
      command: reparentAndReorder(["T1"], null, "B1", "before"),
    },
  },
  {
    // Neither a row nor a `parentNib` answers for H9, so the type hierarchy
    // cannot be consulted — and an unanswerable question is a refusal, not a
    // pass. Folding it onto "the root group, unconstrained" is what let an
    // unchecked destination through, and the type check then silently did not
    // run at all.
    name: "a destination nothing in the view can name is refused, not waved through",
    drag: ["B1"],
    target: "NP",
    zone: "before",
    expected: { ok: false, reason: "unknown-destination", region: { axis: "parent", parentId: "H9" } },
  },
  {
    name: "a declared entry region naming a rowless container is refused the same way",
    drag: ["B1"],
    target: "HS",
    zone: "reparent",
    expected: { ok: false, reason: "unknown-destination", region: { axis: "parent", parentId: "E9" } },
  },

  // --- Declared parent-axis regions: region equality is not real-parent
  //     equality, and only the latter licenses a bare reorder. ---
  {
    // Both rows are in RS's declared ROOT region, so `sameRegion` says yes —
    // but the ANCHOR's real parent is B3, and a reorder is grouped by the
    // subject's own parent whether or not the subject is reparented first.
    name: "a target drawn in a group it is not a member of anchors nothing",
    drag: ["DR1"],
    target: "DR2",
    zone: "before",
    expected: { ok: false, reason: "anchor-not-in-destination", region: TOP_LEVEL },
  },
  {
    // The mirror: the anchor IS a root member, and the divergent row is the
    // SUBJECT — which a reparent moves, so the drop stands.
    name: "a row whose declared region outruns its own parent is reparented to match it",
    drag: ["DR2"],
    target: "DR1",
    zone: "before",
    expected: {
      ok: true,
      region: TOP_LEVEL,
      indicator: "before",
      command: reparentAndReorder(["DR2"], null, "DR1", "before"),
    },
  },
  {
    // The dragged rows share a declared region but not a real parent, so there
    // is no single sibling set for a bare reorder to be grouped by. Only the
    // dragged set's OWN parents decide that — `sameRegion` would say yes here.
    name: "a selection sharing a declared region but not a real parent is reparented, not reordered",
    drag: ["DR1", "DR2"],
    target: "B1",
    zone: "before",
    expected: {
      ok: true,
      region: TOP_LEVEL,
      indicator: "before",
      command: reparentAndReorder(["DR1", "DR2"], null, "B1", "before"),
    },
  },
  {
    // A severed cycle: CY1's real parent CY2 is drawn as CY1's OWN child, so
    // reparenting into it puts the row below the line the indicator drew.
    name: "a destination drawn inside the target's own subtree is refused",
    drag: ["B1"],
    target: "CY1",
    zone: "before",
    expected: { ok: false, reason: "destination-inside-target", region: { axis: "parent", parentId: "CY2" } },
  },
  {
    // The same shape with the cycle one member long: SC's destination group is
    // SC itself, so no containment hop reaches it and only the identity arm
    // refuses. Accepting it reparents the dragged row INTO the row the
    // indicator drew a line above.
    name: "a destination that IS the target is refused (one-member cycle)",
    drag: ["B1"],
    target: "SC",
    zone: "before",
    expected: { ok: false, reason: "destination-inside-target", region: { axis: "parent", parentId: "SC" } },
  },

  // --- Milestone axis. ---
  {
    name: "one queue member before another reorders on the milestone axis",
    drag: ["E1"],
    target: "E2",
    zone: "before",
    expected: {
      ok: true,
      region: QUEUE_M1,
      indicator: "before",
      command: reorderNib("E1", { beforeId: "E2", scope: "MILESTONE" }),
    },
  },
  {
    name: "several queue members chain on the milestone axis, not the parent one",
    drag: ["E1", "E2"],
    target: "E4",
    zone: "before",
    expected: {
      ok: true,
      region: QUEUE_M1,
      indicator: "before",
      command: sequence([
        reorderNib("E1", { beforeId: "E4", scope: "MILESTONE" }),
        reorderNib("E2", { afterId: "PREV1", scope: "MILESTONE" }),
      ]),
    },
  },
  {
    // The bottom edge of a CONTAINER inside the queue the rows are already in
    // stays a queue reorder. Promoting it to a container entry makes the
    // destination parent-axis, which is refused either way — here by the type
    // hierarchy (no epic sits in an epic), and by the cross-axis policy
    // wherever the types would have allowed it. That costs half of a queue's
    // reorder gestures, and the position after its last row when that row is a
    // container.
    name: "the bottom edge of a queue member is an in-queue reorder, not an entry",
    drag: ["E1"],
    target: "E2",
    zone: "after",
    expected: {
      ok: true,
      region: QUEUE_M1,
      indicator: "after",
      command: reorderNib("E1", { afterId: "E2", scope: "MILESTONE" }),
    },
  },
  {
    name: "the trailing edge of a queue leaf anchors after it",
    drag: ["E1"],
    target: "QT",
    zone: "after",
    expected: {
      ok: true,
      region: QUEUE_M1,
      indicator: "after",
      command: reorderNib("E1", { afterId: "QT", scope: "MILESTONE" }),
    },
  },
  {
    name: "the bottom edge of a queue header means first in the queue",
    drag: ["E1"],
    target: "M1",
    zone: "after",
    expected: {
      ok: true,
      region: QUEUE_M1,
      indicator: "into",
      command: reorderNib("E1", { first: true, scope: "MILESTONE" }),
    },
  },
  {
    name: "the middle of a queue header means the same, though a milestone holds no children",
    drag: ["E1"],
    target: "M1",
    zone: "reparent",
    expected: {
      ok: true,
      region: QUEUE_M1,
      indicator: "into",
      command: reorderNib("E1", { first: true, scope: "MILESTONE" }),
    },
  },
  // --- A milestone dragged onto a milestone header, all three zones. The
  // queue under the cursor is one the dragged row can never be a member of
  // (`nibtypes.ValidateAxes`: "a milestone is a waypoint, not work"), so the
  // entry promotion must not swallow the header's own sibling reorder — and no
  // zone may answer with a reassignment the server would refuse. ---
  {
    name: "the top edge of a milestone header reorders the sections",
    drag: ["M2"],
    target: "M1",
    zone: "before",
    expected: { ok: true, region: TOP_LEVEL, indicator: "before", command: reorderNib("M2", { beforeId: "M1" }) },
  },
  {
    name: "the bottom edge of a milestone header reorders the sections too, rather than entering the queue",
    drag: ["M2"],
    target: "M1",
    zone: "after",
    expected: { ok: true, region: TOP_LEVEL, indicator: "after", command: reorderNib("M2", { afterId: "M1" }) },
  },
  {
    name: "the middle of a milestone header refuses as the type question it is, not as an assignment",
    drag: ["M2"],
    target: "M1",
    zone: "reparent",
    expected: { ok: false, reason: "invalid-parent-type" },
  },
  {
    // One move positions one group, so a queue that refuses ONE dragged row
    // refuses the drag — the whole selection falls back to the positioned drop.
    name: "a selection carrying a milestone stays out of the queue too",
    drag: ["M2", "B1"],
    target: "M1",
    zone: "after",
    expected: {
      ok: true,
      region: TOP_LEVEL,
      indicator: "after",
      command: reorderChain(["M2", "B1"], "M1", "after"),
    },
  },
  {
    name: "several rows entering a queue chain behind the first",
    drag: ["E1", "E2"],
    target: "M1",
    zone: "after",
    expected: {
      ok: true,
      region: QUEUE_M1,
      indicator: "into",
      command: sequence([
        reorderNib("E1", { first: true, scope: "MILESTONE" }),
        reorderNib("E2", { afterId: "PREV1", scope: "MILESTONE" }),
      ]),
    },
  },
  {
    // The remedy enters at the FRONT, exactly where the accepted in-queue form
    // of this gesture puts a row — an entry indicator names no neighbor.
    name: "entering a queue from outside it is an assignment, not a move",
    drag: ["B1"],
    target: "M1",
    zone: "after",
    expected: {
      ok: false,
      reason: "needs-assignment",
      actionLabel: "Assign to M1",
      actionCommand: sequence([
        updateNib("B1", { milestone: "M1" }),
        reorderNib("B1", { first: true, scope: "MILESTONE" }),
      ]),
      region: QUEUE_M1,
    },
  },
  {
    // Every assignment lands last in the target queue, so the position the drop
    // pointed at is a second write — and one the anchor can carry, since a
    // milestone section's own rows ARE that queue's members.
    name: "moving between two queues is a reassignment",
    drag: ["E3"],
    target: "E1",
    zone: "before",
    expected: {
      ok: false,
      reason: "needs-assignment",
      actionLabel: "Assign to M1",
      actionCommand: sequence([
        updateNib("E3", { milestone: "M1" }),
        reorderNib("E3", { beforeId: "E1", scope: "MILESTONE" }),
      ]),
      region: QUEUE_M1,
    },
  },
  {
    // A milestone is the one type no queue can hold, so the assignment behind
    // the button would be refused by the server every time. The refusal still
    // stands — the destination is a queue and the subject is not in it — it just
    // leads nowhere, which is the honest answer for a type that can never join.
    name: "a dragged milestone is refused a queue with no assignment offered",
    drag: ["M2"],
    target: "E1",
    zone: "before",
    expected: { ok: false, reason: "needs-assignment", region: QUEUE_M1 },
  },
  {
    // Assign-then-position PER ROW, not every assignment followed by every
    // position: the dispatcher stops a sequence at its first failure, and one
    // row that cannot be assigned would otherwise strand the rows before it
    // assigned but unpositioned — parked at the end of the queue rather than
    // where the drop pointed.
    name: "several rows joining a queue are assigned and positioned one row at a time",
    drag: ["B1", "B2"],
    target: "M1",
    zone: "after",
    expected: {
      ok: false,
      reason: "needs-assignment",
      actionLabel: "Assign to M1",
      actionCommand: sequence([
        updateNib("B1", { milestone: "M1" }),
        reorderNib("B1", { first: true, scope: "MILESTONE" }),
        updateNib("B2", { milestone: "M1" }),
        reorderNib("B2", { afterId: "B1", scope: "MILESTONE" }),
      ]),
      region: QUEUE_M1,
    },
  },
  {
    name: "ordering a queue member in a parent group needs the assignment cleared",
    drag: ["E1"],
    target: "B1",
    zone: "before",
    expected: { ok: false, reason: "needs-unassignment", region: TOP_LEVEL },
  },
  {
    // The MIDDLE, which is the gesture that really means "nest me under it" —
    // the bottom edge of the same row is an in-queue reorder above.
    name: "nesting a queue member under another one is refused for the opposite reason",
    drag: ["QT"],
    target: "E1",
    zone: "reparent",
    expected: { ok: false, reason: "needs-unassignment", region: { axis: "parent", parentId: "E1" } },
  },
  {
    // Both refusals apply — a queued source AND a destination no epic may sit
    // in. The type one is the honest answer: clearing the assignment, which the
    // other prescribes, discards the queue position and leaves the drop refused
    // anyway.
    name: "a queue member aimed at an impossible destination is told the destination is impossible",
    drag: ["E1"],
    target: "B5",
    zone: "before",
    expected: { ok: false, reason: "invalid-parent-type", region: { axis: "parent", parentId: "FT" } },
  },

  // --- Guards. ---
  {
    name: "a row cannot be dropped on itself",
    drag: ["B1"],
    target: "B1",
    zone: "before",
    expected: { ok: false, reason: "drop-on-self" },
  },
  {
    // The middle zone asks to enter the target, and B1 is a leaf that can be
    // entered by nothing — but the row IS the one being dragged, so the drag was
    // canceled rather than aimed anywhere. Reporting the destination's problem
    // here would describe a drop the user never asked for, which is why the
    // self/descendant answers are decided before the destination is.
    name: "releasing on the middle of the dragged row is a cancel, not a bad destination",
    drag: ["B1"],
    target: "B1",
    zone: "reparent",
    expected: { ok: false, reason: "drop-on-self" },
  },
  {
    name: "a row cannot be dropped into its own subtree",
    drag: ["FT"],
    target: "B5",
    zone: "before",
    expected: { ok: false, reason: "drop-on-descendant" },
  },
  {
    // The descendant half of "self/descendant is decided before the destination
    // is". B5 is a leaf, so the middle zone has no group to enter and the
    // destination computation refuses `invalid-parent-type` — but only from the
    // "reparent" zone. The `before` case above never reaches that branch, so it
    // holds under either ordering and cannot pin this on its own.
    name: "releasing on the middle of a descendant is a subtree refusal, not a bad destination",
    drag: ["FT"],
    target: "B5",
    zone: "reparent",
    expected: { ok: false, reason: "drop-on-descendant" },
  },
  {
    name: "a fabricated section is not a drop target",
    drag: ["B1"],
    target: BACKLOG_ID,
    zone: "after",
    expected: { ok: false, reason: "unorderable-target" },
  },
  {
    name: "a fabricated section has no position to move",
    drag: [BACKLOG_ID],
    target: "B1",
    zone: "before",
    expected: { ok: false, reason: "unorderable-source" },
  },
  {
    // Mixed with an orderable row, so the culprit has to be PICKED out of the
    // selection rather than being the only candidate — without that, this
    // reports the selection as merely spanning two groups.
    name: "a fabricated section is named as the culprit inside a mixed selection",
    drag: ["B1", BACKLOG_ID],
    target: "B2",
    zone: "before",
    expected: { ok: false, reason: "unorderable-source" },
  },
  {
    name: "a selection spanning two ordering groups is refused",
    drag: ["B1", "T1"],
    target: "B2",
    zone: "before",
    expected: { ok: false, reason: "mixed-source" },
  },
  {
    name: "a selected row the view does not show is refused separately",
    drag: ["GHOST", "B1"],
    target: "B2",
    zone: "before",
    expected: { ok: false, reason: "hidden-member" },
  },
  {
    name: "an empty drag is refused",
    drag: [],
    target: "B2",
    zone: "before",
    expected: { ok: false, reason: "no-source" },
  },
];

describe("planDrop", () => {
  it.each(CASES)("$name", ({ drag, target, zone, expected }) => {
    const plan = planFor(drag, target, zone);

    expect(plan.ok, plan.ok ? "" : plan.refusal.message).toBe(expected.ok);
    if (expected.ok) {
      if (!plan.ok) throw new Error("unreachable: the assertion above already failed");
      // Every case in this table plans a POSITION. The discriminant is asserted
      // rather than assumed, because the assign arm carries no `region` at all
      // and reading one off it would not compile — see sectionEntry.test.ts for
      // that arm.
      if (plan.kind !== "position") throw new Error(`expected a position plan, got ${plan.kind}`);
      // The four together: a plan whose indicator and command disagree is the
      // defect this function exists to make unrepresentable.
      expect({
        region: plan.region,
        indicator: plan.indicator,
        command: resolveSteps(plan.command),
      }).toEqual({
        region: expected.region,
        indicator: expected.indicator,
        command: resolveSteps(expected.command),
      });
      expect(plan.label.length).toBeGreaterThan(0);
    } else {
      if (plan.ok) throw new Error("unreachable: the assertion above already failed");
      expect(plan.refusal.reason).toBe(expected.reason);
      expect(plan.refusal.actionLabel).toBe(expected.actionLabel);
      expect(resolveOptionalSteps(plan.refusal.actionCommand)).toEqual(
        resolveOptionalSteps(expected.actionCommand),
      );
      expect(plan.refusal.region).toEqual(expected.region);
      expect(plan.refusal.message.length).toBeGreaterThan(0);
    }
  });
});

describe("planDrop refusal messages", () => {
  it("distinguishes a mixed selection from a hidden member", () => {
    const mixed = planFor(["B1", "T1"], "B2", "before");
    const hidden = planFor(["GHOST", "B1"], "B2", "before");
    if (mixed.ok || hidden.ok) throw new Error("both drops should be refused");

    // The two causes the current drag path admits it conflates. A mixed
    // selection names both lists it spans; a hidden member names the row and
    // what is hiding it.
    expect(mixed.refusal.message).toContain("the top level");
    expect(mixed.refusal.message).toContain("the children of E1");
    expect(hidden.refusal.message).toContain("GHOST");
    expect(hidden.refusal.message).toContain("filter");
    expect(hidden.refusal.message).not.toBe(mixed.refusal.message);
  });

  it("points a cross-axis drop at the write that would make it expressible", () => {
    const into = planFor(["B1"], "M1", "after");
    if (into.ok) throw new Error("entering a queue from outside should be refused");
    expect(into.refusal.actionLabel).toBe("Assign to M1");
    expect(into.refusal.message).toContain("the M1 queue");

    // The other direction needs a clear rather than an assignment, so it offers
    // no one-click action.
    const outOf = planFor(["E1"], "B1", "before");
    if (outOf.ok) throw new Error("leaving a queue should be refused");
    expect(outOf.refusal.actionLabel).toBeUndefined();
    expect(outOf.refusal.message).toContain("the M1 queue");

    // The two remedies are opposite, so they get separate reasons: a consumer
    // switching on `reason` must not have to infer which one it is from
    // whether `actionLabel` happens to be present.
    expect(into.refusal.reason).toBe("needs-assignment");
    expect(outOf.refusal.reason).toBe("needs-unassignment");
    // And the queue to join arrives as data, not only inside the prose.
    expect(into.refusal.region).toEqual(QUEUE_M1);

    // The pair a caller renders: a label with the write behind it, or nothing.
    expect(refusalAction(into.refusal)).toEqual({
      label: "Assign to M1",
      command: into.refusal.actionCommand,
    });
    expect(refusalAction(outOf.refusal)).toBeNull();
  });

  // A milestone accepts no children (`VALID_CHILD_TYPES.milestone` is `[]`), so
  // the remedy has to travel the SCHEDULING axis: `updateNib`'s `milestone`
  // field, never a parent link. Read off the command itself, since that is what
  // the dispatcher turns into variables.
  it("offers an assignment, never a parent write", () => {
    const plan = planFor(["B1", "B2"], "M1", "after");
    if (plan.ok) throw new Error("entering a queue from outside should be refused");
    const remedy = refusalAction(plan.refusal);
    if (remedy === null) throw new Error("the assignment refusal should offer a remedy");
    const command = resolveSteps(remedy.command);
    if (command.kind !== "sequence") throw new Error("the remedy should be a sequence");

    for (const step of command.steps) {
      if (typeof step === "function") throw new Error("resolveSteps left a closure behind");
      switch (step.kind) {
        case "update-nib":
          // The whole input, so a `parent` smuggled in beside the assignment
          // fails here rather than passing an "is there a milestone" check.
          expect(step.input).toEqual({ milestone: "M1" });
          break;
        case "reorder-nib":
          expect(step.scope).toBe("MILESTONE");
          // `reorderNib`'s own `parentId` is a container change, which is the
          // same write under another name — and the server refuses it together
          // with MILESTONE scope.
          expect(step).not.toHaveProperty("parentId");
          break;
        default:
          throw new Error(`the remedy should assign and position only, not ${step.kind}`);
      }
    }
    // Both halves ran for both rows, paired: each row's assignment lands before
    // its own positioning, and no row is assigned until the row before it is
    // where the drop asked for it.
    expect(command.steps.map((s) => (typeof s === "function" ? "fn" : s.kind))).toEqual([
      "update-nib",
      "reorder-nib",
      "update-nib",
      "reorder-nib",
    ]);
  });

  it("caps the list of groups a mixed selection spans", () => {
    // A selection is unbounded — select all, then drag — and this message is
    // one line, so it must not grow with the selection.
    const everyOrderableId = ROWS.filter((r) => r.region !== null).map((r) => r.nib.id);
    const plan = planFor(everyOrderableId, "B2", "before");
    if (plan.ok) throw new Error("a selection spanning every group should be refused");

    expect(plan.refusal.reason).toBe("mixed-source");
    expect(plan.refusal.message).toMatch(/ and \d+ more\)/);
    // A group the selection really spans, left out because the cap bit.
    expect(plan.refusal.message).not.toContain("the children of CY1");
  });
});

describe("planDrop labels", () => {
  it.each([
    { drag: ["B1"], target: "B2", zone: "before" as DropZone, label: "Reorder in the top level" },
    { drag: ["E1"], target: "E2", zone: "before" as DropZone, label: "Reorder in the M1 queue" },
    { drag: ["B1"], target: "B3", zone: "after" as DropZone, label: "Move under B3" },
    // A DECLARED entry region names a container that is not the row under the
    // cursor, so the friendly wording would name a row the command never
    // touches: here the write is `setParent(B1, null)`, not anything about RS.
    { drag: ["B1"], target: "RS", zone: "reparent" as DropZone, label: "Move into the top level" },
    { drag: ["B1"], target: "T1", zone: "before" as DropZone, label: "Move into the children of E1" },
    { drag: ["E1"], target: "M1", zone: "after" as DropZone, label: "Move to the front of the M1 queue" },
  ])("names the destination: $label", ({ drag, target, zone, label }) => {
    const plan = planFor(drag, target, zone);
    if (!plan.ok) throw new Error(plan.refusal.message);
    expect(plan.label).toBe(label);
  });
});

// The RFC's cursor badge wants "Reorder in the Q3 Launch queue", and a region
// carries only ids — so the titles have to arrive from the caller. `nameOf`
// resolves them against the fixture rows the way the drag path resolves them
// against the rendered ones.
describe("planDrop with a namer", () => {
  const nameOf = (id: string) => ROWS_BY_ID.get(id)?.nib.title;

  function namedPlan(draggedIds: string[], targetId: string, zone: DropZone): DropPlan {
    const target = ROWS_BY_ID.get(targetId);
    if (target === undefined) throw new Error(`no fixture row ${targetId}`);
    return planDrop({
      draggedIds,
      rowsById: ROWS_BY_ID,
      draggedRowsById: ROWS_BY_ID,
      target,
      zone,
      descendantIds: collectDescendantIds(draggedIds, ROWS),
      containment: CONTAINMENT,
      nameOf,
    });
  }

  it.each([
    // The queue phrase the RFC names, on the fixture's own milestone title.
    { drag: ["E1"], target: "E2", zone: "before" as DropZone, label: "Reorder in the v1.0 queue" },
    { drag: ["E1"], target: "M1", zone: "after" as DropZone, label: "Move to the front of the v1.0 queue" },
    // The parent axis takes titles too: an id names the container no better
    // there than it does in a queue.
    { drag: ["B1"], target: "T1", zone: "before" as DropZone, label: "Move into the children of Epic one" },
    { drag: ["B1"], target: "B3", zone: "after" as DropZone, label: "Move under Backlog epic" },
    // Nothing to spell: the root group IS `parentId: null`.
    { drag: ["B1"], target: "B2", zone: "before" as DropZone, label: "Reorder in the top level" },
  ])("spells the destination as a title: $label", ({ drag, target, zone, label }) => {
    const plan = namedPlan(drag, target, zone);
    if (!plan.ok) throw new Error(plan.refusal.message);
    expect(plan.label).toBe(label);
  });

  it("carries titles into the refusals and their remedies", () => {
    const into = namedPlan(["B1"], "M1", "after");
    if (into.ok) throw new Error("entering a queue from outside should be refused");
    expect(into.refusal.message).toContain("the v1.0 queue");
    expect(into.refusal.actionLabel).toBe("Assign to v1.0");
    // The region still travels as DATA, in ids: a caller acting on the remedy
    // needs the id, and the title is only how the sentence reads.
    expect(into.refusal.region).toEqual(QUEUE_M1);

    const mixed = namedPlan(["B1", "T1"], "B2", "before");
    if (mixed.ok) throw new Error("a mixed selection should be refused");
    expect(mixed.refusal.message).toContain("the children of Epic one");
  });

  it("keeps the id for a container the namer cannot place", () => {
    // HS declares the children of E9, which has no row here. The same absence
    // that stops the type check from being decidable stops the namer from
    // answering, so the phrase falls back to the id — which is the one spelling
    // that is always available.
    const plan = namedPlan(["B1"], "HS", "reparent");
    if (plan.ok) throw new Error("a destination this view does not carry should be refused");
    expect(plan.refusal.reason).toBe("unknown-destination");
    expect(plan.refusal.message).toContain("the children of E9");
  });

  // The WHOLE sentence for the four refusals no namer-covered assertion reached,
  // plus `needs-assignment`, whose subject used to be spelled as a raw id in the
  // same breath as a title ("B1 is not in the v1.0 queue"). `toContain` on one
  // phrase cannot see that: it passes on the half that was threaded.
  it.each([
    {
      reason: "invalid-parent-type",
      drag: ["B3"],
      target: "FT",
      zone: "reparent" as DropZone,
      message: "Cannot put epic in the children of Feature.",
    },
    {
      reason: "needs-assignment",
      drag: ["B1"],
      target: "M1",
      zone: "after" as DropZone,
      message: "Backlog one is not in the v1.0 queue, and joining one is an assignment rather than a move.",
    },
    {
      reason: "needs-unassignment",
      drag: ["E1"],
      target: "B1",
      zone: "before" as DropZone,
      message:
        "Epic one is ordered in the v1.0 queue, so clear the milestone assignment before ordering in the top level.",
    },
    {
      reason: "anchor-not-in-destination",
      drag: ["DR1"],
      target: "DR2",
      zone: "before" as DropZone,
      message: "Declared two is shown in the top level but is not a member of it, so nothing can be positioned against it.",
    },
    {
      reason: "destination-inside-target",
      drag: ["B1"],
      target: "CY1",
      zone: "before" as DropZone,
      message: "the children of Cycle two is drawn inside Cycle one, so the drop would land below the row it points at.",
    },
  ])("spells the whole $reason sentence", ({ reason, drag, target, zone, message }) => {
    const plan = namedPlan(drag, target, zone);
    if (plan.ok) throw new Error(`${reason} should be refused`);
    expect(plan.refusal.reason).toBe(reason);
    expect(plan.refusal.message).toBe(message);
  });

  // The threading itself, rather than one phrase at a time. A required `nameOf`
  // makes a DROPPED argument a compile error, but not a NEW phrase that
  // interpolates an id directly — which is how `subjectIs` shipped spelling half
  // its sentence in ids. This reads the finished sentence: an id this namer CAN
  // place must not survive in one.
  //
  // Runs over the whole refusal table, so a case added there is swept without
  // being enumerated here. Ids the namer cannot place (E9, F9, H9 — containers
  // with no row) are legitimately spelled as ids and are absent from the set by
  // construction, since the set IS the namer's domain.
  it("leaves no placeable id raw in any refusal message", () => {
    const placeable = [...ROWS_BY_ID.keys()].filter((id) => nameOf(id) !== undefined);
    const refusals = CASES.filter((c) => !c.expected.ok);
    // The sweep is only as good as its input: an empty or one-case filter would
    // pass while saying nothing.
    expect(refusals.length).toBeGreaterThan(8);

    for (const { name, drag, target, zone } of refusals) {
      const plan = namedPlan(drag, target, zone);
      if (plan.ok) throw new Error(`${name}: expected a refusal`);
      for (const id of placeable) {
        expect(plan.refusal.message, `${name} — raw id ${id}`).not.toMatch(
          new RegExp(`(^|[^\\w-])${id}([^\\w-]|$)`),
        );
      }
    }
  });
});

describe("entryRegionOf", () => {
  const row = (id: string) => {
    const found = ROWS_BY_ID.get(id);
    if (found === undefined) throw new Error(`no fixture row ${id}`);
    return found;
  };

  it("gives a container the group of its own children", () => {
    expect(entryRegionOf(row("B3"))).toEqual({ axis: "parent", parentId: "B3" });
    expect(entryRegionOf(row("FT"))).toEqual({ axis: "parent", parentId: "FT" });
  });

  it("gives a section header the group it declared", () => {
    // The declaration wins over the type answer, and it has to: this is the
    // predicate `canHaveChildren` cannot supply.
    expect(canHaveChildren("milestone")).toBe(false);
    expect(entryRegionOf(row("M1"))).toEqual(QUEUE_M1);
    expect(entryRegionOf(row("RS"))).toEqual(TOP_LEVEL);
  });

  it("gives a leaf and a fabricated container nothing to enter", () => {
    expect(entryRegionOf(row("B1"))).toBeNull();
    expect(entryRegionOf(row(BACKLOG_ID))).toBeNull();
  });
});

// planDrop's layering claim — that the decision is pure and reaches no
// transport — is a claim about its import list and nothing else. `web/` has no
// eslint, no dependency-cruiser and no import-boundary config, so the source is
// read here: the obvious way to toast a refusal or to fetch a nib's title is to
// import the dispatcher, and that would turn the doc into a false premise
// silently.
//
// THIS file's import list only. A bare specifier picked up later by `dropZone`,
// `region`, `typeHierarchy` or anything they value-import reaches the bundle
// without failing anything here, and so does a `require(...)` call.
describe("dropPlan.ts import isolation", () => {
  const importLines = dropPlanSource.split("\n").filter((line) => line.startsWith("import"));

  it("imports exactly the modules below urql and Svelte", () => {
    expect(importLines).toEqual([
      'import type { DropZone } from "../drag.svelte";',
      'import { isValidCrossParentDrop, isValidDropTarget } from "../dropZone";',
      'import { batch, reorderChain, reorderNib, reparentAndReorder, sequence, setParent, updateNib } from "../mutations/commands";',
      'import type { AnyCommand, CommandResult, LeafCommand, SequenceStep } from "../mutations/types";',
      'import type { ContainmentIndex } from "../containment";',
      'import { takesAssignmentAxes } from "../membership";',
      'import type { RowData } from "../tableData";',
      'import type { SectionKey } from "../tree";',
      'import { canHaveChildren } from "../typeHierarchy";',
      'import { BY_ID, commonRegion, describeRegion, sameRegion, scopeOf, spellId, type Region, type RegionNamer } from "./region";',
      'import type { AssignableField, SectionEntry } from "./sectionMeaning";',
    ]);
    // A multi-line import still begins a line with `import`, so the equality
    // above is what catches it — its first line alone would not match. These
    // two cover what the line filter misses: a statement not starting a line,
    // and a dynamic `import(...)`.
    expect(dropPlanSource.match(/\bfrom\s+"/g)).toHaveLength(importLines.length);
    expect(dropPlanSource).not.toMatch(/\bimport\s*\(/);
  });

  it("names no package, and takes nothing from a runes module at runtime", () => {
    // urql, svelte and svelte-sonner are all bare specifiers, so "every
    // specifier is relative" is the whole of the urql half of the claim.
    // Falls back to the whole line rather than asserting the match: a line the
    // pattern misses then fails the check below by name instead of throwing a
    // bare TypeError out of the map.
    const specifiers = importLines.map((line) => line.match(/from "(.*)";$/)?.[1] ?? line);
    expect(specifiers.every((s) => s.startsWith("."))).toBe(true);
    // The Svelte half: `drag.svelte.ts` is a runes module, and only its
    // `DropZone` union is wanted — `import type` erases, so the module graph
    // never reaches it.
    for (const line of importLines) {
      if (line.includes(".svelte")) expect(line.startsWith("import type ")).toBe(true);
    }
  });
});

describe("the dragged rows are read from the grab, not from the live list", () => {
  // `rowsById` is live so a row arriving mid-drag can be aimed at; the dragged
  // set is frozen because what is being dragged is settled when the gesture picks
  // it up. Resolving the dragged rows against the live list instead makes one
  // that scrolls out of view read as a selection the filter hides — and
  // `hidden-member` is decided before the drop-on-self check, so it would turn
  // the ordinary cancel gesture into an error toast.
  it("plans normally when a dragged row has left the live list", () => {
    const live = new Map(ROWS_BY_ID);
    live.delete("B1");

    const plan = planDrop({
      draggedIds: ["B1"],
      rowsById: live,
      draggedRowsById: ROWS_BY_ID,
      target: ROWS_BY_ID.get("B1")!,
      zone: "reparent",
      descendantIds: collectDescendantIds(["B1"], ROWS),
      containment: CONTAINMENT,
    });

    expect(plan.ok).toBe(false);
    if (plan.ok) return;
    expect(plan.refusal.reason).toBe("drop-on-self");
  });

  it("still refuses a selection member the view never had", () => {
    const plan = planDrop({
      draggedIds: ["B1", "GHOST"],
      rowsById: ROWS_BY_ID,
      draggedRowsById: ROWS_BY_ID,
      target: ROWS_BY_ID.get("B2")!,
      zone: "before",
      descendantIds: collectDescendantIds(["B1", "GHOST"], ROWS),
      containment: CONTAINMENT,
    });

    expect(plan.ok).toBe(false);
    if (plan.ok) return;
    expect(plan.refusal.reason).toBe("hidden-member");
  });
});

describe("the fixture's containment forest", () => {
  // Keeps `CONTAINS` total against `ROWS`: a row with no place in the forest is
  // absent from the index, and every question asked of it answers as if the view
  // never drew it — which would make a whole case pass vacuously.
  it("holds every fixture row exactly once", () => {
    for (const row of ROWS) {
      expect(CONTAINMENT.has(row.nib.id), `${row.nib.id} is in no section of the fixture forest`).toBe(true);
    }
    const placed = [...ROOT_IDS, ...Object.values(CONTAINS).flat()];
    expect(new Set(placed).size).toBe(placed.length);
    expect(new Set(placed)).toEqual(new Set(ROWS.map((r) => r.nib.id)));
  });
});

/**
 * The destination check, on the one shape where reading CONTAINMENT and reading
 * the display-parent chain give different answers — driven end to end through
 * the real Milestones lens, because what has to be shown is a drop landing
 * somewhere the gesture did not point.
 *
 * `m1` is a milestone whose own parent `P` is one of its members. Nothing
 * creates that through the API — `VALID_CHILD_TYPES` lists `milestone` as
 * nobody's legal child — but `parentId` is reported from the file as it is
 * stored, and the client renders the shape rather than dropping it, the same way
 * it renders the severed parent cycle above.
 */
describe("a destination drawn inside a section the target heads", () => {
  const MILESTONE_UNDER_ITS_MEMBER: TreeTableNib[] = [
    makeNib({ id: "m1", title: "Vee one", type: "milestone", parentId: "P" }),
    makeNib({ id: "P", title: "Pea", type: "epic", milestone: "m1" }),
    makeNib({ id: "Y", title: "Why", type: "task" }),
  ];

  function milestoneView(nibs: TreeTableNib[], collapsed: ReadonlySet<string> = new Set()) {
    return buildShapedTableData(nibs, {}, viewShapeFor("milestones"), collapsed, null);
  }

  it("refuses the drop, and the row it would have drawn is inside the target", () => {
    const data = milestoneView(MILESTONE_UNDER_ITS_MEMBER);
    const rowsById = new Map(data.rows.map((r) => [r.nib.id, r]));

    // Why the display-parent chain cannot answer here: P is drawn inside m1's
    // section, and a row inside a section reports no display parent at all, so a
    // walk up from P stops before it reaches anything.
    expect(rowsById.get("P")!.displayParentId).toBeNull();
    expect(data.containment.containerOf("P")).toBe("m1");

    // The gesture: drag Y and point the indicator ABOVE the m1 header row. Its
    // destination is m1's own parent group, whose container is P.
    const plan = planDrop({
      draggedIds: ["Y"],
      rowsById,
      draggedRowsById: rowsById,
      target: rowsById.get("m1")!,
      zone: "before",
      descendantIds: collectDescendantIds(["Y"], data.rows),
      containment: data.containment,
    });

    expect(plan.ok).toBe(false);
    if (plan.ok) return;
    expect(plan.refusal.reason).toBe("destination-inside-target");
    expect(plan.refusal.region).toEqual({ axis: "parent", parentId: "P" });

    // And what accepting it would have meant, rendered: the write is a reparent
    // into P, and a row parented to P is drawn inside m1's section — two rows
    // BELOW the one the "before" indicator drew a line above.
    const landed = milestoneView(
      MILESTONE_UNDER_ITS_MEMBER.map((n) => (n.id === "Y" ? { ...n, parentId: "P" } : n)),
    );
    expect(landed.rows.map((r) => r.nib.id)).toEqual(["m1", "P", "Y"]);
    expect(landed.rows.find((r) => r.nib.id === "Y")!.section?.key).toBe("m1");
  });

  it("refuses it with m1's section COLLAPSED, where the destination has no row", () => {
    // The same gesture with the section shut. P keeps its node and loses its
    // row, so a guard reading the drawn rows would let this through — and the
    // write still lands Y inside the shut section, where the table draws nothing
    // at all.
    const data = milestoneView(MILESTONE_UNDER_ITS_MEMBER, new Set(["m1"]));
    const rowsById = new Map(data.rows.map((r) => [r.nib.id, r]));

    expect(rowsById.has("P")).toBe(false);
    expect(data.containment.has("P")).toBe(true);
    expect(data.containment.contains("m1", "P")).toBe(true);

    const plan = planDrop({
      draggedIds: ["Y"],
      rowsById,
      draggedRowsById: rowsById,
      target: rowsById.get("m1")!,
      zone: "before",
      descendantIds: collectDescendantIds(["Y"], data.rows),
      containment: data.containment,
    });

    expect(plan.ok).toBe(false);
    if (plan.ok) return;
    expect(plan.refusal.reason).toBe("destination-inside-target");
    expect(plan.refusal.region).toEqual({ axis: "parent", parentId: "P" });

    // And what accepting it would have meant, rendered: Y is written into P,
    // which is inside the still-shut section, so the dragged row vanishes.
    const landed = milestoneView(
      MILESTONE_UNDER_ITS_MEMBER.map((n) => (n.id === "Y" ? { ...n, parentId: "P" } : n)),
      new Set(["m1"]),
    );
    expect(landed.rows.map((r) => r.nib.id)).toEqual(["m1"]);
  });
});

/**
 * The one-member cycle, driven end to end through two real lenses. The hand-built
 * fixture above states the same refusal, but only a real build shows what
 * accepting it renders — and shows that `promotedCycleRoots` severs a
 * self-parented nib rather than dropping it, which is why the row is there to be
 * dropped on at all.
 *
 * Nothing creates this through the API (`internal/graph/resolver.go` refuses a
 * self-parent), but `parentId` is reported from the file as stored, so a
 * hand-authored one arrives on the wire verbatim.
 */
describe("a target parented to itself", () => {
  const SELF_PARENT: TreeTableNib[] = [
    makeNib({ id: "S", title: "Ess", type: "epic", parentId: "S" }),
    makeNib({ id: "L", title: "Ell", type: "task" }),
  ];

  it.each(["none", "milestones"] as const)("refuses the drop under the %s lens", (level) => {
    const data = buildShapedTableData(SELF_PARENT, {}, viewShapeFor(level), new Set(), null);
    const rowsById = new Map(data.rows.map((r) => [r.nib.id, r]));

    // Severed, so S is drawn as a root rather than under itself — and no
    // containment hop reaches it, which is why `contains` alone cannot answer.
    expect(rowsById.get("S")!.nib.parentId).toBe("S");
    expect(data.containment.contains("S", "S")).toBe(false);

    const plan = planDrop({
      draggedIds: ["L"],
      rowsById,
      draggedRowsById: rowsById,
      target: rowsById.get("S")!,
      zone: "before",
      descendantIds: collectDescendantIds(["L"], data.rows),
      containment: data.containment,
    });

    expect(plan.ok).toBe(false);
    if (plan.ok) return;
    expect(plan.refusal.reason).toBe("destination-inside-target");
    expect(plan.refusal.region).toEqual({ axis: "parent", parentId: "S" });

    // And what accepting it would have meant, rendered: L is drawn as S's child,
    // one row BELOW the line the "before" indicator drew above S.
    const landed = buildShapedTableData(
      SELF_PARENT.map((n) => (n.id === "L" ? { ...n, parentId: "S" } : n)),
      {},
      viewShapeFor(level),
      new Set(),
      null,
    );
    const landedL = landed.rows.find((r) => r.nib.id === "L")!;
    expect(landed.rows.map((r) => r.nib.id).indexOf("L")).toBeGreaterThan(landed.rows.map((r) => r.nib.id).indexOf("S"));
    expect(landed.containment.containerOf("L")).toBe("S");
    expect(landedL.depth).toBeGreaterThan(landed.rows.find((r) => r.nib.id === "S")!.depth);
  });
});
