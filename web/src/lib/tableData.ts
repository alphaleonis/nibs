import type { TreeTableNib, NibFilter, ViewLevel, TreeNode, TableSort } from "./types";
import { buildViewTree, holdsChildrenByDisplay, isSyntheticRowId } from "./tree";
import { makeNibComparator } from "./tableSort";
import { hasClientFilters, matchesFilter } from "./filter";

/**
 * One rendered table row.
 *
 * INVARIANT: across `TableData.rows`, a real nib's id appears at most ONCE. The
 * rendered list is keyed and addressed by id alone, so a repeat is not a
 * cosmetic extra row; it silently mis-targets four consumers at once, none of
 * which can tell which occurrence was meant:
 *   - TreeTable's delegated handlers carry only the id (`getNibIdFromEvent`) and
 *     recover everything structural with `rows.find(r => r.nib.id === nibId)`,
 *     so a second row with that id is unreachable and every action aimed at it
 *     lands on the first.
 *   - the keyed `{#each rows as row (row.nib.id)}` in TreeTable requires unique
 *     keys; Svelte throws `each_key_duplicate` on a repeat, taking the table
 *     down rather than rendering it wrong.
 *   - the `tr[data-nib-id="..."]` selectors behind reveal-scroll, drag and
 *     keyboard navigation resolve to the first match in document order, so
 *     focus, scroll and drop targets can point at a row the user never touched.
 *   - `visibleIds.indexOf` in selection.svelte.ts anchors a range-select on the
 *     first occurrence, so shift-click can select a span that does not reach the
 *     row that was clicked.
 *
 * Two things uphold it upstream: `buildTree` gives every nib exactly one parent
 * slot, and `buildViewTree`'s `classify` visits each node exactly once (a
 * grouping header and a bucket item both stop the descent, so a container nested
 * under another of its own tier stays inside that header's subtree instead of
 * also being promoted). A membership model where one nib can belong to several
 * containers — assignment-based grouping, multi-parent links — has to reconcile
 * that to a single row here, or pick a row key that is not the nib id and fix
 * all four consumers above.
 *
 * The synthetic bucket ids `buildViewTree` mints are covered too, by a third
 * mechanism: they are minted from a fixed table rather than derived from the
 * input, so neither of the two above reaches them, and instead each one carries
 * a leading "/" that no filename-derived id can hold (see GROUPING_LENSES in
 * tree.ts). The two id spaces are therefore disjoint by construction — a nib
 * cannot be given a bucket's id however its file is named — rather than merely
 * unlikely to overlap.
 */
export interface RowData {
  nib: TreeTableNib;
  depth: number;
  hasChildren: boolean;
  dimmed: boolean;
  parentNib: TreeTableNib | null;
  /**
   * The id of the nib this row would REORDER AGAINST in the current view tree,
   * or null when it reorders at the display root. This is the structural
   * authority for drag reorder: under a grouping lens it differs from
   * `nib.parentId` (a promoted header's display parent is null though its real
   * parent is a hidden container). Distinct from `parentNib`, which stays the
   * real logical parent used by the "Parent" column.
   *
   * INVARIANT: always a real nib id that could hold this row as a child, or
   * `null` — so consumers can use it directly as a backend `parentId` /
   * type-lookup key with no guard of their own. That excludes both kinds of
   * display container: a synthetic bucket, whose id names no nib, and a real nib
   * heading a section of rows that are not its children. Rows under either
   * inherit that container's OWN display parent (`null` for today's single
   * top-level bucket) rather than naming it. If display containers ever nest,
   * the recursion in `flatten` must pass the container's own resolved display
   * parent rather than the value threaded down.
   */
  displayParentId: string | null;
}

export interface TableData {
  rows: RowData[];
  allTags: string[];
  parentIds: Set<string>;
}

export function buildTableData(
  allNibs: TreeTableNib[],
  filter: NibFilter,
  viewLevel: ViewLevel,
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

  // Stage 3: Compute parentIds (which nibs have children)
  //
  // parentId is the server's RESOLVED parent — null when the stored link names
  // no nib — so it already answers "does this nib have a parent" the way every
  // other server surface does. The nibMap.has guard stays as defense: this map
  // holds the nibs in the CURRENT response, which a filter can narrow to a set
  // that excludes a real parent, so a present-and-resolvable id can still be
  // absent here.
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

    // Walk ancestor chains for visibility so a matching descendant keeps its
    // nesting context. Flat view has no nesting — every nib is an ungrouped
    // depth-0 root — so a non-matching ancestor there would render as a stray,
    // unindented dimmed row with no visual link to its match. Skip the walk in
    // flat: a non-match is simply excluded like any other.
    const ancestorIds = new Set<string>();
    if (viewLevel !== "flat") {
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

  // Stage 5: Build view tree. When a column sort is active, hand the tree
  // builder a node comparator (built from the already-computed nibMap) so the
  // epics/features lenses order their promoted headers + bucket items globally
  // by the sort field instead of by their hidden higher-tier ancestor.
  const nodeComparator = sort ? makeNibComparator(sort, nibMap) : undefined;
  const tree = buildViewTree<TreeTableNib>(allNibs, viewLevel, nodeComparator);

  // Stage 5a: `parentIds` and `visibleIds` are both derived from real `parentId`
  // links, so a node that holds its rows by ARRANGEMENT — a synthetic "No X"
  // bucket, or a real nib heading a section of members that are not its children
  // — is absent from both however visible its section is. Fold those in from the
  // emitted tree so consumers treat them like the containers they are:
  //   - collapse (parentIds): a display container with rows under it is
  //     collapsible, or it renders a section with no way to close it.
  //   - filter visibility (visibleIds): a display container whose subtree holds a
  //     visible descendant must itself be visible, or flatten() would skip it AND
  //     everything under it — silently dropping filter-matching rows (the lens is
  //     lossless, so a client filter must not hide them).
  (function foldDisplayContainers(nodes: TreeNode<TreeTableNib>[]): boolean {
    let anyVisible = false;
    for (const node of nodes) {
      const childVisible = foldDisplayContainers(node.children);
      // `some` over the children, so this is false for a childless node and the
      // "has rows under it" half of the collapse question comes for free.
      const byDisplay = holdsChildrenByDisplay(node);
      if (byDisplay) {
        parentIds.add(node.nib.id);
      }
      const selfVisible = (visibleIds ? visibleIds.has(node.nib.id) : true) || childVisible;
      if (visibleIds && byDisplay && selfVisible) {
        visibleIds.add(node.nib.id);
      }
      anyVisible = anyVisible || selfVisible;
    }
    return anyVisible;
  })(tree);

  // Stage 6: Flatten tree with collapse gating, visibility filtering, dimming, parent resolution
  const rows: RowData[] = [];

  function flatten(nodes: TreeNode<TreeTableNib>[], displayParentId: string | null): void {
    for (const node of nodes) {
      // If we have visibility filtering, skip non-visible nodes
      if (visibleIds && !visibleIds.has(node.nib.id)) continue;

      // An identity question, not an arrangement one: a synthetic bucket row is
      // never in matchingIds because it is no nib, so a client filter must not
      // dim it. A real nib heading a section IS in the filter's domain and dims
      // like any other row when it does not match.
      const dimmed = matchingIds && !isSyntheticRowId(node.nib.id) ? !matchingIds.has(node.nib.id) : false;
      const visibleChildren = visibleIds
        ? node.children.filter(c => visibleIds.has(c.nib.id))
        : node.children;
      const parentNib = node.nib.parentId ? nibMap.get(node.nib.parentId) ?? null : null;

      rows.push({
        nib: node.nib,
        depth: node.depth,
        hasChildren: visibleChildren.length > 0,
        dimmed,
        parentNib,
        // The display parent is the node whose children array holds this node.
        // Threaded top-down from the forest roots (null) so it reflects the
        // node's DISPLAY position after buildViewTree's grouping reparenting,
        // not its raw nib.parentId.
        displayParentId,
      });

      if (!collapsedIds.has(node.nib.id)) {
        // Rows a node holds by ARRANGEMENT are not its children, so they inherit
        // that node's OWN display parent instead of naming it — upholding the
        // RowData.displayParentId invariant. Naming it would hand a reorder a
        // parent id the backend rejects: a synthetic bucket resolves to no nib,
        // and a milestone heading a section accepts no children of any type.
        // (If display containers ever nest, this must resolve the node's own
        // display parent rather than the value threaded down.)
        flatten(node.children, holdsChildrenByDisplay(node) ? displayParentId : node.nib.id);
      }
    }
  }

  flatten(tree, null);

  return { rows, allTags, parentIds };
}
