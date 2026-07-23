import type { TreeNib, TreeNode, ViewLevel } from "./types";
import { typeRank } from "./typeHierarchy";

export function buildTree<T extends TreeNib>(nibs: T[]): TreeNode<T>[] {
  const nodeMap = new Map<string, TreeNode<T>>();
  const roots: TreeNode<T>[] = [];

  // First pass: create all nodes
  for (const nib of nibs) {
    nodeMap.set(nib.id, { nib, children: [], depth: 0 });
  }

  // Second pass: link children to parents
  for (const nib of nibs) {
    const node = nodeMap.get(nib.id)!;
    if (nib.parentId !== null && nodeMap.has(nib.parentId)) {
      const parent = nodeMap.get(nib.parentId)!;
      parent.children.push(node);
    } else {
      roots.push(node);
    }
  }

  // Third pass: compute depths via recursive traversal
  setDepths(roots, 0);

  return roots;
}

function setDepths<T extends TreeNib>(nodes: TreeNode<T>[], depth: number): void {
  for (const node of nodes) {
    node.depth = depth;
    setDepths(node.children, depth + 1);
  }
}

interface LensConfig {
  grouping: Set<string>;
  bucketId: string;
  bucketLabel: string;
}

const GROUPING_LENSES: Record<Exclude<ViewLevel, "none">, LensConfig> = {
  milestones: { grouping: new Set(["milestone"]),     bucketId: "__no_milestone__",      bucketLabel: "No milestone" },
  epics:      { grouping: new Set(["epic"]),          bucketId: "__no_epic__",           bucketLabel: "No epic" },
  features:   { grouping: new Set(["feature", "bug"]), bucketId: "__no_feature_or_bug__", bucketLabel: "No feature or bug" },
};

/**
 * The container tier a lens groups by, derived from the single source of truth
 * (`typeRank`) rather than a hardcoded copy. All grouping types in a lens share
 * one rank (feature and bug are both rank 1), so the first suffices.
 */
function lensRank(cfg: LensConfig): number {
  return typeRank([...cfg.grouping][0]);
}

/** Exact set of synthetic "No X" bucket ids, derived from the lens configs. */
const BUCKET_IDS = new Set<string>(Object.values(GROUPING_LENSES).map(c => c.bucketId));

/**
 * True for synthetic display-bucket ids ("No X"). Membership is exact against the
 * known bucket ids, so it can never collide with a real nib id — even under a
 * user-configured `nibs.prefix` that happens to start with underscores.
 */
export function isBucketId(id: string): boolean {
  return BUCKET_IDS.has(id);
}

/**
 * The bucket id an item would fall under for the given lens, or null if the item
 * is not bucketed (it sits under a grouping header, is a grouping header itself,
 * or the lens has no buckets). Used to un-collapse an item's enclosing bucket
 * when revealing it, since a bucket is never any real nib's `parentId`.
 */
export function bucketIdForItem<T extends TreeNib>(
  nibMap: Map<string, T>,
  nibId: string,
  viewLevel: ViewLevel,
): string | null {
  if (viewLevel === "none") return null;
  const cfg = GROUPING_LENSES[viewLevel];
  const self = nibMap.get(nibId);
  // A container ranked above the grouping tier is hidden outright by
  // buildViewTree (its row is suppressed, not swept into a bucket), so it has no
  // enclosing bucket. Only the item's OWN rank matters here — an above-tier
  // *ancestor* (e.g. a bare milestone over a loose task in the Epics lens) still
  // leaves the item itself in the bucket.
  if (self && typeRank(self.type) > lensRank(cfg)) return null;
  const visited = new Set<string>();
  let current = self;
  while (current && !visited.has(current.id)) {
    visited.add(current.id);
    // A grouping-type ancestor (or the item itself) means it lives under that
    // header, not the bucket.
    if (cfg.grouping.has(current.type)) return null;
    if (current.parentId === null) break;
    const parent = nibMap.get(current.parentId);
    if (!parent) break;
    current = parent;
  }
  return cfg.bucketId;
}

/**
 * Finds the node with the given id anywhere in a (view) tree. Returns null if
 * absent. Depth-first; the tree is shallow so recursion is fine.
 */
function findNode<T extends TreeNib>(nodes: TreeNode<T>[], id: string): TreeNode<T> | null {
  for (const node of nodes) {
    if (node.nib.id === id) return node;
    const found = findNode(node.children, id);
    if (found) return found;
  }
  return null;
}

/**
 * Collects the ids of every descendant of `rootId` within the given tree,
 * EXCLUDING `rootId` itself. Returns an empty set when `rootId` is not present.
 *
 * The tree must be the DISPLAYED view tree (from `buildViewTree`), not the raw
 * nib list: the grouping lens reparents nodes (headers keep their subtree,
 * above-tier containers are hidden, loose items fall into a synthetic "No X"
 * bucket whose id is not a real `parentId`). Walking `node.children` here —
 * rather than raw `nib.parentId` — yields exactly the rows currently shown under
 * the subtree. A visited guard makes the walk safe even if a malformed tree ever
 * contained a cycle.
 */
export function collectDescendantIds<T extends TreeNib>(
  tree: TreeNode<T>[],
  rootId: string,
): Set<string> {
  const result = new Set<string>();
  const root = findNode(tree, rootId);
  if (!root) return result;

  const stack: TreeNode<T>[] = [...root.children];
  while (stack.length > 0) {
    const node = stack.pop()!;
    if (result.has(node.nib.id)) continue; // cycle guard
    result.add(node.nib.id);
    for (const child of node.children) stack.push(child);
  }
  return result;
}

/**
 * Build a synthetic "No X" bucket node. This node is not a real member of the
 * input set, so TypeScript cannot verify it against the open generic `T` (a
 * caller could instantiate `T` with a subtype requiring extra fields). We cast
 * through `unknown` deliberately: the literal must carry every field any
 * concrete `T` is expected to need — it currently covers all of TreeTableNib
 * (blockingIds/blockedByIds are read by TreeTableRow). If `T` gains new required
 * fields, add them here too.
 */
function makeBucketNode<T extends TreeNib>(id: string, title: string, children: TreeNode<T>[]): TreeNode<T> {
  const bucketNib = {
    id,
    title,
    status: "",
    type: "",
    priority: "",
    estimate: "",
    tags: [],
    createdAt: "",
    updatedAt: "",
    parentId: null,
    blockingIds: [],
    blockedByIds: [],
  } as unknown as T;
  return { nib: bucketNib, children, depth: 0 };
}

/**
 * Reframe the full tree through a grouping "lens". Every work item is preserved
 * (lossless): items of the lens's grouping type(s) become headers keeping their
 * entire subtree; containers ranked above the tier are hidden as rows but
 * descended into; everything at or below the tier that isn't a grouping type
 * falls into a single "No X" bucket. The `none` lens returns the full tree.
 */
export function buildViewTree<T extends TreeNib>(nibs: T[], viewLevel: ViewLevel): TreeNode<T>[] {
  const fullTree = buildTree(nibs);

  if (viewLevel === "none") {
    // Full tree, nothing hidden; depths already set by buildTree.
    return fullTree;
  }

  const cfg = GROUPING_LENSES[viewLevel];
  const gRank = lensRank(cfg);
  const headers: TreeNode<T>[] = [];
  const bucketItems: TreeNode<T>[] = [];

  // Single nearest-grouping-ancestor rule. The forest from buildTree is freshly
  // allocated and private to this call, so reparenting its nodes is safe. Each
  // node is classified exactly once (headers and bucket items stop descent;
  // only above-tier containers are descended-through), so there is no duplication.
  function classify(nodes: TreeNode<T>[]): void {
    for (const node of nodes) {
      const type = node.nib.type;
      if (cfg.grouping.has(type)) {
        // Group header: keep its entire subtree verbatim, stop descending.
        headers.push(node);
      } else if (typeRank(type) > gRank) {
        // Container above the tier: hide this row, descend into children.
        classify(node.children);
      } else {
        // Bucket item: keep its subtree verbatim, stop descending.
        bucketItems.push(node);
      }
    }
  }

  classify(fullTree);

  const roots: TreeNode<T>[] = [...headers];
  if (bucketItems.length > 0) {
    roots.push(makeBucketNode(cfg.bucketId, `${cfg.bucketLabel} (${bucketItems.length})`, bucketItems));
  }

  // Reset depths relative to new roots.
  setDepths(roots, 0);

  return roots;
}
