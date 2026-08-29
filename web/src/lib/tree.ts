import type { TreeNib, TreeNode, ViewLevel } from "./types";
import { typeRank } from "./typeHierarchy";

export function buildTree<T extends TreeNib>(nibs: T[]): TreeNode<T>[] {
  const nodeMap = new Map<string, TreeNode<T>>();
  const roots: TreeNode<T>[] = [];

  // First pass: create all nodes
  for (const nib of nibs) {
    nodeMap.set(nib.id, { nib, children: [], depth: 0 });
  }

  // One member of every parent cycle is promoted to a root; without that, no
  // member of a cycle qualifies as a root and the whole cycle is dropped.
  const promoted = promotedCycleRoots(nodeMap);

  // Second pass: link children to parents. Severing a promoted nib's edge and
  // making it a root are the same decision here, so a promoted node is always
  // detached and the erasure this guards against cannot come back half-applied.
  // (The Go side splits the two, where severing is what makes its recursion
  // terminate — see promotedCycleRoots in internal/ui/tree.go.)
  for (const nib of nibs) {
    const node = nodeMap.get(nib.id)!;
    if (nib.parentId !== null && nodeMap.has(nib.parentId) && !promoted.has(nib.id)) {
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

/**
 * Picks one member of every parent cycle lying wholly inside `nodeMap`. Every
 * member of such a cycle has its parent present, so none satisfies the ordinary
 * root rule and the cycle would render nowhere at all; promoting one member and
 * severing its parent edge turns the cycle into an ordinary chain, so a
 * malformed hierarchy shows up as an oddity instead of a disappearance.
 *
 * The member with the lowest id wins, matching `promotedCycleRoots` in
 * internal/ui/tree.go — so both views promote the same member and nest a cycle
 * identically. Sibling order still follows each view's own arrangement.
 *
 * Comparison is over UTF-16 code units here and bytes there. Those orders differ
 * only for supplementary-plane characters, which generated ids never contain —
 * but an imported file can carry one, in which case the two views root the cycle
 * at different members and nothing else breaks.
 *
 * A nib has at most one parent, so cycles are disjoint and each is discovered
 * exactly once. Every node is walked once — unseen -> onPath -> settled —
 * making the pass linear in the size of the map.
 */
function promotedCycleRoots<T extends TreeNib>(nodeMap: Map<string, TreeNode<T>>): Set<string> {
  const state = new Map<string, "onPath" | "settled">();
  const promoted = new Set<string>();

  for (const startId of nodeMap.keys()) {
    if (state.has(startId)) continue;
    // Follow this node's parent chain until it leaves the map, ends, or
    // re-enters itself.
    const path: string[] = [];
    let current: string | null = startId;
    while (current !== null) {
      const seen = state.get(current);
      if (seen === "onPath") {
        // The chain closed on itself: the cycle is the path from this node
        // onward. Anything before it merely leads into the cycle.
        const start = path.indexOf(current);
        let lowest = path[start];
        for (let i = start + 1; i < path.length; i++) {
          if (path[i] < lowest) lowest = path[i];
        }
        promoted.add(lowest);
        break;
      }
      // "settled" means already fully explored, along with any cycle beyond it.
      if (seen === "settled") break;
      state.set(current, "onPath");
      path.push(current);
      // Annotated rather than inferred: `current` is assigned from `parentId`
      // below, so inference would be circular.
      const parentId: string | null = nodeMap.get(current)!.nib.parentId;
      current = parentId !== null && nodeMap.has(parentId) ? parentId : null;
    }
    for (const id of path) state.set(id, "settled");
  }

  return promoted;
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

/**
 * Every bucket id leads with a SLASH and ends with an UNDERSCORE. Both are
 * load-bearing, because a nib id can reach the UI by two routes and each
 * character closes one of them.
 *
 * The leading slash closes the filename-derived route: an id read off disk is
 * `nib.ParseFilename(filepath.Base(path))`, a substring of one filename
 * component, and no filesystem admits a path separator inside one. Front matter
 * cannot supply an id either — `Nib.ID` carries `yaml:"-"`.
 *
 * The trailing underscore closes the created-nib route, which the slash does
 * NOT: `nib.NewID(prefix, length)` concatenates an unvalidated caller prefix
 * with a nanoid, so a created id can perfectly well hold a slash — `nibs new
 * --prefix "a/b-"` minted `a/b-ejgn`, and `--prefix "/__no_milestone__"` minted
 * `/__no_milestone__0q1d`. What it can never do is end in "_": the nanoid is
 * drawn from [0-9a-z] and its length is floored above zero at every call site,
 * so a minted id always ends in one of those 36 characters.
 *
 * A bucket id added here must therefore satisfy BOTH — lead with "/" AND end
 * outside [0-9a-z]. The slash alone is not sufficient: `/no-area` would be
 * reachable from `--prefix "/no-are"` under `nibs.id_length: 1`. Both halves are
 * asserted over `BUCKET_IDS` in tree.test.ts, so a new id that satisfies only
 * one fails there rather than reinstating a duplicate-key collision in the
 * table.
 */
const GROUPING_LENSES: Record<Exclude<ViewLevel, "none" | "flat">, LensConfig> = {
  milestones: { grouping: new Set(["milestone"]),     bucketId: "/__no_milestone__",      bucketLabel: "No milestone" },
  epics:      { grouping: new Set(["epic"]),          bucketId: "/__no_epic__",           bucketLabel: "No epic" },
  features:   { grouping: new Set(["feature", "bug"]), bucketId: "/__no_feature_or_bug__", bucketLabel: "No feature or bug" },
};

/**
 * The container tier a lens groups by, derived from the single source of truth
 * (`typeRank`) rather than a hardcoded copy. All grouping types in a lens share
 * one rank (feature and bug are both rank 1), so the first suffices.
 */
function lensRank(cfg: LensConfig): number {
  return typeRank([...cfg.grouping][0]);
}

/**
 * Exact set of synthetic "No X" bucket ids, derived from the lens configs.
 * Exported so the disjointness invariant documented on GROUPING_LENSES is
 * asserted against the real set rather than a copy of it.
 */
export const BUCKET_IDS = new Set<string>(Object.values(GROUPING_LENSES).map(c => c.bucketId));

/**
 * True for ids the view layer fabricated — the synthetic "No X" bucket rows,
 * which carry a `data-nib-id` so delegation reaches them but name no nib.
 *
 * This is an IDENTITY question, and only that: it answers whether a row has a
 * nib behind it, never whether the row is a header. A real nib heading a
 * section of its own answers false and is selectable, openable and a legal
 * action target like any other row; use `holdsChildrenByDisplay` to ask what a
 * node's children mean.
 *
 * Membership is exact against the known bucket ids, and no id a store can
 * produce — parsed from a filename or minted by `nib.NewID` — can equal one of
 * them, because every bucket id both carries a path separator and ends outside
 * the nanoid charset (see GROUPING_LENSES for why it takes both). So this is
 * not merely "unlikely to collide": the two id spaces are disjoint by
 * construction, under any `nibs.prefix` and for any hand-created or imported
 * file. That disjointness is what lets the question be settled from the string
 * alone, with no node to consult.
 */
export function isSyntheticRowId(id: string): boolean {
  return BUCKET_IDS.has(id);
}

/**
 * True when a node's view-tree children are held by ARRANGEMENT rather than
 * parentage — a synthetic bucket sweeping up loose items, or a real nib heading
 * a section of members that are not its children. The rows beneath such a node
 * are not its children, so they must not name it as their backend `parentId`
 * and it must still behave as a container for collapse and filter visibility.
 *
 * Read off the tree rather than declared per row kind: `buildTree` nests a child
 * only under the parent its `parentId` names, so a node holds by arrangement
 * exactly when some child disagrees. A new kind of section therefore needs no
 * second list of row kinds kept in sync with this one.
 *
 * This is a whole-node verdict, which is sound only while a container's members
 * can never ALSO be its structural children — today `VALID_CHILD_TYPES.milestone`
 * is `[]`, so nothing parents under the one type that heads a section (the
 * precondition is asserted in typeHierarchy.test.ts). A future kind admitting
 * both at once would need a per-edge form; taking `.some()` for it would re-root
 * its genuine children onto the container's own display parent.
 */
export function holdsChildrenByDisplay<T extends TreeNib>(node: TreeNode<T>): boolean {
  return node.children.some((child) => child.nib.parentId !== node.nib.id);
}

/**
 * The id of the display container an item would fall under for the given lens,
 * or null if it has none (it sits under a grouping header, is a grouping header
 * itself, or the lens groups nothing). Used to un-collapse an item's enclosing
 * container when revealing it, since a container that holds its rows by display
 * is never their `parentId` and so is missed by an ancestor-chain walk.
 */
export function displayContainerIdForItem<T extends TreeNib>(
  nibMap: Map<string, T>,
  nibId: string,
  viewLevel: ViewLevel,
): string | null {
  // The "none" (full tree) and "flat" (ungrouped) views have no synthetic
  // buckets, so an item never has an enclosing bucket to un-collapse.
  if (viewLevel === "none" || viewLevel === "flat") return null;
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
 *
 * `sortComparator` (optional) is the active column sort's node comparator. When
 * present, the promoted group headers and the bucket's loose items are ordered
 * GLOBALLY by the sort field, instead of by the DFS position of their hidden
 * higher-tier ancestor (the epics/features lenses descend through above-tier
 * containers, so `classify` would otherwise emit headers grouped by that hidden
 * ancestor). Each header keeps its entire subtree unchanged — only the top-level
 * header / bucket-item order changes. `flat` and `none` return before this and
 * are unaffected (their order comes from the pre-sorted input array).
 */
export function buildViewTree<T extends TreeNib>(
  nibs: T[],
  viewLevel: ViewLevel,
  sortComparator?: (a: T, b: T) => number,
): TreeNode<T>[] {
  if (viewLevel === "flat") {
    // Flat view: every nib is an ungrouped depth-0 root — no nesting, no
    // buckets. Preserves incoming order (the manual `order` sequence).
    return nibs.map((nib) => ({ nib, children: [], depth: 0 }));
  }

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

  // Under an active sort, order the promoted headers and the bucket's loose
  // items globally by the sort field. `Array.sort` is stable, so equal-key
  // entries keep their `classify` (DFS/grouped) order as the tiebreak; each
  // header's subtree is untouched.
  if (sortComparator) {
    headers.sort((x, y) => sortComparator(x.nib, y.nib));
    bucketItems.sort((x, y) => sortComparator(x.nib, y.nib));
  }

  const roots: TreeNode<T>[] = [...headers];
  if (bucketItems.length > 0) {
    roots.push(makeBucketNode(cfg.bucketId, `${cfg.bucketLabel} (${bucketItems.length})`, bucketItems));
  }

  // Reset depths relative to new roots.
  setDepths(roots, 0);

  return roots;
}
