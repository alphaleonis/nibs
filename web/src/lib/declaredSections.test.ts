import { describe, expect, it } from "vitest";
import type { DeclaredSection, GroupingLens, Placement, ViewShape } from "./tree";
import {
  buildShapedViewTree,
  holdsChildrenByDisplay,
  isSyntheticRowId,
} from "./tree";
import { buildShapedTableData } from "./tableData";
import { useKeyboardNav } from "./composables/useKeyboardNav.svelte";
import { SelectionState } from "./selection.svelte";
import { DEFAULT_OPEN_DETAIL_ON } from "./types";
import { GOVERNS_NOTHING } from "./ordering/sectionMeaning";
import type { NibFilter, TableSort, TreeTableNib } from "./types";

/**
 * One fixture, driven through every surface that answers "what contains this
 * row" — the emitted view tree, `holdsChildrenByDisplay`, `parentIds`,
 * `displayParentId`, `viewMemberIds`, the containment index, reveal and
 * ArrowLeft — and required to agree. Deliberately not an allowlist of call
 * sites: a surface added later that gets this wrong shows up as a disagreement
 * here, and a rename dodges nothing.
 *
 * The lens is local because no shipped view level declares sections yet: all
 * three answer `{kind:"none"}`, so the declared-forest half of the builder has
 * no production caller to exercise it.
 */

function nib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    id: "x1",
    title: "Untitled",
    status: "todo",
    type: "task",
    priority: "normal",
    estimate: "m",
    tags: [],
    createdAt: "2026-03-15T10:00:00Z",
    updatedAt: "2026-03-20T10:00:00Z",
    parentId: null,
    milestone: "",
    milestoneOrder: "",
    area: "",
    blockingIds: [],
    blockedByIds: [],
    etag: "etag-test",
    ...overrides,
  };
}

const NO_AREA = "/__no_area__";

/** Two roots, one of them nested, one of them barren. Description and color are
 *  set on `web` and left empty on its sub-section, so the row assertions can
 *  tell "carried through" from "defaulted". */
const FOREST: readonly DeclaredSection[] = [
  {
    key: "web",
    label: "Web",
    description: "Everything served over HTTP",
    color: "#3366ff",
    children: [{ key: "web/api", label: "web/api", description: "", color: "", children: [] }],
  },
  { key: "docs", label: "docs", description: "Prose, not code", color: "slateblue", children: [] },
];

/** Groups by the stored `area:` verbatim — so an undeclared value reaches the
 *  builder as a section key nothing declared, which is the union rule's case. */
const areaLens: GroupingLens = {
  leftover: { key: NO_AREA, label: "No area" },
  declares: { kind: "forest", roots: FOREST },
  nestHeadersStructurally: false,
  meaning: () => GOVERNS_NOTHING,
  orderWithinSection: () => null,
  place: (item): Placement => ({
    kind: "member",
    section: item.area === "" ? NO_AREA : item.area,
  }),
};
const areaShape: ViewShape = { kind: "grouped", lens: areaLens };

const WEB = "/section:web_";
const API = "/section:web/api_";
const DOCS = "/section:docs_";
const LEGACY = "/section:legacy_";

const FIXTURE: TreeTableNib[] = [
  nib({ id: "w1", title: "Bravo", area: "web", priority: "high" }),
  nib({ id: "w2", title: "Alpha", area: "web", priority: "high" }),
  nib({ id: "a1", title: "Api one", area: "web/api", priority: "low" }),
  nib({ id: "r1", title: "Retired", area: "legacy", priority: "low" }),
  nib({ id: "u1", title: "Unfiled", area: "", priority: "low" }),
];

const noFilter: NibFilter = {};
const noCollapsed = new Set<string>();

/** The filter that keeps only the two `web` members — emptying `web/api` (which
 *  is declared) and both `legacy` and the leftover (which are not). */
const HIGH_ONLY: NibFilter = { priority: ["high"] };

function tableOf(filter: NibFilter, sort: TableSort | null = null) {
  const data = buildShapedTableData(FIXTURE, filter, areaShape, noCollapsed, sort);
  return {
    ...data,
    ids: data.rows.map((r) => r.nib.id),
    row: (id: string) => data.rows.find((r) => r.nib.id === id),
  };
}

describe("a lens that declares its sections", () => {
  it("seeds the forest ahead of every discovered section, nested and in declaration order", () => {
    const tree = buildShapedViewTree(FIXTURE, areaShape);

    // Declared roots lead, in the order the forest stated; the section a nib's
    // undeclared `area:` minted follows, and the leftover is last.
    expect(tree.map((n) => n.nib.id)).toEqual([WEB, DOCS, LEGACY, NO_AREA]);
    // A sub-section leads its parent's rows, the same way declared roots lead
    // the top level.
    expect(tree[0].children.map((n) => n.nib.id)).toEqual([API, "w1", "w2"]);
    expect(tree[0].children[0].children.map((n) => n.nib.id)).toEqual(["a1"]);
    expect(tree[1].children).toEqual([]);
    // setDepths reaches the extra level.
    expect([tree[0].depth, tree[0].children[0].depth, tree[0].children[0].children[0].depth]).toEqual([0, 1, 2]);
  });

  it("marks a declared section declared and a discovered one discovered", () => {
    const tree = buildShapedViewTree(FIXTURE, areaShape);
    const persistenceOf = (id: string) =>
      tree.flatMap((n) => [n, ...n.children]).find((n) => n.nib.id === id)?.section?.persistence;

    expect(persistenceOf(WEB)).toBe("declared");
    expect(persistenceOf(API)).toBe("declared");
    expect(persistenceOf(DOCS)).toBe("declared");
    expect(persistenceOf(LEGACY)).toBe("discovered");
    expect(persistenceOf(NO_AREA)).toBe("discovered");
  });

  it("renders a declared section with no members, with a caret only when it declares children", () => {
    const tree = buildShapedViewTree(FIXTURE, areaShape);
    const docs = tree.find((n) => n.nib.id === DOCS)!;
    const web = tree.find((n) => n.nib.id === WEB)!;
    const { ids, parentIds, viewMemberIds, row } = tableOf(noFilter);

    // Every surface that answers "does this row contain anything" agrees.
    expect(ids).toContain(DOCS);
    expect(holdsChildrenByDisplay(docs)).toBe(false);
    expect(parentIds.has(DOCS)).toBe(false);
    expect(row(DOCS)!.hasChildren).toBe(false);

    expect(holdsChildrenByDisplay(web)).toBe(true);
    expect(parentIds.has(WEB)).toBe(true);
    expect(row(WEB)!.hasChildren).toBe(true);

    // The lens has a row for it, so the reveal/selection surfaces can address it.
    expect(viewMemberIds.has(DOCS)).toBe(true);
    expect(isSyntheticRowId(DOCS)).toBe(true);
  });

  it("keeps a declared section whose only member a filter excludes", () => {
    // Not vacuous: unfiltered, the section has exactly the one member.
    expect(tableOf(noFilter).row(API)!.hasChildren).toBe(true);

    const { ids, row } = tableOf(HIGH_ONLY);

    expect(ids).not.toContain("a1");
    expect(ids).toContain(API);
    expect(row(API)!.hasChildren).toBe(false);
    // Its declared parent stands too, and still holds it.
    expect(ids).toContain(WEB);
    expect(row(API)!.depth).toBe(1);
  });

  /**
   * Today's Backlog behavior, and the mutation that proves this file's guards
   * bite: flipping `rendersWhenEmpty` to true for `discovered` in
   * `SECTION_RULES` fails exactly this case.
   */
  it("still prunes a DISCOVERED section a filter emptied", () => {
    // Not vacuous: unfiltered, both discovered sections render with their member.
    const unfiltered = tableOf(noFilter).ids;
    expect(unfiltered).toEqual(expect.arrayContaining([LEGACY, "r1", NO_AREA, "u1"]));

    const { ids } = tableOf(HIGH_ONLY);

    expect(ids).not.toContain(LEGACY);
    expect(ids).not.toContain(NO_AREA);
    expect(ids).not.toContain("r1");
    expect(ids).not.toContain("u1");
    // The declared ones are the only sections left, so the difference is
    // persistence and not the filter having emptied nothing.
    expect(ids).toEqual([WEB, API, "w1", "w2", DOCS]);
  });

  it("mints a section of its own for an undeclared key, distinct from the leftover", () => {
    const { ids, row } = tableOf(noFilter);

    expect(row(LEGACY)).toBeDefined();
    expect(row(LEGACY)!.nib.title).toContain("legacy");
    expect(LEGACY).not.toBe(NO_AREA);
    // The two catch-alls are separate rows, and each holds its own nib.
    expect(ids.indexOf("r1")).toBe(ids.indexOf(LEGACY) + 1);
    expect(ids.indexOf("u1")).toBe(ids.indexOf(NO_AREA) + 1);
    // And the index agrees with where the builder actually put them.
    const { containment } = tableOf(noFilter);
    expect(containment.containerOf("r1")).toBe(LEGACY);
    expect(containment.containerOf("u1")).toBe(NO_AREA);
    expect(containment.containerOf("a1")).toBe(API);
  });

  it("keeps declared order under an active column sort, which orders members within a section", () => {
    const sorted = tableOf(noFilter, { field: "title", direction: "asc" });

    // Members reordered: "Alpha" (w2) now precedes "Bravo" (w1).
    expect(sorted.ids).toEqual([WEB, API, "a1", "w2", "w1", DOCS, LEGACY, "r1", NO_AREA, "u1"]);
    // The sub-section still leads its parent's rows, and the roots are still in
    // declaration order — a column sort orders a section's members, not the
    // declaration.
    expect(sorted.ids.indexOf(WEB)).toBeLessThan(sorted.ids.indexOf(DOCS));
  });

  it("gives a row two synthetic containers deep the containers' own display parent", () => {
    const { row } = tableOf(noFilter);

    // The whole reason `displayParentId` needs no per-level resolution: the
    // value threaded down is already the container's own resolved display
    // parent, so nesting containers changes nothing.
    expect(row(WEB)!.displayParentId).toBeNull();
    expect(row(API)!.displayParentId).toBeNull();
    expect(row("a1")!.displayParentId).toBeNull();
    expect(row("w1")!.displayParentId).toBeNull();
    // And the invariant it exists for: never a synthetic id.
    for (const r of tableOf(noFilter).rows) {
      expect(r.displayParentId === null || !isSyntheticRowId(r.displayParentId)).toBe(true);
    }
  });

  it("refuses a forest node keyed on the lens's own leftover key", () => {
    const collidingLens: GroupingLens = {
      ...areaLens,
      declares: {
        kind: "forest",
        roots: [{ key: NO_AREA, label: "No area", description: "", color: "", children: [] }],
      },
    };

    // `SectionKey` is `string`, so the types cannot close this; a silent accept
    // would assemble one section twice and put one row id in the table twice.
    expect(() => buildShapedViewTree(FIXTURE, { kind: "grouped", lens: collidingLens })).toThrow(
      /collides with the lens's leftover key/,
    );
  });

  it("refuses two forest siblings under one key", () => {
    const duplicateLens: GroupingLens = {
      ...areaLens,
      declares: {
        kind: "forest",
        roots: [
          { key: "web", label: "Web one", description: "", color: "", children: [] },
          { key: "web", label: "Web two", description: "", color: "", children: [] },
        ],
      },
    };

    // `sectionFor` is memoized, so both nodes resolve to one section: accepted,
    // it renders that section twice — one row id, and w1 with it, twice over.
    expect(() => buildShapedViewTree(FIXTURE, { kind: "grouped", lens: duplicateLens })).toThrow(
      /declared section "web" appears twice in the forest/,
    );
  });

  it("refuses a forest node keyed on one of its own ancestors", () => {
    const selfLens: GroupingLens = {
      ...areaLens,
      declares: {
        kind: "forest",
        roots: [
          {
            key: "web",
            label: "Web",
            description: "",
            color: "",
            children: [{ key: "web", label: "Web again", description: "", color: "", children: [] }],
          },
        ],
      },
    };

    // The same memo puts the section inside its own `declaredChildren`, which
    // `assembleSection` then recurses through until the stack runs out — so the
    // untended failure is a RangeError, not a wrong render.
    expect(() => buildShapedViewTree(FIXTURE, { kind: "grouped", lens: selfLens })).toThrow(
      /declared section "web" appears twice in the forest/,
    );
  });
});

/**
 * A nib heading a section and that section declaring children are each fine
 * alone; together they make `holdsChildrenByDisplay` true for the header node,
 * and `flatten` then re-roots the header's genuine structural children onto the
 * display parent. No shipped lens can build it, so the builder refuses it
 * rather than carrying a per-edge containment rule.
 */
describe("a declared section a nib heads", () => {
  const headed = (declaredChildren: DeclaredSection[]): ViewShape => ({
    kind: "grouped",
    lens: {
      leftover: { key: NO_AREA, label: "No area" },
      declares: {
        kind: "forest",
        roots: [{ key: "H", label: "Headed", description: "", color: "", children: declaredChildren }],
      },
      nestHeadersStructurally: true,
      meaning: () => GOVERNS_NOTHING,
      orderWithinSection: () => null,
      place: (item): Placement =>
        item.id === "H"
          ? { kind: "header", section: "H" }
          : { kind: "member", section: item.id === "s1" ? "sub" : NO_AREA },
    },
  });

  const HEADED_FIXTURE: TreeTableNib[] = [
    nib({ id: "H", title: "Header", type: "milestone" }),
    nib({ id: "c1", title: "Genuine child", parentId: "H" }),
    nib({ id: "s1", title: "Sub member" }),
  ];

  it("refuses to also hold declared sub-sections", () => {
    const sub: DeclaredSection = { key: "sub", label: "Sub", description: "", color: "", children: [] };
    expect(() => buildShapedViewTree(HEADED_FIXTURE, headed([sub]))).toThrow(
      /declared section "H" is headed by nib "H" and also declares children/,
    );
  });

  it("still nests the header's own structural children, which is what the refusal protects", () => {
    // Not vacuous: the same lens minus the declared children renders, and c1
    // keeps naming H as its display parent — the value the refused shape lost.
    const tree = buildShapedViewTree(HEADED_FIXTURE, headed([]));
    const h = tree.find((n) => n.nib.id === "H")!;

    expect(h.children.map((n) => n.nib.id)).toEqual(["c1"]);
    expect(holdsChildrenByDisplay(h)).toBe(false);

    const rows = buildShapedTableData(HEADED_FIXTURE, noFilter, headed([]), noCollapsed, null).rows;
    expect(rows.find((r) => r.nib.id === "c1")!.displayParentId).toBe("H");
  });

  it("is hidden by a client filter that excludes it, like any other nib", () => {
    // A DECLARED section persists through a filter that empties it, but only
    // when the view fabricated its row. A nib heading one is still a nib: the
    // declaration says its section exists, not that the nib is exempt from the
    // filter. Guards a real leak — `persists` reached this row before it asked
    // `isSyntheticRowId`, and the force-add to `visibleIds` made H unhideable.
    const rows = buildShapedTableData(
      HEADED_FIXTURE,
      { priority: ["high"] },
      headed([]),
      noCollapsed,
      null,
    ).rows;

    expect(rows.map((r) => r.nib.id)).not.toContain("H");
  });
});

/**
 * The consumers of containment, on the shape only a declared forest makes: a row
 * inside a section inside a section.
 *
 * No shipped view level declares one, so these drive the two consumers directly
 * rather than through `TreeTable` — which can only be rendered at a shipped view
 * level. The single-section path each of them takes in a shipped view is driven
 * through the component in TreeTable.test.ts.
 */
describe("what contains a row when sections nest", () => {
  const BOTH_SHUT = new Set([WEB, API]);

  function idsUnder(collapsed: ReadonlySet<string>): string[] {
    return buildShapedTableData(FIXTURE, noFilter, areaShape, collapsed, null).rows.map((r) => r.nib.id);
  }

  it("keeps the chain of a row inside a collapsed section, which has no row at all", () => {
    const { rows, containment } = buildShapedTableData(FIXTURE, noFilter, areaShape, BOTH_SHUT, null);

    expect(rows.map((r) => r.nib.id)).not.toContain("a1");
    // Nothing about the index moved: it is read off the tree, and the collapse
    // set reaches it only as which ids got a row.
    expect(containment.chainOf("a1")).toEqual([API, WEB]);
    expect(containment.contains(WEB, "a1")).toBe(true);
  });

  it("needs the WHOLE chain opened to reveal a row, not the innermost container", () => {
    const { containment } = buildShapedTableData(FIXTURE, noFilter, areaShape, BOTH_SHUT, null);
    const chain = containment.chainOf("a1");
    expect(chain.length).toBeGreaterThan(1);

    // Opening only the section the row is directly in leaves it hidden — what a
    // single container id could do, and the whole of the reveal defect.
    const innermostOnly = new Set(BOTH_SHUT);
    innermostOnly.delete(chain[0]);
    expect(idsUnder(innermostOnly)).not.toContain("a1");

    // Opening every container the chain names is what reveal does, and it is
    // enough: the row is drawn.
    const wholeChain = new Set(BOTH_SHUT);
    for (const id of chain) wholeChain.delete(id);
    expect(idsUnder(wholeChain)).toContain("a1");
  });

  describe("ArrowLeft", () => {
    function focusAfterArrowLeft(from: string, collapsed: ReadonlySet<string> = noCollapsed): string | null {
      const { rows, containment } = buildShapedTableData(FIXTURE, noFilter, areaShape, collapsed, null);
      const selection = new SelectionState();
      selection.focus(from);
      const { handleKeydown } = useKeyboardNav({
        selection,
        getRows: () => rows,
        getVisibleRowIds: () => rows.map((r) => r.nib.id),
        getCollapsedIds: () => collapsed,
        getContainment: () => containment,
        toggleNode: () => {},
        getScrollContainer: () => null,
        onDragKeyDown: () => {},
        navigateToNib: () => {},
        getOpenDetailOn: () => DEFAULT_OPEN_DETAIL_ON,
      });
      handleKeydown(new KeyboardEvent("keydown", { key: "ArrowLeft", bubbles: true, cancelable: true }));
      return selection.focusedNibId;
    }

    it("steps a member out to the section drawn around it", () => {
      // Its real parent is null and its display parent is null; the only thing
      // that answers here is what DRAWS it.
      const { rows } = buildShapedTableData(FIXTURE, noFilter, areaShape, noCollapsed, null);
      expect(rows.find((r) => r.nib.id === "a1")!.displayParentId).toBeNull();

      expect(focusAfterArrowLeft("a1")).toBe(API);
    });

    it("steps a section out to the section that declares it", () => {
      // Collapsed, or ArrowLeft closes the section instead of leaving it.
      expect(focusAfterArrowLeft(API, new Set([API]))).toBe(WEB);
    });

    it("stays put at the display root", () => {
      expect(focusAfterArrowLeft(WEB, new Set([WEB]))).toBe(WEB);
    });
  });
});

/**
 * What a section row SHOWS: its label, description and color, and a count.
 *
 * Both facts used to live in the row's nib title as `${label} (${n})`, where n
 * was `members.length` — the section's top-level member NODES. That number is
 * neither of the two a heading could mean: a member arrives with its own
 * subtree attached, so a chain of three nibs counts 1, and a declared
 * sub-section is not in the array at all, so a node holding only sub-sections
 * counts 0 while visibly holding rows.
 *
 * THE RULE THIS FILE PINS. The count is over the nib rows the section draws:
 * its members, everything nested under them, and every declared sub-section's
 * rows, rolled up; the section rows themselves are not counted, because they
 * name no nib. It tracks the client filter — a row the filter hides is not
 * drawn and is not counted — and it does NOT track collapse, which hides rows
 * precisely when the summary is the only thing left.
 *
 * Filter-tracking is the half that had a defensible alternative: a count of the
 * section's MEMBERSHIP, unmoved by a filter, telling a reader what the area
 * holds behind the one they typed. It loses because a declared section renders
 * when a filter empties it (`SECTION_RULES.declared.rendersWhenEmpty`), so that
 * count would sit above zero drawn rows asserting rows nobody can see — the
 * same disagreement between a heading and the rows under it that the old
 * concatenated count was, restated. Expanding a heading and counting is how a
 * reader checks the number, and it is the only check available to them.
 */
describe("what a section row shows", () => {
  /** `top` declares a sub-section and has no members of its own — the shape
   *  that made a heading read `(0)` above a visibly occupied section. */
  const ROLLUP_FOREST: readonly DeclaredSection[] = [
    {
      key: "top",
      label: "Top",
      description: "Holds no work of its own",
      color: "teal",
      children: [{ key: "top/inner", label: "Inner", description: "", color: "", children: [] }],
    },
  ];
  const rollupShape: ViewShape = {
    kind: "grouped",
    lens: { ...areaLens, declares: { kind: "forest", roots: ROLLUP_FOREST } },
  };

  const TOP = "/section:top_";
  const INNER = "/section:top/inner_";

  /** `c1` is `p1`'s child, and both are in `top/inner` — so the section's
   *  `members` array holds ONE root and the section holds two nibs. */
  const ROLLUP_FIXTURE: TreeTableNib[] = [
    nib({ id: "p1", title: "Parent", area: "top/inner", priority: "high" }),
    nib({ id: "c1", title: "Child", area: "top/inner", parentId: "p1", priority: "low" }),
    nib({ id: "o1", title: "Outside", area: "", priority: "low" }),
  ];

  function rollupRows(filter: NibFilter, collapsed: ReadonlySet<string> = noCollapsed) {
    const data = buildShapedTableData(ROLLUP_FIXTURE, filter, rollupShape, collapsed, null);
    return {
      rows: data.rows,
      ids: data.rows.map((r) => r.nib.id),
      row: (id: string) => data.rows.find((r) => r.nib.id === id)!,
    };
  }

  it("counts a declared node that holds only sub-sections by what they hold", () => {
    const { ids, row } = rollupRows(noFilter);

    // Not vacuous: `top` really does hold no member of its own, which is what
    // made the old count read 0 for it.
    expect(buildShapedViewTree(ROLLUP_FIXTURE, rollupShape)[0].children.map((n) => n.nib.id)).toEqual([INNER]);
    expect(ids).toEqual([TOP, INNER, "p1", "c1", NO_AREA, "o1"]);
    expect(row(TOP).drawsSection!.count).toBe(2);
  });

  it("counts a member's descendants, which arrive as one node", () => {
    const tree = buildShapedViewTree(ROLLUP_FIXTURE, rollupShape);
    const inner = tree[0].children[0];
    // The trap, stated as the builder sees it: `buildTree` nests c1 under p1
    // when it rebuilds the section, so the array a count could be taken over
    // has one entry for the two nibs in the section.
    expect(inner.children.map((n) => n.nib.id)).toEqual(["p1"]);

    expect(rollupRows(noFilter).row(INNER).drawsSection!.count).toBe(2);
  });

  it("leaves the section rows themselves out of the count", () => {
    const { row } = rollupRows(noFilter);
    // `top` draws three rows: the `top/inner` heading and the two nibs. Two of
    // them name a nib, and the count is over those.
    expect(row(TOP).drawsSection!.count).toBe(2);
    expect(row(INNER).drawsSection!.count).toBe(2);
  });

  it("drops a filtered-out row from the count, at every level it rolls through", () => {
    // Not vacuous: unfiltered both read 2, and the filter takes exactly c1.
    expect(rollupRows(noFilter).row(TOP).drawsSection!.count).toBe(2);

    const { ids, row } = rollupRows(HIGH_ONLY);

    expect(ids).not.toContain("c1");
    expect(row(INNER).drawsSection!.count).toBe(1);
    expect(row(TOP).drawsSection!.count).toBe(1);
  });

  it("reads 0 for a declared section a filter emptied, which still renders", () => {
    const { ids, row } = rollupRows({ priority: ["critical"] });

    // The case where "what is in the section" and "what is drawn" come apart:
    // the section stands on its declaration, and the count answers for the rows.
    expect(ids).toEqual([TOP, INNER]);
    expect(row(TOP).drawsSection!.count).toBe(0);
    expect(row(INNER).drawsSection!.count).toBe(0);
  });

  it("keeps the count of a collapsed section, whose rows are the ones it summarizes", () => {
    const { ids, row } = rollupRows(noFilter, new Set([TOP]));

    expect(ids).toEqual([TOP, NO_AREA, "o1"]);
    expect(row(TOP).drawsSection!.count).toBe(2);
  });

  it("carries the declared description and color to the row, and empties them where nothing declared any", () => {
    const { row } = rollupRows(noFilter);

    expect(row(TOP).drawsSection!.display).toEqual({
      label: "Top",
      description: "Holds no work of its own",
      color: "teal",
    });
    // A declared section that set neither, and the leftover, which has no
    // declaration to set them from.
    expect(row(INNER).drawsSection!.display).toEqual({ label: "Inner", description: "", color: "" });
    expect(row(NO_AREA).drawsSection!.display).toEqual({ label: "No area", description: "", color: "" });
  });

  it("labels a discovered section with its key, having nothing else to name it", () => {
    const { row } = tableOf(noFilter);

    expect(row(LEGACY)!.drawsSection!.display).toEqual({ label: "legacy", description: "", color: "" });
    expect(row(WEB)!.drawsSection!.display.label).toBe("Web");
  });

  it("puts nothing but the label in a section row's title", () => {
    // The property the count violated by living there: a row title is read as
    // prose by whatever holds a row and needs to name it, and none of those
    // callers can subtract a suffix the view added. Asserted over every section
    // in the table rather than at the callers, so a new one is covered without
    // being enrolled anywhere.
    const rows = [...tableOf(noFilter).rows, ...rollupRows(noFilter).rows];
    const sections = rows.filter((r) => r.drawsSection !== null);
    expect(sections.length).toBeGreaterThan(4);

    for (const r of sections) {
      expect(r.nib.title).toBe(r.drawsSection!.display.label);
    }
  });

  it("gives a member row the enclosing section's display and count, transitively", () => {
    const { row } = rollupRows(noFilter);

    // c1 is nested under a member, so the section around it is its ancestor's.
    expect(row("c1").drawsSection).toBeNull();
    expect(row("c1").section).toEqual(row("p1").section);
    expect(row("c1").section!.key).toBe("top/inner");
    expect(row("c1").section!.count).toBe(2);
    expect(row("c1").section!.display.label).toBe("Inner");
  });
});
