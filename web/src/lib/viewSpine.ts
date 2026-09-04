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
 * Not a `ReadonlySet`: that type is erased, so `(spine.bucketIds as Set<string>)`
 * hands back the live set and `.add` on a module singleton would follow a vitest
 * worker into every other suite it serves. `Object.freeze` does not close that —
 * it leaves a Set's contents writable. A frozen object with only `has` has
 * nothing to call.
 */
export interface BucketIds {
  has(id: string): boolean;
}

/**
 * The view core, bound to one areas vocabulary.
 *
 * Membership is mechanical rather than a matter of taste: a function belongs
 * here iff `viewShapeFor` is on its call path. Everything else in tree.ts,
 * tableData.ts, dragBlock.ts, filter.ts and ordering/ stays a free export and is
 * reached directly.
 *
 * Every method is a one-line delegation that supplies a `ViewShape` — which is
 * also the god-object test: a method that had to reimplement logic instead of
 * passing a shape through would mean that logic was drawn on the wrong side.
 *
 * The methods are closures in an object literal and never read `this`, so a
 * caller may destructure them.
 */
export interface ViewSpine {
  /** The vocabulary this spine is bound to. */
  readonly areas: AreaVocabulary;
  viewShapeFor(level: ViewLevel): ViewShape;
  readonly bucketIds: BucketIds;
  /**
   * No production caller — `TreeTable`'s two subtree helpers were the last, and
   * they read `TableData.containment` instead. Kept because it is the only
   * spine member returning `TreeNode`, and `TreeNode.section` is the only place
   * `SectionMeta` is reachable at all: the table's rows carry a `RowSection`
   * (key, display, count, onEnter), which drops both `persistence` and the rest
   * of `SectionMeaning`.
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
// The shipped lenses, and the switch that hands one to a view level. Private to
// this module — see `viewShapeFor`.
// ---------------------------------------------------------------------------

/**
 * A lens grouping by nib TYPE: nibs of `grouping` head sections keeping their
 * whole subtree, containers ranked above that tier lose their row but are
 * descended into, and everything else at or below the tier falls into the
 * leftover section. `leftoverKey` must satisfy the rule on `isSyntheticRowId`.
 */
function typeLens(grouping: string[], leftoverKey: LeftoverKey, leftoverLabel: string): GroupingLens {
  const groupingTypes = new Set(grouping);
  // The container tier this lens groups by, derived from the single source of
  // truth (`typeRank`) rather than a hardcoded copy. All grouping types in a
  // lens share one rank (feature and bug are both rank 1), so the first
  // suffices.
  const tier = typeRank(grouping[0]);

  return {
    leftover: { key: leftoverKey, label: leftoverLabel },
    // A type lens's sections ARE nibs: each is minted by the nib that heads it,
    // and which nibs arrive is the response's decision. There is nothing to
    // state up front.
    declares: { kind: "none" },
    nestHeadersStructurally: true,
    // Headers keep their subtrees, so a type lens's only members are the loose
    // items in its leftover section — whose order is the walk's, or the active
    // column sort's. There is no third order to declare.
    orderWithinSection: () => null,
    // Grouping by type rearranges which rows are DRAWN together; it moves no row
    // into another ordering group, so every row keeps its own parent one — and a
    // drop into a section means what the row under the cursor means.
    meaning: () => GOVERNS_NOTHING,

    place(nib, byId) {
      // The section is decided by the OUTERMOST ancestor-or-self at or below the
      // tier: descent into a grouped view passes through above-tier containers
      // and nothing else, so the first such node on the root-to-nib path owns
      // everything under it. Climbing to find it reads the same rule backwards.
      const chain: TreeNib[] = [nib];
      const seen = new Set<string>([nib.id]);
      let current: TreeNib | undefined = nib.parentId !== null ? byId.get(nib.parentId) : undefined;
      while (current !== undefined && !seen.has(current.id)) {
        seen.add(current.id);
        chain.push(current);
        current = current.parentId !== null ? byId.get(current.parentId) : undefined;
      }

      // How far up the chain the RENDERED path reaches. Ordinarily the chain runs
      // out and its last entry is the root. A chain that closed on itself has no
      // root of its own, so the answer is `buildTree`'s: it promotes exactly one
      // member of the cycle — the lowest id, per `promotedCycleRoots` — to a root
      // and severs its parent edge, which leaves everything the climb walked PAST
      // that member off the rendered path entirely. Re-deriving that decision
      // here is what lets the lens agree with the forest it classifies, for a nib
      // merely LEADING INTO a cycle as much as for a member of one.
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
 * The Milestones view's leftover section.
 *
 * "Backlog" rather than "Unplanned" or "No milestone": this is the set the
 * server's `noMilestone: true` filter selects and `nibs list --backlog` prints,
 * and internal/membership says outright that "Backlog" is the name every
 * surface uses for it. That package says so because the set once carried four
 * names at once, which `TestRoadmapNamesTheBacklogTheSameWayEverywhere`
 * (cmd/roadmap_test.go) now guards against on the Go side.
 *
 * The key satisfies both halves of `isSyntheticRowId` — asserted over the
 * derived `bucketIds` in tree.test.ts, not left to this sentence.
 */
const BACKLOG_KEY: LeftoverKey = "/__backlog__";

/**
 * A milestone queue's order: the `milestoneOrder` key ascending, rows with no
 * key appended, title then id breaking a tie.
 *
 * The tail rule is not decoration. An assignee can legitimately carry no key —
 * `TreeNib.milestoneOrder` is empty for a nib never placed in a queue — and
 * plain key order would then float exactly those rows to the TOP of the queue,
 * where the server's own listing appends them. The shape mirrors
 * `nib.CompareByKey` (internal/nib/sort.go), which is what every server-side
 * queue listing sorts through.
 *
 * Compares with `<` rather than `localeCompare`, because these are fractional
 * ordering keys the server compares as bytes; a locale collation reorders
 * mixed-case keys against it. Titles keep `localeCompare`, matching the Go
 * tiebreak's case-insensitive comparison closely enough for a display order.
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
 * The lens the Milestones view renders: sections are MILESTONES, and what puts
 * a nib in one is its ASSIGNMENT, not its position in the parent tree. Every
 * milestone in the response heads a section of its own; everything else lands
 * in the section `milestoneOf` names, or in the Backlog when that is "".
 *
 * `milestoneOf` is CALLED, not restated. It is the mirror of Go's
 * `(*membership.View).MilestoneOf`, held to it by a generated parity contract
 * that pins the mirror and not its callers — so a second copy of the rule here
 * would drift with nothing to catch it.
 *
 * Two properties follow from keying on `milestoneOf`'s answer rather than on
 * the raw `milestone:` field, and both are what `meaning`'s invariant asks
 * for:
 *
 *   - A section key is always the id of a milestone-typed nib present in
 *     `byId`, since that is the only thing `milestoneOf` ever returns non-empty.
 *     That nib heads its own section here, so this lens mints no HEADLESS
 *     section — a dangling assignment or one naming a non-milestone resolves to
 *     "" and its nib walks on rather than minting a section labeled with the raw
 *     id.
 *   - The rows a milestone section declares its region over are its node's
 *     DIRECT children, and those are the DIRECTLY assigned rows: a derived
 *     member's parent lands in the same section (the walk that gave the child
 *     its answer runs through the parent), so `buildTree` nests it. Both halves
 *     are executable checks in tree.test.ts. The one shape that escapes them is
 *     a parent cycle wholly inside a section, where `buildTree` severs one
 *     member's edge and promotes it — the divergence `RowData.region` already
 *     names ("a cycle member `promotedCycleRoots` severed") — pinned there too.
 *
 * STATUS is not consulted. A closed milestone in the response heads a section
 * like any other, and its members stay in it. Dropping it instead would take
 * the "losing a milestone" path `milestoneOf` documents: the walk continues
 * past the emptied step rather than stopping, so its members land in the
 * Backlog or, on hand-authored data carrying a second assignment up the chain,
 * in a DIFFERENT milestone's section. `(*membership.View).Backlog` settles the
 * same question the same way on the Go side — "work under a status-hidden
 * milestone is scheduled work, not backlog" — so which milestones exist is the
 * response's decision, made by the filter, not this lens's.
 */
const MILESTONE_MEMBERSHIP_LENS: GroupingLens = {
  leftover: { key: BACKLOG_KEY, label: "Backlog" },
  // Which milestones exist is the response's decision, made by the filter (see
  // the STATUS paragraph above), so every section here is minted from the nibs
  // that arrived rather than stated up front.
  declares: { kind: "none" },
  // Membership does not run along parent links, so a section's nesting is
  // rebuilt from whichever nibs landed in it rather than inherited from a
  // header's subtree.
  nestHeadersStructurally: false,
  // The declaration a membership lens exists for: a milestone section's rows
  // are in that milestone's queue, so a drag inside one reorders on the
  // MILESTONE scope and a drop into the section joins that queue. The Backlog
  // means NOTHING — "" is memberless in that scope, and its rows are not all at
  // the display root either, so each falls back to its own resolved parent
  // group.
  //
  // One `Region` value serves both members, which is the milestone axis's own
  // shape rather than a coincidence worth generalizing: the queue a section's
  // rows are ordered in IS the queue a drop into it joins.
  meaning: (section) => {
    if (section === BACKLOG_KEY) return GOVERNS_NOTHING;
    const queue: Region = { axis: "milestone", milestoneId: section };
    return { memberRegion: queue, onEnter: { kind: "region", region: queue } };
  },
  // The Backlog has no queue, so it takes the walk's order (or the active
  // column sort's) rather than a key none of its rows share.
  orderWithinSection: (section) => (section === BACKLOG_KEY ? null : byMilestoneOrder),

  place(nib, byId) {
    if (nib.type === MILESTONE_TYPE) return { kind: "header", section: nib.id };
    // A closure, never `byId.get` itself: the method needs its receiver, and
    // the bare reference type-checks clean (see `MembershipLookup`).
    const section = milestoneOf(nib, (id) => byId.get(id));
    return { kind: "member", section: section === "" ? BACKLOG_KEY : section };
  },
};

/**
 * The Areas view's leftover section.
 *
 * "No area" rather than a borrowed name: unlike the milestone axis, whose
 * unassigned set is called the Backlog everywhere including on the server, the
 * area axis has no such set to name — `cmd/list.go` says outright that there is
 * no `--no-area` for `--area` to redirect an empty value to.
 *
 * The key satisfies both halves of `isSyntheticRowId` — asserted over the
 * derived `bucketIds` in tree.test.ts, not left to this sentence.
 */
const NO_AREA_KEY: LeftoverKey = "/__no_area__";

/**
 * The declared forest of an areas vocabulary, read off the DEPTH RUNS of the
 * flat list `sections()` answers: declaration order, a parent immediately before
 * its subtree, depth on every node. That is the same contract `subtreeOf`
 * reads, so the two derive the tree from one statement of it.
 *
 * Never re-split from `path`. The vocabulary already states the nesting, and a
 * second derivation here would be a second rule to hold against the server's —
 * which is the drift `AreaVocabulary` exists as a port of questions to avoid.
 *
 * A node whose depth names no open ancestor becomes a root rather than being
 * dropped, so a list this walk cannot nest still renders every area in it.
 */
function areaForest(nodes: readonly AreaNode[]): readonly DeclaredSection[] {
  interface Building extends SectionDisplay {
    key: SectionKey;
    children: Building[];
  }
  const roots: Building[] = [];
  // The open ancestor at each depth, truncated after every node so a run that
  // steps back out cannot attach a later node inside a closed subtree.
  const open: Building[] = [];
  for (const node of nodes) {
    const section: Building = {
      // The PATH is the key, because that is the value a nib's `area:` carries
      // and the value `onEnter` writes; the NAME is the label, because a nested
      // section is drawn inside the parent that supplies the rest of the path.
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
 * The lens the Areas view renders: sections are the DECLARED areas, and what
 * puts a nib in one is its `area:` assignment rather than its position in the
 * parent tree.
 *
 * RESOLVED, never trusted. A stored `area:` arrives verbatim and can name an
 * area the vocabulary no longer declares; a nib carrying one falls into the
 * leftover alongside a nib carrying none, so this lens mints no section outside
 * the declared forest. That is what makes `meaning`'s assignment safe to offer:
 * a section key is always a path the server would accept.
 *
 * The sections are DECLARED rather than discovered, which is the whole reason
 * this lens states a forest: an area with nothing in it is a fact about the
 * project, and `SECTION_RULES.declared.rendersWhenEmpty` is what keeps its row.
 */
function areaLens(areas: AreaVocabulary): GroupingLens {
  return {
    leftover: { key: NO_AREA_KEY, label: "No area" },
    declares: { kind: "forest", roots: areaForest(areas.sections()) },
    // Membership does not run along parent links, so a section's nesting is
    // rebuilt from whichever nibs landed in it rather than inherited from a
    // header's subtree. No nib heads an area section: an area is vocabulary,
    // not a nib.
    nestHeadersStructurally: false,
    // An area has no order of its own — there is no area ordering scope on the
    // server — so a section takes the walk's order, or the active column sort's.
    orderWithinSection: () => null,
    // `memberRegion: null` is REQUIRED, not incidental. `Region`'s arms are
    // exactly the ordering groups the server can resolve and there is no area
    // one, so naming a group here would claim membership of a group that does
    // not exist. Declaring none lets each row fall back to its own resolved
    // parent group, and `planDrop`'s `crosses-section` band is what then keeps a
    // line drawn between two sections from writing a reorder in the group they
    // happen to share.
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
 * EXHAUSTIVE switch, no default arm, declared return type — deliberately, since
 * this is the ONLY thing forcing every view level to say how it groups. A new
 * member of ViewLevel fails to compile here until it declares one; a `default`
 * arm here would let it default into some shape instead, which is the whole
 * hole `ViewShape` exists to close.
 *
 * MODULE-PRIVATE, and that is the point: the Areas arm's sections come from a
 * vocabulary, so a free export taking one would let any caller build a shape
 * against a vocabulary the app is not bound to — declaring zero sections, every
 * nib in the leftover — and compute table data, a drag block or an adjacency
 * check that disagrees with what is rendered, with no type error to say so. A
 * shape is reachable only through a spine, which is bound to one vocabulary.
 *
 * `areaSections` is that lens, built once by `makeViewSpine`: its forest is
 * derived from the vocabulary, which a spine holds for its lifetime, so
 * rebuilding it here would rebuild it on every table build.
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
 * The leftover-section keys, derived by asking every view level what it renders
 * as.
 *
 * DERIVED, not listed. The property guard on these keys is only worth anything
 * if every shipped lens is enrolled in it, and a hand-kept list enrolls a new
 * lens only if someone remembers to — while `viewShapeFor`'s exhaustive switch
 * enrolls it or fails to compile. An unenrolled leftover key that misses the
 * `isSyntheticRowId` property makes its own section row classify as a REAL nib
 * on every render: selectable, a legal Delete/batch target, a drop target, and a
 * member of the root ordering group (`makeSectionNode` gives every fabricated
 * container `parentId: null`, which is the fallback `rowRegion` then applies).
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

/** Bind the view core to a vocabulary. Two spines can coexist — a test's and the
 *  app's — which a module-level mutable vocabulary could not express. */
export function makeViewSpine(areas: AreaVocabulary): ViewSpine {
  // Built once per spine, because the forest it declares is derived from a
  // vocabulary the spine holds for its lifetime.
  const areaSections = areaLens(areas);
  // The binding itself. Every member below reaches a shape through this one
  // closure, which is what lets `ViewSpine.viewShapeFor` stay one-argument.
  const shapeOf = (level: ViewLevel): ViewShape => viewShapeFor(level, areaSections);

  // Frozen for the reason `createAreaVocabulary` is: `EMPTY_SPINE` and
  // `LOADING_SPINE` are module singletons every test file in a vitest worker
  // shares, so a reassigned method or vocabulary there would follow the worker
  // into unrelated suites.
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
 * The spine before the config query resolves.
 *
 * A stable singleton, so the `$derived`s reading it do not re-run while the app
 * waits — and distinct from `EMPTY_SPINE`, because during this window
 * `validity()` must answer "unknown" rather than "undeclared".
 */
export const LOADING_SPINE: ViewSpine = makeViewSpine(LOADING_AREAS);

/** The spine of a project that declares no areas — and the one tests destructure
 *  when the vocabulary is beside the point. */
export const EMPTY_SPINE: ViewSpine = makeViewSpine(EMPTY_AREAS);

/**
 * The spine when the config query failed.
 *
 * Distinct from `LOADING_SPINE` because that one promises an answer shortly, and
 * from `EMPTY_SPINE` because that one asserts the project declares no areas —
 * neither is true here. `validity()` answers "unknown" as it does while loading.
 */
export const UNAVAILABLE_SPINE: ViewSpine = makeViewSpine(UNAVAILABLE_AREAS);
