import type { TreeTableNib, NibFilter, ViewLevel, TreeNode } from "./types";
import { buildViewTree, isBucketId } from "./tree";
import { hasClientFilters, matchesFilter } from "./filter";

export interface RowData {
  nib: TreeTableNib;
  depth: number;
  hasChildren: boolean;
  dimmed: boolean;
  parentNib: TreeTableNib | null;
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
  collapsedIds: Set<string>,
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

    // Walk ancestor chains for visibility
    const ancestorIds = new Set<string>();
    for (const id of matchingIds) {
      const visited = new Set<string>();
      let current = nibMap.get(id);
      while (current?.parentId && !visited.has(current.parentId)) {
        visited.add(current.parentId);
        ancestorIds.add(current.parentId);
        current = nibMap.get(current.parentId);
      }
    }

    visibleIds = new Set<string>([...matchingIds, ...ancestorIds]);
  }

  // Stage 5: Build view tree
  const tree = buildViewTree<TreeTableNib>(allNibs, viewLevel);

  // Stage 5a: Synthetic "No X" bucket nodes are not real nibs, so they never
  // appear in the real-parentId-derived `parentIds` or `visibleIds` sets. Fold
  // them in from the emitted tree so consumers treat them like real containers:
  //   - collapse (parentIds): a bucket with children is collapsible.
  //   - filter visibility (visibleIds): a bucket whose subtree contains a visible
  //     descendant must itself be visible, or flatten() would skip it AND its
  //     children — silently dropping filter-matching loose items (the lens is
  //     lossless, so a client filter must not hide them).
  (function foldBuckets(nodes: TreeNode<TreeTableNib>[]): boolean {
    let anyVisible = false;
    for (const node of nodes) {
      const childVisible = foldBuckets(node.children);
      const isBucket = isBucketId(node.nib.id);
      if (isBucket && node.children.length > 0) {
        parentIds.add(node.nib.id);
      }
      const selfVisible = (visibleIds ? visibleIds.has(node.nib.id) : true) || childVisible;
      if (visibleIds && isBucket && selfVisible) {
        visibleIds.add(node.nib.id);
      }
      anyVisible = anyVisible || selfVisible;
    }
    return anyVisible;
  })(tree);

  // Stage 6: Flatten tree with collapse gating, visibility filtering, dimming, parent resolution
  const rows: RowData[] = [];

  function flatten(nodes: TreeNode<TreeTableNib>[]): void {
    for (const node of nodes) {
      // If we have visibility filtering, skip non-visible nodes
      if (visibleIds && !visibleIds.has(node.nib.id)) continue;

      // Bucket nodes are synthetic structural containers, never real nibs in
      // matchingIds, so they must never be dimmed by a client filter.
      const dimmed = matchingIds && !isBucketId(node.nib.id) ? !matchingIds.has(node.nib.id) : false;
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
      });

      if (!collapsedIds.has(node.nib.id)) {
        flatten(node.children);
      }
    }
  }

  flatten(tree);

  return { rows, allTags, parentIds };
}
