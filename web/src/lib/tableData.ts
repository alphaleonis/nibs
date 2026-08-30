import type { TreeTableNib, NibFilter, ViewLevel, TreeNode, TableSort } from "./types";
import type { Region } from "./ordering/region";
import { buildShapedViewTree, holdsChildrenByDisplay, isSyntheticRowId, viewShapeFor } from "./tree";
import type { ViewShape } from "./tree";
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
 * slot, and a grouping lens decides each nib ONCE — `buildShapedViewTree` asks
 * `place` a single time per nib and reads that memo everywhere after, so the
 * reconciliation a membership model needs (one nib, several plausible
 * containers) happens there rather than being left to whichever consumer looks
 * first. The alternative it rules out is a row key that is not the nib id, which
 * would mean fixing all four consumers above.
 *
 * The fabricated section-container ids are covered too, by a third mechanism:
 * each carries a leading "/" that no filename-derived id can hold AND a last
 * character outside the nanoid charset that no minted id can end on (see
 * `isSyntheticRowId` in tree.ts). The two id spaces are therefore disjoint by
 * construction — a nib cannot be given a section container's id however its file
 * is named — rather than merely unlikely to overlap.
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
  /**
   * The ordering group this row's DISPLAY POSITION is governed by — the list a
   * `reorderNib` moves it within to match what the user sees — or null when no
   * reorder can address it. `rowRegion` is the rule.
   *
   * Single-valued, where server membership is not: a nib has an `order` key
   * among its resolved parent's children and may ALSO have a `milestoneOrder`
   * key in a queue. This picks the one the view put it in, so same region
   * implies co-orderable but different regions does not imply the converse.
   *
   * NOT a replacement for `displayParentId` above, which answers who RENDERS
   * this row as a child. A row inside a container that DECLARES a region takes
   * that declaration, which need not be parent-axis at all. Only absent one does
   * `region` follow `nib.parentId` — server-resolved against the whole store,
   * where `displayParentId` comes from the view tree built out of this response,
   * so in that fallback case the two diverge wherever those disagree: a lens
   * hiding a container, a parent the filter left out, or a cycle member
   * `promotedCycleRoots` severed.
   */
  region: Region | null;
  /**
   * The ordering group this row's children are members of, or null when it
   * declares none — in which case each child falls back to its own resolved
   * parent group. Read off the view tree, so only a container a lens declared one
   * for carries anything here.
   */
  childRegion: Region | null;
}

export interface TableData {
  rows: RowData[];
  allTags: string[];
  parentIds: Set<string>;
  /**
   * Every id the CURRENT view tree contains — real nibs plus the lens's own
   * fabricated section containers. Answers "does this lens have a row for that
   * id at all", which a grouping lens can say no to: it hides a container ranked
   * above its tier while descending into it, so a milestone selected in the Tree
   * view has no row under the Epics lens.
   *
   * Read off `buildShapedViewTree`'s output, BEFORE the flatten, so it is
   * collapse-independent — a collapsed parent must never look like a departed
   * one. It is also filter-independent: a client filter narrows which members
   * are rendered, not which the lens has, and the continuous filter pruner
   * already owns that dimension.
   */
  viewMemberIds: Set<string>;
}

/**
 * The whole `RowData.region` rule, in one place so `flatten` and the row
 * fixtures in tests cannot drift into describing different production.
 *
 * A row in the synthetic id space is a container the view fabricated: it names
 * no nib, so no reorder can address it and it is a member of nothing. Every
 * other row takes the group its enclosing container declared for its children,
 * else the group of its own resolved parent — which is what `parentId` already
 * carries, having arrived from the same function the server groups the PARENT
 * scope by.
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
 * Whether a row's ancestors are shown around it, which decides whether a client
 * filter has to keep a non-matching ancestor for context.
 *
 * Exhaustive switch, no default arm: a fourth view shape is a compile error here
 * rather than silently taking whichever answer a `!== "flat"` string test fell
 * through to.
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

export function buildTableData(
  allNibs: TreeTableNib[],
  filter: NibFilter,
  viewLevel: ViewLevel,
  collapsedIds: ReadonlySet<string>,
  sort: TableSort | null = null,
): TableData {
  const shape = viewShapeFor(viewLevel);

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

  // Stage 5: Build view tree. When a column sort is active, hand the tree
  // builder a node comparator (built from the already-computed nibMap) so the
  // epics/features lenses order their promoted headers + bucket items globally
  // by the sort field instead of by their hidden higher-tier ancestor.
  const nodeComparator = sort ? makeNibComparator(sort, nibMap) : undefined;
  const tree = buildShapedViewTree<TreeTableNib>(allNibs, shape, nodeComparator);

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
  //
  // The walk visits every emitted node exactly once, so `viewMemberIds` is
  // collected here rather than in a pass of its own. It takes EVERY node, not
  // just the display containers: it answers which ids the lens has a row for.
  const viewMemberIds = new Set<string>();
  (function foldDisplayContainers(nodes: TreeNode<TreeTableNib>[]): boolean {
    let anyVisible = false;
    for (const node of nodes) {
      viewMemberIds.add(node.nib.id);
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

  function flatten(
    nodes: TreeNode<TreeTableNib>[],
    displayParentId: string | null,
    enclosingChildRegion: Region | null,
  ): void {
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

      const childRegion = node.childRegion ?? null;
      const region = rowRegion(node.nib.id, node.nib.parentId, enclosingChildRegion);

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
        region,
        childRegion,
      });

      if (!collapsedIds.has(node.nib.id)) {
        // Rows a node holds by ARRANGEMENT are not its children, so they inherit
        // that node's OWN display parent instead of naming it — upholding the
        // RowData.displayParentId invariant. Naming it would hand a reorder a
        // parent id the backend rejects: a synthetic bucket resolves to no nib,
        // and a milestone heading a section accepts no children of any type.
        // (If display containers ever nest, this must resolve the node's own
        // display parent rather than the value threaded down.)
        // The region declaration passed down is this node's OWN, not the one it
        // received: a declaration covers a container's rows, not everything
        // beneath them. Under a queued epic, a subtask carrying no assignment of
        // its own is in no queue at all (ResolvedMilestoneID reads the nib's own
        // `milestone:` and returns "" when it is empty, and an empty group id is
        // memberless in the MILESTONE scope) — it orders under the epic. It
        // normally carries none: the server refuses to assign a nib whose
        // ancestor is already assigned. A hand-authored file can still hold that
        // pair, and the rule is deliberately the same for it — the row is drawn
        // under its parent, so that is the list its position governs.
        flatten(node.children, holdsChildrenByDisplay(node) ? displayParentId : node.nib.id, childRegion);
      }
    }
  }

  flatten(tree, null, null);

  return { rows, allTags, parentIds, viewMemberIds };
}
