import type { TreeTableNib, NibFilter, TreeNode, TableSort } from "./types";
import type { Region } from "./ordering/region";
import type { SectionEntry } from "./ordering/sectionMeaning";
import { buildShapedViewTree, holdsChildrenByDisplay, isSyntheticRowId, SECTION_RULES } from "./tree";
import type { SectionDisplay, SectionKey, ViewShape } from "./tree";
import { buildContainmentIndex } from "./containment";
import type { ContainmentIndex } from "./containment";
import { makeNibComparator } from "./tableSort";
import { hasClientFilters, matchesFilter } from "./filter";
import { MILESTONE_TYPE } from "./membership";

/**
 * One rendered table row.
 *
 * INVARIANT: a real nib's id appears at most once across `TableData.rows`. Rows
 * are keyed and addressed by id alone, so a repeat breaks:
 *   - TreeTable's delegated handlers, which recover the row by id with
 *     `rows.find` and always reach the first;
 *   - the keyed `{#each}`, where Svelte throws `each_key_duplicate`;
 *   - the `tr[data-nib-id]` lookups behind reveal-scroll, drag and keyboard nav;
 *   - `rangeSelect`'s `visibleIds.indexOf` anchor.
 *
 * `buildTree` gives each nib one parent slot, and `buildShapedViewTree` calls a
 * lens's `place` once per nib. Section-container ids cannot collide with nib ids
 * (see `isSyntheticRowId`).
 */
export interface RowData {
  nib: TreeTableNib;
  depth: number;
  hasChildren: boolean;
  dimmed: boolean;
  parentNib: TreeTableNib | null;
  /**
   * The milestone this row's `milestone` assignment names, or null when
   * unassigned, when that id is not in this table, or when it is not a milestone.
   */
  milestoneNib: TreeTableNib | null;
  /**
   * The id this row reorders against in the view tree, or null at the display
   * root. Unlike `parentNib` (the logical parent) and `TableData.containment`
   * (what draws the row), display containers are elided: rows under a synthetic
   * bucket or under a nib heading a section of non-children take that
   * container's own display parent.
   *
   * INVARIANT: null or a real nib id that could hold this row as a child, usable
   * directly as a backend `parentId`.
   *
   * No production reader.
   */
  displayParentId: string | null;
  /**
   * The ordering group governing this row's display position — the list
   * `reorderNib` moves it within — or null when no reorder can address it. See
   * `rowRegion`.
   *
   * A nib may be in both a parent group and a milestone queue; this is the one
   * the view put it in, so rows in different regions may still share a group.
   * Without a container declaration it follows the server-resolved
   * `nib.parentId`, which diverges from `displayParentId` wherever the view tree
   * does: a lens hiding a container, a parent the filter left out, or a cycle
   * member `promotedCycleRoots` severed.
   */
  region: Region | null;
  /**
   * The section whose own row this is, or null for every other row. This decides
   * what aiming AT the row means; a member row carries only `section`, so a drop
   * onto it is decided by the row.
   */
  drawsSection: RowSection | null;
  /**
   * The section this row is a member of, transitively; null in an ungrouped view
   * and for outermost section rows. Omits `memberRegion`, which `rowRegion` has
   * already folded into `region`.
   */
  section: RowSection | null;
}

/** Which section, what it shows, and what entering it does. */
export interface RowSection {
  readonly key: SectionKey;
  readonly display: SectionDisplay;
  /**
   * How many nib rows the section draws with every container expanded: members,
   * their descendants and declared sub-sections' rows, not fabricated rows.
   * Follows the client filter but not collapse, so a collapsed heading keeps its
   * count; a declared section a filter empties reads 0.
   */
  readonly count: number;
  readonly onEnter: SectionEntry;
}

export interface TableData {
  rows: RowData[];
  allTags: string[];
  parentIds: Set<string>;
  /**
   * Every id the view tree has a row for, fabricated containers included. A
   * grouping lens can lack one: the Epics lens has no row for a milestone.
   *
   * Independent of collapse, so a collapsed parent never looks departed, and of
   * the client filter, whose pruning `retainOnly` handles.
   */
  viewMemberIds: Set<string>;
  /**
   * What contains what in this view, for reveal, ArrowLeft, subtree
   * expand/collapse and the drop plan. Built from the tree, so it covers nibs
   * inside collapsed sections.
   */
  containment: ContainmentIndex;
}

/**
 * The `RowData.region` rule, shared by `flatten` and test fixtures.
 *
 * A synthetic row names no nib and has no region. Any other row takes its
 * container's declared region, else its resolved parent's group.
 */
export function rowRegion(
  id: string,
  parentId: string | null,
  declaredByContainer: Region | null = null,
): Region | null {
  if (isSyntheticRowId(id)) return null;
  return declaredByContainer ?? { axis: "parent", parentId };
}

/**
 * Whether a shape shows a row's ancestors, so a client filter keeps non-matching
 * ancestors for context. Exhaustive switch: a new shape fails to compile here.
 */
function showsAncestorContext(shape: ViewShape): boolean {
  switch (shape.kind) {
    case "flat":
      return false;
    case "tree":
    case "grouped":
      return true;
  }
}

export function buildShapedTableData(
  allNibs: TreeTableNib[],
  filter: NibFilter,
  shape: ViewShape,
  collapsedIds: ReadonlySet<string>,
  sort: TableSort | null = null,
): TableData {
  // Stage 1: Build nibMap for O(1) parent lookups
  const nibMap = new Map<string, TreeTableNib>();
  for (const nib of allNibs) {
    nibMap.set(nib.id, nib);
  }

  // Stage 2: Collect allTags (sorted, deduplicated)
  const tagSet = new Set<string>();
  for (const nib of allNibs) {
    for (const tag of nib.tags) {
      tagSet.add(tag);
    }
  }
  const allTags = [...tagSet].sort();

  // Stage 3: Compute parentIds (which nibs have children). parentId is already
  // server-resolved; nibMap.has covers a parent the filter left out of the
  // response.
  const parentIds = new Set<string>();
  for (const nib of allNibs) {
    if (nib.parentId && nibMap.has(nib.parentId)) {
      parentIds.add(nib.parentId);
    }
  }

  // Stage 4: If advanced filters active, compute visibility
  let matchingIds: Set<string> | null = null;
  let visibleIds: Set<string> | null = null;

  if (hasClientFilters(filter)) {
    matchingIds = new Set<string>();
    for (const nib of allNibs) {
      if (matchesFilter(nib, filter)) {
        matchingIds.add(nib.id);
      }
    }

    // Keep ancestors of matches for nesting context — except in flat view, where
    // an ancestor would render as a stray unindented row.
    const ancestorIds = new Set<string>();
    if (showsAncestorContext(shape)) {
      for (const id of matchingIds) {
        const visited = new Set<string>();
        let current = nibMap.get(id);
        while (current?.parentId && !visited.has(current.parentId)) {
          visited.add(current.parentId);
          ancestorIds.add(current.parentId);
          current = nibMap.get(current.parentId);
        }
      }
    }

    visibleIds = new Set<string>([...matchingIds, ...ancestorIds]);
  }

  // Stage 5: Build view tree. A column sort orders the grouped lenses' headers
  // and leftover items by the sort field, not by their hidden ancestors.
  const nodeComparator = sort ? makeNibComparator(sort, nibMap) : undefined;
  const tree = buildShapedViewTree<TreeTableNib>(allNibs, shape, nodeComparator);

  // Stage 5a: one walk over the emitted tree. A node holding rows by ARRANGEMENT
  // (a synthetic bucket, or a nib heading a section of non-children) is missing
  // from `parentIds` and `visibleIds`, which follow real parent links. Fold it
  // in: collapsible when it has rows, visible when a descendant is — otherwise
  // flatten() skips it and every matching row under it.
  //
  // The same walk collects `viewMemberIds` (every node) and `sectionCounts`,
  // which depend on the `visibleIds` settled here.
  const viewMemberIds = new Set<string>();
  const sectionCounts = new Map<string, number>();
  (function foldDisplayContainers(nodes: TreeNode<TreeTableNib>[]): { anyVisible: boolean; drawnNibs: number } {
    let anyVisible = false;
    let drawnNibs = 0;
    for (const node of nodes) {
      viewMemberIds.add(node.nib.id);
      const below = foldDisplayContainers(node.children);
      const childVisible = below.anyVisible;
      // False for a childless node, so this also answers "has rows under it".
      const byDisplay = holdsChildrenByDisplay(node);
      if (byDisplay) {
        parentIds.add(node.nib.id);
      }
      // A declared section's row survives a client filter that empties it; a
      // discovered one prunes. Only a fabricated row persists this way — a real
      // nib heading a declared section still hides when filtered out.
      const persists =
        node.section !== undefined &&
        isSyntheticRowId(node.nib.id) &&
        SECTION_RULES[node.section.persistence].rendersWhenEmpty;
      const selfVisible =
        persists || (visibleIds ? visibleIds.has(node.nib.id) : true) || childVisible;
      if (visibleIds && (byDisplay || persists) && selfVisible) {
        visibleIds.add(node.nib.id);
      }
      // `flatten`'s visibility skip, read after the add above. Collapse is left
      // out: a collapsed node keeps its row and its subtree still counts.
      const drawn = visibleIds === null || visibleIds.has(node.nib.id);
      if (node.section !== undefined) sectionCounts.set(node.nib.id, below.drawnNibs);
      // Synthetic rows do not count; an undrawn node takes its subtree with it.
      if (drawn) drawnNibs += below.drawnNibs + (isSyntheticRowId(node.nib.id) ? 0 : 1);
      anyVisible = anyVisible || selfVisible;
    }
    return { anyVisible, drawnNibs };
  })(tree);

  // Stage 6: Flatten tree with collapse gating, visibility filtering, dimming, parent resolution
  const rows: RowData[] = [];

  function flatten(
    nodes: TreeNode<TreeTableNib>[],
    displayParentId: string | null,
    enclosingMemberRegion: Region | null,
    enclosingSection: RowSection | null,
  ): void {
    for (const node of nodes) {
      if (visibleIds && !visibleIds.has(node.nib.id)) continue;

      // A synthetic row is never in matchingIds, so it never dims; a real nib
      // heading a section dims like any other row.
      const dimmed = matchingIds && !isSyntheticRowId(node.nib.id) ? !matchingIds.has(node.nib.id) : false;
      const visibleChildren = visibleIds
        ? node.children.filter(c => visibleIds.has(c.nib.id))
        : node.children;
      const parentNib = node.nib.parentId ? nibMap.get(node.nib.parentId) ?? null : null;
      // `milestone` is verbatim; a target missing from this response or not a
      // milestone resolves to null, as membership treats it as no assignment.
      const assignedNib = node.nib.milestone ? nibMap.get(node.nib.milestone) : undefined;
      const milestoneNib = assignedNib?.type === MILESTONE_TYPE ? assignedNib : null;

      const drawsSection: RowSection | null =
        node.section === undefined
          ? null
          : {
              key: node.section.key,
              display: node.section.display,
              count: sectionCounts.get(node.nib.id) ?? 0,
              onEnter: node.section.meaning.onEnter,
            };
      const memberRegion = node.section?.meaning.memberRegion ?? null;
      const region = rowRegion(node.nib.id, node.nib.parentId, enclosingMemberRegion);

      rows.push({
        nib: node.nib,
        depth: node.depth,
        hasChildren: visibleChildren.length > 0,
        dimmed,
        parentNib,
        milestoneNib,
        displayParentId,
        region,
        drawsSection,
        section: enclosingSection,
      });

      if (!collapsedIds.has(node.nib.id)) {
        // Rows held by ARRANGEMENT take this node's own display parent (see
        // `RowData.displayParentId`).
        //
        // Pass down this node's OWN declared region, not the inherited one: a
        // declaration covers a container's rows, not everything beneath them. A
        // subtask under a queued epic orders under the epic, even when a
        // hand-authored file assigns it a milestone too.
        //
        // The section identity travels all the way down: both assembly modes
        // put a member's descendants in the member's section.
        flatten(
          node.children,
          holdsChildrenByDisplay(node) ? displayParentId : node.nib.id,
          memberRegion,
          drawsSection ?? enclosingSection,
        );
      }
    }
  }

  flatten(tree, null, null, null);

  // From `tree`, not `rows`, so reveal can open the collapsed sections hiding a
  // nib.
  const containment = buildContainmentIndex(tree);

  return { rows, allTags, parentIds, viewMemberIds, containment };
}
