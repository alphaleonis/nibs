import { VIEW_LEVELS } from "./types";
import type { NibFilter, TableSort, TreeNib, TreeNode, TreeTableNib, ViewLevel } from "./types";
import type {
  DeclaredSection,
  GroupingLens,
  LeftoverKey,
  SectionDisplay,
  SectionKey,
  ViewShape,
} from "./tree";
import { buildShapedViewTree } from "./tree";
import { buildShapedTableData } from "./tableData";
import type { TableData } from "./tableData";
import { shapedAdjacencyReflectsOrdering, shapedDragBlockFor } from "./dragBlock";
import type { DragBlock } from "./dragBlock";
import { EMPTY_AREAS, LOADING_AREAS, UNAVAILABLE_AREAS } from "./areas";
import type { AreaNode, AreaVocabulary } from "./areas";
import type { Region } from "./ordering/region";
import { GOVERNS_NOTHING } from "./ordering/sectionMeaning";
import { MILESTONE_TYPE, milestoneOf } from "./membership";
import { typeRank } from "./typeHierarchy";

/**
 * Membership-only view of the leftover-section keys.
 *
 * Not a `ReadonlySet`: a cast recovers the live set, and `.add` on a module
 * singleton leaks into every suite a vitest worker serves. `Object.freeze`
 * leaves a Set's contents writable, so expose only `has`.
 */
export interface BucketIds {
  has(id: string): boolean;
}

/**
 * The view core, bound to one areas vocabulary.
 *
 * A function belongs here iff `viewShapeFor` is on its call path; everything
 * else stays a free export. Each method is a one-line delegation that supplies a
 * `ViewShape` — logic a method would have to add belongs outside the spine.
 *
 * The methods never read `this`, so callers may destructure them.
 */
export interface ViewSpine {
  readonly areas: AreaVocabulary;
  viewShapeFor(level: ViewLevel): ViewShape;
  readonly bucketIds: BucketIds;
  /**
   * No production caller. Kept because `TreeNode.section` is the only place
   * `SectionMeta` is reachable: a row's `RowSection` drops `persistence` and
   * `memberRegion`.
   */
  buildViewTree<T extends TreeNib>(
    nibs: T[],
    level: ViewLevel,
    sortComparator?: (a: T, b: T) => number,
  ): TreeNode<T>[];
  buildTableData(
    nibs: TreeTableNib[],
    filter: NibFilter,
    level: ViewLevel,
    collapsed: ReadonlySet<string>,
    sort?: TableSort | null,
  ): TableData;
  dragBlockFor(filter: NibFilter, level: ViewLevel, sort: TableSort | null): DragBlock | null;
  adjacencyReflectsOrdering(filter: NibFilter, level: ViewLevel, sort: TableSort | null): boolean;
}

// ---------------------------------------------------------------------------
// The shipped lenses, and the switch that hands one to a view level.
// ---------------------------------------------------------------------------

/**
 * A lens grouping by nib TYPE: nibs of `grouping` head sections keeping their
 * whole subtree, containers ranked above that tier lose their row but are
 * descended into, and everything else at or below the tier falls into the
 * leftover section. `leftoverKey` must satisfy the rule on `isSyntheticRowId`.
 */
function typeLens(grouping: string[], leftoverKey: LeftoverKey, leftoverLabel: string): GroupingLens {
  const groupingTypes = new Set(grouping);
  // All grouping types in a lens share one rank (feature and bug are both 1).
  const tier = typeRank(grouping[0]);

  return {
    leftover: { key: leftoverKey, label: leftoverLabel },
    // Each section is minted by the nib that heads it.
    declares: { kind: "none" },
    nestHeadersStructurally: true,
    // Headers keep their subtrees, so the only members are the leftover's loose
    // items, in the walk's or the column sort's order.
    orderWithinSection: () => null,
    // Grouping by type moves no row into another ordering group, so a drop into
    // a section means what the row under the cursor means.
    meaning: () => GOVERNS_NOTHING,

    place(nib, byId) {
      // The section is decided by the OUTERMOST ancestor-or-self at or below the
      // tier: grouped descent passes through above-tier containers only.
      const chain: TreeNib[] = [nib];
      const seen = new Set<string>([nib.id]);
      let current: TreeNib | undefined = nib.parentId !== null ? byId.get(nib.parentId) : undefined;
      while (current !== undefined && !seen.has(current.id)) {
        seen.add(current.id);
        chain.push(current);
        current = current.parentId !== null ? byId.get(current.parentId) : undefined;
      }

      // How far up the chain the RENDERED path reaches. For a chain that closed
      // on itself, match `buildTree`: the cycle member with the lowest id (see
      // `promotedCycleRoots`) becomes a root, so nodes climbed past it are off the
      // rendered path — for a nib leading into a cycle as well as a member.
      let rootIndex = chain.length - 1;
      const closedOn = current;
      if (closedOn !== undefined) {
        let promoted = chain.findIndex((node) => node.id === closedOn.id);
        for (let i = promoted + 1; i < chain.length; i++) {
          if (chain[i].id < chain[promoted].id) promoted = i;
        }
        rootIndex = promoted;
      }

      // Outermost = nearest the root, so scan down from where the path starts.
      let outermost: TreeNib | null = null;
      for (let i = rootIndex; i >= 0; i--) {
        if (typeRank(chain[i].type) <= tier) {
          outermost = chain[i];
          break;
        }
      }

      if (outermost === null) return { kind: "hidden" };
      if (!groupingTypes.has(outermost.type)) return { kind: "member", section: leftoverKey };
      return outermost.id === nib.id
        ? { kind: "header", section: nib.id }
        : { kind: "member", section: outermost.id };
    },
  };
}

const EPIC_TYPE_LENS = typeLens(["epic"], "/__no_epic__", "No epic");
const FEATURE_TYPE_LENS = typeLens(["feature", "bug"], "/__no_feature_or_bug__", "No feature or bug");

/**
 * The Milestones view's leftover section: the set the server's `noMilestone`
 * filter selects and `nibs list --backlog` prints, so it carries that flag's
 * name. Must satisfy `isSyntheticRowId`.
 */
const BACKLOG_KEY: LeftoverKey = "/__backlog__";

/**
 * A milestone queue's order, matching `nib.CompareByKey` (internal/nib/sort.go):
 * keyed rows by `milestoneOrder`, unkeyed rows after them, then title, then id.
 *
 * Keys compare with `<` because the server compares them as bytes; a locale
 * collation would reorder mixed-case keys. Titles use `localeCompare`, close
 * enough to Go's case-insensitive tiebreak for display.
 */
function byMilestoneOrder(a: TreeNib, b: TreeNib): number {
  const aKeyed = a.milestoneOrder !== "";
  const bKeyed = b.milestoneOrder !== "";
  if (aKeyed && bKeyed && a.milestoneOrder !== b.milestoneOrder) {
    return a.milestoneOrder < b.milestoneOrder ? -1 : 1;
  }
  if (aKeyed !== bKeyed) return aKeyed ? -1 : 1;
  const byTitle = a.title.localeCompare(b.title);
  return byTitle !== 0 ? byTitle : a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
}

/**
 * The Milestones view's lens: every milestone in the response heads a section,
 * and each other nib lands in the section `milestoneOf` names, or the Backlog
 * when that is "".
 *
 * Call `milestoneOf`; do not restate it. The generated parity contract pins that
 * mirror of Go's `(*membership.View).MilestoneOf`, not its callers.
 *
 * Keying on `milestoneOf` rather than the raw `milestone:` field gives `meaning`
 * two properties, both checked in tree.test.ts:
 *   - A section key is always the id of a milestone in `byId`, which heads its
 *     own section; a dangling or non-milestone assignment resolves to "" and
 *     mints no headless section.
 *   - A milestone section's direct children are its directly assigned rows: a
 *     derived member's parent lands in the same section, so `buildTree` nests
 *     it. A parent cycle inside a section is the exception (see `RowData.region`).
 *
 * Status is not consulted: a closed milestone still heads its section. Dropping
 * it would continue `milestoneOf`'s walk past it, moving its members to the
 * Backlog or another milestone. Go's `(*membership.View).Backlog` likewise
 * counts work under a milestone of any status as scheduled.
 */
const MILESTONE_MEMBERSHIP_LENS: GroupingLens = {
  leftover: { key: BACKLOG_KEY, label: "Backlog" },
  // Sections are minted from the nibs that arrived; the filter decides which
  // milestones exist.
  declares: { kind: "none" },
  // Membership does not follow parent links, so each section's nesting is
  // rebuilt from the nibs that landed in it.
  nestHeadersStructurally: false,
  // A milestone section's rows are in its queue: a drag inside reorders on the
  // MILESTONE scope, and a drop into it joins the queue. The Backlog governs
  // nothing, so its rows fall back to their own parent group.
  meaning: (section) => {
    if (section === BACKLOG_KEY) return GOVERNS_NOTHING;
    const queue: Region = { axis: "milestone", milestoneId: section };
    return { memberRegion: queue, onEnter: { kind: "region", region: queue } };
  },
  // The Backlog has no queue, so it takes the walk's or the column sort's order.
  orderWithinSection: (section) => (section === BACKLOG_KEY ? null : byMilestoneOrder),

  place(nib, byId) {
    if (nib.type === MILESTONE_TYPE) return { kind: "header", section: nib.id };
    // A closure, not bare `byId.get`, which loses its receiver yet type-checks
    // (see `MembershipLookup`).
    const section = milestoneOf(nib, (id) => byId.get(id));
    return { kind: "member", section: section === "" ? BACKLOG_KEY : section };
  },
};

/**
 * The Areas view's leftover section. Must satisfy `isSyntheticRowId`.
 */
const NO_AREA_KEY: LeftoverKey = "/__no_area__";

/**
 * The declared forest of an areas vocabulary, read off the depth runs of
 * `sections()` — the same ordering contract `subtreeOf` reads. Do not re-split
 * `path`.
 *
 * A node whose depth names no open ancestor becomes a root rather than being
 * dropped.
 */
function areaForest(nodes: readonly AreaNode[]): readonly DeclaredSection[] {
  interface Building extends SectionDisplay {
    key: SectionKey;
    children: Building[];
  }
  const roots: Building[] = [];
  // The open ancestor at each depth, truncated after every node so a later node
  // cannot attach inside a closed subtree.
  const open: Building[] = [];
  for (const node of nodes) {
    const section: Building = {
      // Keyed by PATH, the value `area:` carries and `onEnter` writes; labeled by
      // NAME, since a nested section is drawn inside its parent.
      key: node.path,
      label: node.name,
      description: node.description,
      color: node.color,
      children: [],
    };
    const parent = node.depth > 0 ? open[node.depth - 1] : undefined;
    if (parent === undefined) roots.push(section);
    else parent.children.push(section);
    open.length = node.depth;
    open.push(section);
  }
  return roots;
}

/**
 * The Areas view's lens: sections are the DECLARED areas, and a nib's resolved
 * `area:` places it.
 *
 * A stored `area:` the vocabulary does not declare lands in the leftover, so
 * every section key is a path the server accepts — which keeps `meaning`'s
 * assignment valid. Declared sections render even when empty (`SECTION_RULES`).
 */
function areaLens(areas: AreaVocabulary): GroupingLens {
  return {
    leftover: { key: NO_AREA_KEY, label: "No area" },
    declares: { kind: "forest", roots: areaForest(areas.sections()) },
    // Membership does not follow parent links, so each section's nesting is
    // rebuilt from the nibs that landed in it. No nib heads an area section.
    nestHeadersStructurally: false,
    // The server has no area ordering scope.
    orderWithinSection: () => null,
    // `memberRegion` must stay null: `Region` has no area axis. Each row falls
    // back to its own parent group, and `planDrop`'s `crosses-section` refusal
    // keeps a drop between two sections from reordering the group they share.
    meaning: (section) =>
      section === NO_AREA_KEY
        ? GOVERNS_NOTHING
        : {
            memberRegion: null,
            onEnter: { kind: "assign", field: "area", value: section, noun: "area" },
          },

    place(nib) {
      const declared = areas.resolve(nib.area);
      return { kind: "member", section: declared === null ? NO_AREA_KEY : declared.path };
    },
  };
}

/**
 * The shape each view level renders in.
 *
 * Exhaustive switch with no default arm: a new `ViewLevel` fails to compile
 * until it declares a shape.
 *
 * Keep it module-private. A caller passing some other vocabulary's lens would
 * compute table data, drag blocks and adjacency that disagree with what is
 * rendered, with no type error. `areaSections` is built once per spine.
 */
function viewShapeFor(viewLevel: ViewLevel, areaSections: GroupingLens): ViewShape {
  switch (viewLevel) {
    case "none":
      return { kind: "tree" };
    case "flat":
      return { kind: "flat" };
    case "milestones":
      return { kind: "grouped", lens: MILESTONE_MEMBERSHIP_LENS };
    case "epics":
      return { kind: "grouped", lens: EPIC_TYPE_LENS };
    case "features":
      return { kind: "grouped", lens: FEATURE_TYPE_LENS };
    case "areas":
      return { kind: "grouped", lens: areaSections };
  }
}

/**
 * The leftover-section keys, derived from every view level's shape so the
 * tests' `isSyntheticRowId` check covers each lens `viewShapeFor` ships.
 *
 * A leftover key failing that check makes its section row classify as a real
 * nib: selectable, a Delete/batch and drop target, and — with the `parentId:
 * null` of `makeSectionNode` — a member of the root ordering group.
 */
function bucketIdsFor(shapeOf: (level: ViewLevel) => ViewShape): BucketIds {
  const keys = new Set<string>(
    VIEW_LEVELS.flatMap((level) => {
      const shape = shapeOf(level);
      return shape.kind === "grouped" ? [shape.lens.leftover.key] : [];
    }),
  );
  return Object.freeze({ has: (id: string) => keys.has(id) });
}

/** Bind the view core to a vocabulary. Several spines may coexist, e.g. a test's
 *  and the app's. */
export function makeViewSpine(areas: AreaVocabulary): ViewSpine {
  const areaSections = areaLens(areas);
  const shapeOf = (level: ViewLevel): ViewShape => viewShapeFor(level, areaSections);

  // Frozen: the exported spines are module singletons shared by every suite in
  // a vitest worker.
  return Object.freeze({
    areas,
    viewShapeFor: shapeOf,
    bucketIds: bucketIdsFor(shapeOf),
    buildViewTree: (nibs, level, sortComparator) =>
      buildShapedViewTree(nibs, shapeOf(level), sortComparator),
    buildTableData: (nibs, filter, level, collapsed, sort = null) =>
      buildShapedTableData(nibs, filter, shapeOf(level), collapsed, sort),
    dragBlockFor: (filter, level, sort) => shapedDragBlockFor(filter, shapeOf(level), sort),
    adjacencyReflectsOrdering: (filter, level, sort) =>
      shapedAdjacencyReflectsOrdering(filter, shapeOf(level), sort),
  } satisfies ViewSpine);
}

/**
 * The spine before the config query resolves. A stable singleton, so `$derived`s
 * reading it do not re-run while waiting; `validity()` answers "unknown", not
 * "undeclared".
 */
export const LOADING_SPINE: ViewSpine = makeViewSpine(LOADING_AREAS);

/** The spine of a project that declares no areas — and the one tests destructure
 *  when the vocabulary is beside the point. */
export const EMPTY_SPINE: ViewSpine = makeViewSpine(EMPTY_AREAS);

/**
 * The spine when the config query failed: no answer is coming, and the project
 * is not known to declare no areas. `validity()` answers "unknown".
 */
export const UNAVAILABLE_SPINE: ViewSpine = makeViewSpine(UNAVAILABLE_AREAS);
