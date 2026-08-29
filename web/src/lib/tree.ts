import { VIEW_LEVELS } from "./types";
import type { TreeNib, TreeNode, TreeTableNib, ViewLevel } from "./types";
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

/**
 * The key naming one section of a grouped view. The space is the LENS's to mint:
 * today's type lenses use the heading nib's own id, and a membership lens would
 * use the id its assignment names. Nothing validates a key — see the
 * union-of-sections rule on `buildShapedViewTree` for why that is deliberate.
 */
export type SectionKey = string;

/**
 * Where one nib goes in a grouped view.
 *
 * `hidden` is TYPE-LENS-ONLY today: it is how a container ranked ABOVE the
 * lens's tier loses its own row while everything beneath it keeps one. A
 * membership lens has no notion of a tier and never returns it.
 */
export type Placement =
  /** Placed inside a section by something other than heading it. */
  | { kind: "member"; section: SectionKey }
  /** This nib IS the section's row — still a real, selectable nib. */
  | { kind: "header"; section: SectionKey }
  /** No row of its own; whatever it contains splices up a level. */
  | { kind: "hidden" };

/**
 * How a grouped view arranges nibs into sections.
 *
 * A lens answers per NIB, not per tree, so the two arrangements the table needs
 * — grouping by TYPE along the parent chain, and grouping by ASSIGNMENT, which
 * does not run along it at all — differ only in this object.
 */
export interface GroupingLens<T extends TreeNib = TreeNib> {
  /**
   * Decide where one nib goes. Must be TOTAL and SELF-CONSISTENT: every nib gets
   * an answer, and the same answer every time it is asked about the same nib
   * under the same `byId`.
   *
   * Both halves are load-bearing on the caller side. `buildGroupedTree` asks once
   * per nib and reads a memo thereafter, while `containingSectionRowId` asks
   * again on demand — so a lens that answers a second ask differently makes those
   * two disagree about which row contains a nib.
   */
  place(nib: T, byId: ReadonlyMap<string, T>): Placement;
  /**
   * The section sweeping up everything that belongs to no other. REQUIRED — a
   * lossless view needs somewhere to put a nib that fits nowhere. Its key must
   * live in the synthetic id space (see `isSyntheticRowId`), because no nib
   * heads it and the key is used as its row id verbatim.
   */
  readonly leftover: { key: SectionKey; label: string };
  /**
   * The lens's own order for a section's top-level members, or null for none.
   * An active column sort outranks it: sorting a column means the user asked
   * for that order specifically.
   */
  orderWithinSection?(section: SectionKey): ((a: T, b: T) => number) | null;
  /**
   * Whether a section's rows follow PARENTAGE or PLACEMENT.
   *
   * True (the type lenses): the emitted forest is the structural one, and a nib
   * that claims a section takes its whole subtree with it — so a cycle, a
   * dangling parent and a mis-nested container are arranged exactly as
   * `buildTree` already resolved them, rather than re-derived here.
   *
   * False (a membership lens): membership does not run along parent links, so
   * every nib is positioned by its own placement and the nesting inside a
   * section is rebuilt from whichever nibs landed in it.
   */
  readonly nestHeadersStructurally: boolean;
}

/**
 * What a view level renders as. Closing this as a union is what turns the
 * scattered `viewLevel === "flat"` string tests into exhaustive switches: a
 * fourth shape is then a compile error at every one of them instead of silently
 * taking whichever branch the string test happened to fall through to.
 */
export type ViewShape =
  | { kind: "tree" }
  | { kind: "flat" }
  | { kind: "grouped"; lens: GroupingLens };

/**
 * A lens grouping by nib TYPE: nibs of `grouping` head sections keeping their
 * whole subtree, containers ranked above that tier lose their row but are
 * descended into, and everything else at or below the tier falls into the
 * leftover section. `leftoverKey` must satisfy the rule on `isSyntheticRowId`.
 */
function typeLens(grouping: string[], leftoverKey: SectionKey, leftoverLabel: string): GroupingLens {
  const groupingTypes = new Set(grouping);
  // The container tier this lens groups by, derived from the single source of
  // truth (`typeRank`) rather than a hardcoded copy. All grouping types in a
  // lens share one rank (feature and bug are both rank 1), so the first
  // suffices.
  const tier = typeRank(grouping[0]);

  return {
    leftover: { key: leftoverKey, label: leftoverLabel },
    // Headers keep their subtrees, so a type lens's only members are the loose
    // items in its leftover section — whose order is the walk's, or the active
    // column sort's. There is no third order to declare.
    nestHeadersStructurally: true,

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

const MILESTONE_TYPE_LENS = typeLens(["milestone"], "/__no_milestone__", "No milestone");
const EPIC_TYPE_LENS = typeLens(["epic"], "/__no_epic__", "No epic");
const FEATURE_TYPE_LENS = typeLens(["feature", "bug"], "/__no_feature_or_bug__", "No feature or bug");

/**
 * The shape each view level renders in.
 *
 * EXHAUSTIVE switch, no default arm, declared return type — deliberately, since
 * this is the ONLY thing forcing every view level to say how it groups. A new
 * member of ViewLevel fails to compile here until it declares one; a `default`
 * arm here would let it default into some shape instead, which is the whole
 * hole `ViewShape` exists to close.
 */
export function viewShapeFor(viewLevel: ViewLevel): ViewShape {
  switch (viewLevel) {
    case "none":
      return { kind: "tree" };
    case "flat":
      return { kind: "flat" };
    case "milestones":
      return { kind: "grouped", lens: MILESTONE_TYPE_LENS };
    case "epics":
      return { kind: "grouped", lens: EPIC_TYPE_LENS };
    case "features":
      return { kind: "grouped", lens: FEATURE_TYPE_LENS };
  }
}

/**
 * Exact set of leftover-section keys, derived by asking every view level what it
 * renders as.
 *
 * DERIVED, not listed. The guard in tree.test.ts is only worth anything if every
 * shipped lens is enrolled in it, and a hand-kept list beside `viewShapeFor`
 * enrolls a new lens only if someone remembers to — while the switch above
 * enrolls it or fails to compile. An unenrolled leftover key that misses the
 * `isSyntheticRowId` property makes its own section row classify as a REAL nib
 * on every render: selectable, a legal Delete/batch target, and a drop target.
 *
 * Evaluated after the lens constants and after `viewShapeFor` (a hoisted
 * declaration), so the module-level initialization is well ordered.
 */
export const BUCKET_IDS = new Set<string>(
  VIEW_LEVELS.flatMap((level) => {
    const shape = viewShapeFor(level);
    return shape.kind === "grouped" ? [shape.lens.leftover.key] : [];
  }),
);

/**
 * The row id for a section no nib heads.
 *
 * The leftover key is the lens's own literal and already lives in the synthetic
 * id space, so it is used as it is. Every OTHER key is escaped into that space,
 * because a lens may derive one from a nib's stored assignment — so a key can
 * perfectly well equal the id of a nib rendered elsewhere in the same view, and
 * a container carrying it verbatim would put that id in `rows` twice. Escaping
 * is injective, so two sections can never land on one row id either.
 */
function sectionRowId(key: SectionKey, lens: GroupingLens): string {
  return key === lens.leftover.key ? key : `/section:${key}_`;
}

/**
 * True for ids the view layer fabricated — the section container rows, which
 * carry a `data-nib-id` so delegation reaches them but name no nib.
 *
 * This is an IDENTITY question, and only that: it answers whether a row has a
 * nib behind it, never whether the row is a header. A real nib heading a
 * section of its own answers false and is selectable, openable and a legal
 * action target like any other row; use `holdsChildrenByDisplay` to ask what a
 * node's children mean.
 *
 * A fabricated id leads with a SLASH and ends OUTSIDE [0-9a-z]. Both are
 * load-bearing, because a nib id can reach the UI by two routes and each half of
 * the test closes one of them.
 *
 * The leading slash closes the filename-derived route: an id read off disk is
 * `nib.ParseFilename(filepath.Base(path))`, a substring of one filename
 * component, and no filesystem admits a path separator inside one. Front matter
 * cannot supply an id either — `Nib.ID` carries `yaml:"-"`. The last character
 * does NOT close this route: a hand-authored or imported file names its own id,
 * and `FOO.md`, `foo#.md` and `tnib-x9z2_.md` all load with those ids intact.
 *
 * The last character closes the created-nib route: `nib.NewID(prefix, length)`
 * appends a nanoid drawn from exactly those 36 characters, its length floored
 * above zero at every call site, so a created id can never END outside [0-9a-z]
 * however arbitrary the caller's prefix.
 *
 * The slash would close that route too, as things stand: `Core.Create` puts every
 * id through `nib.ValidateIDForFilename`, which refuses a path separator, and
 * both `--prefix` and a store's `nibs.prefix` reach it — `--prefix "a/b-"` and
 * `--prefix "/__no_milestone__"` are each refused with VALIDATION_ERROR. The
 * conjunction is kept because the two halves rest on different mechanisms: that
 * refusal is a create-time gate in another layer, while the nanoid tail is a
 * property of every id `NewID` composes at all. Relax the gate and the last
 * character is the only thing left between a caller's prefix and a bucket id.
 *
 * So the predicate IS the disjointness argument, not a list of ids that happen
 * to satisfy it. Testing the property rather than membership in a fixed table is
 * what lets a container id be DERIVED from an arbitrary section key (see
 * `sectionRowId`), which a lens grouping by a stored assignment needs. It also
 * puts the burden on the LENS: a leftover key meeting only one half — `/no-area`
 * leads with a slash but ends in `a` — makes its own section row answer FALSE
 * here and classify as a real nib. Every shipped `leftover.key` is asserted
 * against both halves in tree.test.ts, against the derived `BUCKET_IDS`, so such
 * a key fails there rather than reaching a render.
 */
export function isSyntheticRowId(id: string): boolean {
  return id.startsWith("/") && !/[0-9a-z]$/.test(id);
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
 * The id of the row CONTAINING this item in the given view — the section it
 * lands in — or null when it has none: it heads a section itself, the lens hides
 * it, or the view is not grouped at all.
 *
 * Used to un-collapse an item's enclosing section when revealing it. An
 * ancestor-chain walk cannot find that section on its own: a container holding
 * its rows by arrangement is never their `parentId`, and under a membership lens
 * even a real header is not their ancestor.
 *
 * This asks `place` rather than restating its rule, so the answer cannot drift
 * from where `buildShapedViewTree` actually put the row. Asking again rather than
 * sharing the builder's memo is sound because `place` is contracted to answer the
 * same way for the same inputs; the cost is a placement or two recomputed per
 * call.
 */
export function containingSectionRowId<T extends TreeNib>(
  byId: ReadonlyMap<string, T>,
  nibId: string,
  viewLevel: ViewLevel,
): string | null {
  const shape = viewShapeFor(viewLevel);
  if (shape.kind !== "grouped") return null;
  const self = byId.get(nibId);
  if (self === undefined) return null;

  const placement = shape.lens.place(self, byId);
  if (placement.kind !== "member") return null;

  // A section keyed on a nib that does not actually head it (a dangling
  // assignment, say) is drawn by a fabricated container instead.
  const claimant = byId.get(placement.section);
  const claim = claimant !== undefined ? shape.lens.place(claimant, byId) : null;
  const headed = claim?.kind === "header" && claim.section === placement.section;
  return headed ? placement.section : sectionRowId(placement.section, shape.lens);
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
 * Build a fabricated section-container node — a row for a section no nib heads.
 *
 * The literal is annotated `TreeTableNib`, the widest shape any caller
 * instantiates `T` with, so the COMPILER checks it: a field added there fails
 * here rather than reaching a row as `undefined`. The cast is still needed
 * because `T` stays open — a caller could instantiate it with a subtype
 * demanding fields this literal cannot know about — but it no longer hides
 * anything the codebase itself declares.
 */
function makeSectionNode<T extends TreeNib>(id: string, title: string, children: TreeNode<T>[]): TreeNode<T> {
  const sectionNib: TreeTableNib = {
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
    milestone: "",
    milestoneOrder: "",
    blockingIds: [],
    blockedByIds: [],
    etag: "",
  };
  return { nib: sectionNib as unknown as T, children, depth: 0 };
}

/** One section of a grouped view, while it is being assembled. */
interface Section<T extends TreeNib> {
  key: SectionKey;
  /** The nib whose row IS this section, when one claimed it. */
  header: TreeNode<T> | null;
  /** Rows placed into the section by something other than heading it. */
  members: TreeNode<T>[];
}

/**
 * Reframe the nib list into the given view shape. Every work item is preserved
 * (lossless) in all three shapes.
 *
 * `sortComparator` (optional) is the active column sort's node comparator. Under
 * a grouped shape it orders the sections that a nib heads, and the members
 * within each section, GLOBALLY by the sort field — instead of by the position
 * of the hidden higher-tier ancestor the walk descended through. Each header
 * keeps its subtree unchanged. `flat` and `tree` take their order from the
 * (pre-sorted) input array and ignore it.
 */
export function buildShapedViewTree<T extends TreeNib>(
  nibs: T[],
  shape: ViewShape,
  sortComparator?: (a: T, b: T) => number,
): TreeNode<T>[] {
  switch (shape.kind) {
    case "flat":
      // Every nib an ungrouped depth-0 root — no nesting, no sections.
      // Preserves incoming order (the manual `order` sequence).
      return nibs.map((nib) => ({ nib, children: [], depth: 0 }));
    case "tree":
      // Full tree, nothing hidden; depths already set by buildTree.
      return buildTree(nibs);
    case "grouped":
      return buildGroupedTree(nibs, shape.lens, sortComparator);
  }
}

/** `buildShapedViewTree` addressed by view level, for the many callers that hold
 *  one rather than a shape. */
export function buildViewTree<T extends TreeNib>(
  nibs: T[],
  viewLevel: ViewLevel,
  sortComparator?: (a: T, b: T) => number,
): TreeNode<T>[] {
  return buildShapedViewTree(nibs, viewShapeFor(viewLevel), sortComparator);
}

function buildGroupedTree<T extends TreeNib>(
  nibs: T[],
  lens: GroupingLens,
  sortComparator?: (a: T, b: T) => number,
): TreeNode<T>[] {
  const byId = new Map<string, T>();
  for (const nib of nibs) byId.set(nib.id, nib);

  // Every placement is decided here, before any assembly. `byId` is complete
  // before this loop and never mutated after it, and `place` is contracted total
  // and self-consistent, so asking again is equivalent —
  // `containingSectionRowId` leans on exactly that and asks again outside the
  // build rather than sharing this map.
  //
  // It is worth being blunt about what this map is NOT, because two earlier
  // readings of it were wrong. It is not load-bearing for correctness: the
  // assembly below reads each placement exactly once, at one of two mutually
  // exclusive sites, so an inconsistent lens could not render a nib twice even
  // without it. Nor is it a speed optimization — it is eager, one call per nib,
  // where a lazy read would ask only for the nodes the walk reaches, which under
  // the type lenses is far fewer.
  //
  // What it buys is that `place` is called in exactly one loop, so the assembly
  // is a read over settled decisions rather than a walk that interleaves lens
  // calls with tree building. That is a legibility choice, paid for in calls.
  const placements = new Map<string, Placement>();
  for (const nib of nibs) placements.set(nib.id, lens.place(nib, byId));

  // Sections are the UNION of every key any placement produced, in discovery
  // order. A key that nothing heads still CREATES a section, so a dangling
  // assignment renders as a visibly odd section rather than deleting its rows.
  const sections = new Map<SectionKey, Section<T>>();
  const sectionFor = (key: SectionKey): Section<T> => {
    let section = sections.get(key);
    if (section === undefined) {
      section = { key, header: null, members: [] };
      sections.set(key, section);
    }
    return section;
  };

  if (lens.nestHeadersStructurally) {
    // Rows follow PARENTAGE. Descend the structural forest; where a nib claims a
    // section it takes its whole subtree along and the descent stops, so each
    // node is reached exactly once. The forest is freshly allocated and private
    // to this call, so re-rooting its nodes is safe.
    const walk = (nodes: TreeNode<T>[]): void => {
      for (const node of nodes) {
        const placement = placements.get(node.nib.id)!;
        if (placement.kind === "hidden") {
          walk(node.children);
          continue;
        }
        const section = sectionFor(placement.section);
        // A second nib claiming a section already headed becomes a member of it,
        // so a lens handing two nibs one key loses neither.
        if (placement.kind === "header" && section.header === null) {
          section.header = node;
        } else {
          section.members.push(node);
        }
      }
    };
    walk(buildTree(nibs));
  } else {
    // Rows follow PLACEMENT. Membership does not run along parent links, so
    // every nib is positioned by its own answer; `buildTree` then rebuilds the
    // nesting from whichever nibs landed in the same section (one whose parent
    // is elsewhere simply becomes a top-level member).
    const memberNibs = new Map<SectionKey, T[]>();
    for (const nib of nibs) {
      const placement = placements.get(nib.id)!;
      if (placement.kind === "hidden") continue;
      const section = sectionFor(placement.section);
      if (placement.kind === "header" && section.header === null) {
        section.header = { nib, children: [], depth: 0 };
      } else {
        const list = memberNibs.get(placement.section);
        if (list === undefined) memberNibs.set(placement.section, [nib]);
        else list.push(nib);
      }
    }
    for (const [key, list] of memberNibs) sectionFor(key).members = buildTree(list);
  }

  // Sections a real nib heads come first — ordered by the active column sort
  // when there is one, else by discovery. A section nothing heads has no nib to
  // sort by, so it follows them; the leftover is last either way, since
  // "everything else" reads wrong anywhere but the end.
  const headed: Section<T>[] = [];
  const headless: Section<T>[] = [];
  let leftover: Section<T> | null = null;
  for (const section of sections.values()) {
    if (section.key === lens.leftover.key) leftover = section;
    else if (section.header !== null) headed.push(section);
    else headless.push(section);
  }
  if (sortComparator) {
    // `Array.sort` is stable, so equal-key sections keep their discovery order.
    headed.sort((x, y) => sortComparator(x.header!.nib, y.header!.nib));
  }

  const ordered = [...headed, ...headless, ...(leftover !== null ? [leftover] : [])];
  const roots = ordered.map((section) => assembleSection(section, lens, sortComparator));

  // Reset depths relative to the new roots.
  setDepths(roots, 0);

  return roots;
}

/** Turn one assembled section into the node that renders it. */
function assembleSection<T extends TreeNib>(
  section: Section<T>,
  lens: GroupingLens,
  sortComparator?: (a: T, b: T) => number,
): TreeNode<T> {
  const order: ((a: T, b: T) => number) | null =
    sortComparator ?? lens.orderWithinSection?.(section.key) ?? null;
  const members = order ? [...section.members].sort((x, y) => order(x.nib, y.nib)) : section.members;

  if (section.header !== null) {
    // Under `nestHeadersStructurally` the header arrived with its own subtree
    // attached; anything placed into the section joins it.
    return { ...section.header, children: [...section.header.children, ...members] };
  }
  const label = section.key === lens.leftover.key ? lens.leftover.label : section.key;
  return makeSectionNode(sectionRowId(section.key, lens), `${label} (${members.length})`, members);
}
