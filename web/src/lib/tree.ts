import type { TreeNib, TreeNode, TreeTableNib } from "./types";
import { MILESTONE_TYPE } from "./membership";
import type { SectionMeaning } from "./ordering/sectionMeaning";

export function buildTree<T extends TreeNib>(nibs: T[]): TreeNode<T>[] {
  const nodeMap = new Map<string, TreeNode<T>>();
  const roots: TreeNode<T>[] = [];

  for (const nib of nibs) {
    nodeMap.set(nib.id, { nib, children: [], depth: 0 });
  }

  // Without a promoted member, no member of a cycle qualifies as a root and the
  // whole cycle is dropped.
  const promoted = promotedCycleRoots(nodeMap);

  // Severing a promoted nib's edge and rooting it are one branch, so they cannot
  // come apart.
  for (const nib of nibs) {
    const node = nodeMap.get(nib.id)!;
    if (nib.parentId !== null && nodeMap.has(nib.parentId) && !promoted.has(nib.id)) {
      const parent = nodeMap.get(nib.parentId)!;
      parent.children.push(node);
    } else {
      roots.push(node);
    }
  }

  setDepths(roots, 0);

  return roots;
}

/**
 * Picks the lowest-id member of every parent cycle lying wholly inside
 * `nodeMap`, for `buildTree` to promote to a root.
 *
 * Keep the rule in agreement with `promotedCycleRoots` in internal/ui/tree.go.
 * This compares UTF-16 code units and Go compares bytes, so ids holding
 * supplementary-plane characters can promote different members.
 *
 * Each node is walked once: unseen -> onPath -> settled.
 */
function promotedCycleRoots<T extends TreeNib>(nodeMap: Map<string, TreeNode<T>>): Set<string> {
  const state = new Map<string, "onPath" | "settled">();
  const promoted = new Set<string>();

  for (const startId of nodeMap.keys()) {
    if (state.has(startId)) continue;
    const path: string[] = [];
    let current: string | null = startId;
    while (current !== null) {
      const seen = state.get(current);
      if (seen === "onPath") {
        // The cycle is the path from this node onward.
        const start = path.indexOf(current);
        let lowest = path[start];
        for (let i = start + 1; i < path.length; i++) {
          if (path[i] < lowest) lowest = path[i];
        }
        promoted.add(lowest);
        break;
      }
      if (seen === "settled") break;
      state.set(current, "onPath");
      path.push(current);
      // Annotated: `current` is assigned from it, so inference would be circular.
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
 * The key naming one section of a grouped view, minted by the lens: type lenses
 * use the heading nib's id, a membership lens the assignment's value (a nib id
 * for milestones, a declared path for areas). Keys are not validated; see
 * `GroupingLens.declares`.
 */
export type SectionKey = string;

/**
 * The key of a lens's leftover section. It satisfies `isSyntheticRowId` by
 * construction ("_" is outside [0-9a-z]) and is stricter than the predicate,
 * which also accepts e.g. `/no-area~`.
 */
export type LeftoverKey = `/__${string}__`;

/**
 * A section a lens declares, rendered whether or not anything lands in it.
 * Array index is its order and `children` its nesting.
 */
export interface DeclaredSection extends SectionDisplay {
  readonly key: SectionKey;
  /** Required: `[]` is a leaf you wrote, not a question you skipped. */
  readonly children: readonly DeclaredSection[];
}

/**
 * What a section row shows besides its count (`RowSection.count`). Every field
 * is required, empty meaning unset, as on `AreaNode`.
 */
export interface SectionDisplay {
  readonly label: string;
  readonly description: string;
  /** A hex code or a bare color name. */
  readonly color: string;
}

/** What a lens states up front — a forest of sections, or nothing. */
export type SectionDeclaration =
  | { readonly kind: "none" }
  | { readonly kind: "forest"; readonly roots: readonly DeclaredSection[] };

/** Whether a section exists because a placement named it, or because the lens
 *  declared it. */
export type SectionPersistence = "discovered" | "declared";

/** The section facts of a node that is a section; one optional on `TreeNode`. */
export interface SectionMeta {
  readonly key: SectionKey;
  readonly persistence: SectionPersistence;
  readonly meaning: SectionMeaning;
  /** Not the count, which depends on client filtering: see `RowSection.count`. */
  readonly display: SectionDisplay;
}

/**
 * What each persistence implies. Read this rather than testing the string, so a
 * new persistence fails to compile until it answers both.
 */
export const SECTION_RULES: Record<
  SectionPersistence,
  { readonly rendersWhenEmpty: boolean; readonly placedByDeclaration: boolean }
> = {
  discovered: { rendersWhenEmpty: false, placedByDeclaration: false },
  declared: { rendersWhenEmpty: true, placedByDeclaration: true },
};

/**
 * Where one nib goes in a grouped view. Only type lenses return `hidden`, for a
 * container ranked above the lens's tier.
 */
export type Placement =
  /** Placed inside a section by something other than heading it. */
  | { kind: "member"; section: SectionKey }
  /** This nib IS the section's row — still a real, selectable nib. */
  | { kind: "header"; section: SectionKey }
  /** No row of its own; whatever it contains splices up a level. */
  | { kind: "hidden" };

/** How a grouped view arranges nibs into sections, decided per nib. */
export interface GroupingLens<T extends TreeNib = TreeNib> {
  /** Where one nib goes. Must answer every nib, and the same way each time for
   *  the same nib and `byId`. */
  place(nib: T, byId: ReadonlyMap<string, T>): Placement;
  /** The section for nibs that fit no other. Its key is used verbatim as the row
   *  id, so it must satisfy `isSyntheticRowId`. */
  readonly leftover: { readonly key: LeftoverKey; readonly label: string };
  /**
   * Sections that exist whether or not anything lands in them. Declaring does
   * not close the section space: a placement naming an undeclared key still
   * mints its own section, so a retired assignment renders visibly instead of
   * merging into the leftover.
   */
  readonly declares: SectionDeclaration;
  /** The lens's order for a section's top-level members, or null. An active
   *  column sort takes precedence. */
  orderWithinSection(section: SectionKey): ((a: T, b: T) => number) | null;
  /**
   * What one section means: the ordering group its rows belong to, and what a
   * drop into it does. Asked per key, since undeclared and leftover sections need
   * a meaning too. Type lenses answer `GOVERNS_NOTHING`.
   *
   * Every row placed in section S must satisfy the server's group resolution for
   * `meaning(S).memberRegion`, so mint milestone section keys from the resolved
   * assignment, never the raw `milestone:` field.
   *
   * A `memberRegion` applies to every member, so give a catch-all section null
   * and let each row fall back to its own parent group.
   */
  meaning(section: SectionKey): SectionMeaning;
  /**
   * True: rows follow parentage, and a nib claiming a section brings its whole
   * `buildTree` subtree. False: every nib is positioned by its own placement,
   * and each section's nesting is rebuilt from the nibs that landed in it.
   */
  readonly nestHeadersStructurally: boolean;
}

/** What a view level renders as. Switch over `kind` exhaustively. */
export type ViewShape =
  | { kind: "tree" }
  | { kind: "flat" }
  | { kind: "grouped"; lens: GroupingLens };

/**
 * The row id for a section no nib heads. The leftover key is already synthetic;
 * any other key may equal a real nib id in the same view, so it is escaped,
 * injectively, into the synthetic id space.
 */
function sectionRowId(key: SectionKey, lens: GroupingLens): string {
  return key === lens.leftover.key ? key : `/section:${key}_`;
}

/**
 * True for ids the view layer fabricated for section rows, which name no nib.
 * An identity test only: a real nib heading a section answers false. Use
 * `holdsChildrenByDisplay` to ask what a node's children mean.
 *
 * A fabricated id leads with "/" and ends outside [0-9a-z]. Each half keeps it
 * out of one source of real ids, so keep both:
 * - Loaded ids come from `nib.ParseFilename` over one filename component, which
 *   cannot hold a separator (`Nib.ID` is `yaml:"-"`). They may end in anything.
 * - Created ids end in a `nib.NewID` nanoid over [0-9a-z], whatever the prefix.
 *
 * Test the property, not a list: `sectionRowId` derives ids from arbitrary keys.
 */
export function isSyntheticRowId(id: string): boolean {
  return id.startsWith("/") && !/[0-9a-z]$/.test(id);
}

/**
 * True when a node's children are held by arrangement rather than parentage: a
 * synthetic bucket, or a real nib heading a section of members that are not its
 * children. Those rows must not name it as their `parentId`, and it still acts
 * as a container for collapse and filter visibility.
 *
 * The verdict is per node, so it holds only while a section's placed members
 * and structural children never share a node — a milestone admits no children
 * (`VALID_CHILD_TYPES.milestone` is `[]`). A kind admitting both needs a
 * per-edge form, or its genuine children get re-rooted.
 */
export function holdsChildrenByDisplay<T extends TreeNib>(node: TreeNode<T>): boolean {
  return node.children.some((child) => child.nib.parentId !== node.nib.id);
}

/**
 * Build the node for a section no nib heads. The literal is typed
 * `TreeTableNib` so a field added there fails to compile here; the cast remains
 * because `T` is open.
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
    area: "",
    blockingIds: [],
    blockedByIds: [],
    etag: "",
  };
  return { nib: sectionNib as unknown as T, children, depth: 0 };
}

/** One section of a grouped view, while it is being assembled. */
interface Section<T extends TreeNib> {
  key: SectionKey;
  persistence: SectionPersistence;
  /** Null for an undeclared section. */
  declared: SectionDisplay | null;
  /** The nib whose row is this section, when one claimed it. */
  header: TreeNode<T> | null;
  members: TreeNode<T>[];
  /** Sections declared inside this one, in order; emitted by this section. */
  declaredChildren: Section<T>[];
}

/**
 * Reframe the nib list into the given view shape without dropping any nib.
 *
 * `sortComparator` is used only by grouped shapes: it orders headed sections and
 * each section's members globally by the sort field. `flat` and `tree` keep the
 * input order.
 */
export function buildShapedViewTree<T extends TreeNib>(
  nibs: T[],
  shape: ViewShape,
  sortComparator?: (a: T, b: T) => number,
): TreeNode<T>[] {
  switch (shape.kind) {
    case "flat":
      return nibs.map((nib) => ({ nib, children: [], depth: 0 }));
    case "tree":
      // Milestones sit outside the parent graph, so this shape omits their rows;
      // the Milestone column shows that axis. Filter before `buildTree` so a nib
      // hand-parented to a milestone is rooted rather than dropped.
      return buildTree(nibs.filter((nib) => nib.type !== MILESTONE_TYPE));
    case "grouped":
      return buildGroupedTree(nibs, shape.lens, sortComparator);
  }
}

function buildGroupedTree<T extends TreeNib>(
  nibs: T[],
  lens: GroupingLens,
  sortComparator?: (a: T, b: T) => number,
): TreeNode<T>[] {
  const byId = new Map<string, T>();
  for (const nib of nibs) byId.set(nib.id, nib);

  // One `place` call per nib, up front; assembly below only reads the answers.
  const placements = new Map<string, Placement>();
  for (const nib of nibs) placements.set(nib.id, lens.place(nib, byId));

  // Every key any placement produces becomes a section, headed or not, in the
  // order first reached.
  const sections = new Map<SectionKey, Section<T>>();
  const sectionFor = (key: SectionKey): Section<T> => {
    let section = sections.get(key);
    if (section === undefined) {
      section = {
        key,
        persistence: "discovered",
        declared: null,
        header: null,
        members: [],
        declaredChildren: [],
      };
      sections.set(key, section);
    }
    return section;
  };

  // Seeded through `sectionFor`, so a placement naming a declared key reuses the
  // declared section.
  const declaredRoots: Section<T>[] = [];
  if (lens.declares.kind === "forest") {
    const seed = (nodes: readonly DeclaredSection[], into: Section<T>[]): void => {
      for (const node of nodes) {
        // The leftover is emitted separately, so declaring its key would emit
        // that section twice.
        if (node.key === lens.leftover.key) {
          throw new Error(
            `declared section ${JSON.stringify(node.key)} collides with the lens's leftover key`,
          );
        }
        // A repeated key resolves to the same section: emitted twice, or, under
        // its own ancestor, recursing without bound in `assembleSection`.
        // `sections` holds only seeded keys at this point.
        if (sections.has(node.key)) {
          throw new Error(
            `declared section ${JSON.stringify(node.key)} appears twice in the forest`,
          );
        }
        const section = sectionFor(node.key);
        section.persistence = "declared";
        section.declared = { label: node.label, description: node.description, color: node.color };
        into.push(section);
        seed(node.children, section.declaredChildren);
      }
    };
    seed(lens.declares.roots, declaredRoots);
  }

  if (lens.nestHeadersStructurally) {
    // A nib claiming a section takes its subtree along and the descent stops
    // there. The forest is private to this call, so re-rooting nodes is safe.
    const walk = (nodes: TreeNode<T>[]): void => {
      for (const node of nodes) {
        const placement = placements.get(node.nib.id)!;
        if (placement.kind === "hidden") {
          walk(node.children);
          continue;
        }
        const section = sectionFor(placement.section);
        // A second nib claiming a headed section becomes a member of it.
        if (placement.kind === "header" && section.header === null) {
          section.header = node;
        } else {
          section.members.push(node);
        }
      }
    };
    walk(buildTree(nibs));
  } else {
    // `buildTree` rebuilds nesting within each section; a nib whose parent
    // landed elsewhere becomes a top-level member.
    //
    // Headers get their own first pass so sections are minted in header order.
    // The input is sorted flat across all nibs, so members often precede their
    // header.
    const memberNibs = new Map<SectionKey, T[]>();
    for (const nib of nibs) {
      const placement = placements.get(nib.id)!;
      if (placement.kind !== "header") continue;
      const section = sectionFor(placement.section);
      if (section.header === null) section.header = { nib, children: [], depth: 0 };
    }
    for (const nib of nibs) {
      const placement = placements.get(nib.id)!;
      if (placement.kind === "hidden") continue;
      // Skip the header itself; a second nib claiming the section falls through
      // as a member.
      if (sectionFor(placement.section).header?.nib.id === nib.id) continue;
      const list = memberNibs.get(placement.section);
      if (list === undefined) memberNibs.set(placement.section, [nib]);
      else list.push(nib);
    }
    for (const [key, list] of memberNibs) sectionFor(key).members = buildTree(list);
  }

  // Order: declared roots as the forest states them (a column sort never
  // reorders these), then headed sections (by the column sort if any, else as
  // reached), then headless sections, then the leftover.
  const headed: Section<T>[] = [];
  const headless: Section<T>[] = [];
  let leftover: Section<T> | null = null;
  for (const section of sections.values()) {
    // Declared sections are emitted through `declaredRoots` or their parent's
    // `assembleSection`.
    if (section.key === lens.leftover.key) leftover = section;
    else if (SECTION_RULES[section.persistence].placedByDeclaration) continue;
    else if (section.header !== null) headed.push(section);
    else headless.push(section);
  }
  if (sortComparator) {
    // `Array.sort` is stable, so equal-key sections keep the order above.
    headed.sort((x, y) => sortComparator(x.header!.nib, y.header!.nib));
  }

  const ordered = [...declaredRoots, ...headed, ...headless, ...(leftover !== null ? [leftover] : [])];
  const roots = ordered.map((section) => assembleSection(section, lens, sortComparator));

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
    sortComparator ?? lens.orderWithinSection(section.key) ?? null;
  const members = order ? [...section.members].sort((x, y) => order(x.nib, y.nib)) : section.members;

  // Declared sub-sections carry `parentId: null`, so under a header they would
  // make `holdsChildrenByDisplay` true and re-root the header's genuine children.
  if (section.header !== null && section.declaredChildren.length > 0) {
    throw new Error(
      `declared section ${JSON.stringify(section.key)} is headed by nib ` +
        `${JSON.stringify(section.header.nib.id)} and also declares children — a headed ` +
        `section's rows are the header's own, so sub-sections there would re-root them`,
    );
  }

  // `flatten` applies the meaning's `memberRegion` to this node's direct
  // children, which under `nestHeadersStructurally` include the header's
  // structural children. A structurally nesting lens must therefore not declare
  // one; tree.test.ts asserts no shipped lens does.

  // A headed section's label is the header's title, since the table draws that
  // row as the nib.
  const display: SectionDisplay = {
    label:
      section.header?.nib.title ??
      section.declared?.label ??
      (section.key === lens.leftover.key ? lens.leftover.label : section.key),
    description: section.declared?.description ?? "",
    color: section.declared?.color ?? "",
  };

  const meta: SectionMeta = {
    key: section.key,
    persistence: section.persistence,
    meaning: lens.meaning(section.key),
    display,
  };

  if (section.header !== null) {
    // Placed members join whatever subtree the header already carries.
    return {
      ...section.header,
      children: [...section.header.children, ...members],
      section: meta,
    };
  }
  // Sub-sections lead their section's rows.
  const nested = section.declaredChildren.map((child) => assembleSection(child, lens, sortComparator));
  // The title is the label alone; the count travels as `RowSection.count`.
  const node = makeSectionNode(sectionRowId(section.key, lens), display.label, [
    ...nested,
    ...members,
  ]);
  return { ...node, section: meta };
}
