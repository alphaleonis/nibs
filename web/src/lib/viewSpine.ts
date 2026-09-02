import { VIEW_LEVELS } from "./types";
import type { NibFilter, TableSort, TreeNib, TreeNode, TreeTableNib, ViewLevel } from "./types";
import type { ViewShape } from "./tree";
import { buildShapedViewTree, shapedContainingSectionRowId, viewShapeFor } from "./tree";
import { buildShapedTableData } from "./tableData";
import type { TableData } from "./tableData";
import { shapedAdjacencyReflectsOrdering, shapedDragBlockFor } from "./dragBlock";
import type { DragBlock } from "./dragBlock";
import { EMPTY_AREAS, LOADING_AREAS } from "./areas";
import type { AreaVocabulary } from "./areas";

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
  buildViewTree<T extends TreeNib>(
    nibs: T[],
    level: ViewLevel,
    sortComparator?: (a: T, b: T) => number,
  ): TreeNode<T>[];
  containingSectionRowId<T extends TreeNib>(
    byId: ReadonlyMap<string, T>,
    nibId: string,
    level: ViewLevel,
  ): string | null;
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
  // Frozen for the reason `createAreaVocabulary` is: `EMPTY_SPINE` and
  // `LOADING_SPINE` are module singletons every test file in a vitest worker
  // shares, so a reassigned method or vocabulary there would follow the worker
  // into unrelated suites.
  return Object.freeze({
    areas,
    viewShapeFor,
    bucketIds: bucketIdsFor(viewShapeFor),
    buildViewTree: (nibs, level, sortComparator) =>
      buildShapedViewTree(nibs, viewShapeFor(level), sortComparator),
    containingSectionRowId: (byId, nibId, level) =>
      shapedContainingSectionRowId(byId, nibId, viewShapeFor(level)),
    buildTableData: (nibs, filter, level, collapsed, sort = null) =>
      buildShapedTableData(nibs, filter, viewShapeFor(level), collapsed, sort),
    dragBlockFor: (filter, level, sort) => shapedDragBlockFor(filter, viewShapeFor(level), sort),
    adjacencyReflectsOrdering: (filter, level, sort) =>
      shapedAdjacencyReflectsOrdering(filter, viewShapeFor(level), sort),
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
