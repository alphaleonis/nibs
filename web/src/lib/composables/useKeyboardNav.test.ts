import { describe, it, expect, vi } from "vitest";
import { SelectionState } from "../selection.svelte";
import { buildContainmentIndex, type ContainmentIndex } from "../containment";
import { rowRegion, type RowData } from "../tableData";
import { EMPTY_SPINE } from "../viewSpine";

const { buildTableData } = EMPTY_SPINE;
import type { TreeTableNib, OpenDetailGesture } from "../types";
import { DEFAULT_OPEN_DETAIL_ON } from "../types";
import { useKeyboardNav } from "./useKeyboardNav.svelte";

function makeNib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    id: "nibs-001",
    title: "Task 1",
    status: "todo",
    type: "task",
    priority: "normal",
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
    etag: "etag-test",
    ...overrides,
  };
}

function makeRow(nib: TreeTableNib, opts: Partial<RowData> = {}): RowData {
  return {
    nib,
    depth: 0,
    hasChildren: false,
    dimmed: false,
    parentNib: null,
    displayParentId: null,
    // Production's own rule, called rather than restated — a fabricated bucket
    // built through this helper must come out a member of nothing, as it does
    // in a real table.
    region: rowRegion(nib.id, nib.parentId),
    drawsSection: null,
    section: null,
    ...opts,
  };
}

const EMPTY_CONTAINMENT = buildContainmentIndex([]);

describe("useKeyboardNav", () => {
  function setup(overrides: {
    selection?: SelectionState;
    rows?: RowData[];
    collapsedIds?: Set<string>;
    toggleNode?: (id: string) => void;
    onDragKeyDown?: (e: KeyboardEvent) => void;
    navigateToNib?: (id: string) => void;
    openDetailOn?: OpenDetailGesture;
    /** Defaults to the index of an EMPTY view, which contains nothing — so a
     *  case that says nothing about containment cannot walk out of a row by
     *  accident. The ArrowLeft cases supply a real table's. */
    containment?: ContainmentIndex;
  } = {}) {
    const selection = overrides.selection ?? new SelectionState();
    const rows = overrides.rows ?? [];
    const visibleRowIds = rows.map(r => r.nib.id);
    const collapsedIds = overrides.collapsedIds ?? new Set<string>();
    const toggleNode = overrides.toggleNode ?? vi.fn();
    const onDragKeyDown = overrides.onDragKeyDown ?? vi.fn();
    const navigateToNib = overrides.navigateToNib ?? vi.fn((id: string) => selection.select(id));
    const scrollContainer = document.createElement("div");

    const result = useKeyboardNav({
      selection,
      getRows: () => rows,
      getVisibleRowIds: () => visibleRowIds,
      getCollapsedIds: () => collapsedIds,
      getContainment: () => overrides.containment ?? EMPTY_CONTAINMENT,
      toggleNode,
      getScrollContainer: () => scrollContainer,
      onDragKeyDown,
      navigateToNib,
      getOpenDetailOn: () => overrides.openDetailOn ?? DEFAULT_OPEN_DETAIL_ON,
    });

    return { ...result, selection, toggleNode, onDragKeyDown, navigateToNib, scrollContainer };
  }

  function keydown(key: string, opts: KeyboardEventInit = {}): KeyboardEvent {
    return new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true, ...opts });
  }

  it("ArrowDown with no focus focuses first row", () => {
    const nib1 = makeNib({ id: "nibs-001" });
    const nib2 = makeNib({ id: "nibs-002" });
    const rows = [makeRow(nib1), makeRow(nib2)];
    const { handleKeydown, selection } = setup({ rows });

    handleKeydown(keydown("ArrowDown"));

    expect(selection.focusedNibId).toBe("nibs-001");
  });

  it("ArrowDown moves focus to next row", () => {
    const selection = new SelectionState();
    selection.focus("nibs-001");
    const nib1 = makeNib({ id: "nibs-001" });
    const nib2 = makeNib({ id: "nibs-002" });
    const rows = [makeRow(nib1), makeRow(nib2)];
    const { handleKeydown } = setup({ selection, rows });

    handleKeydown(keydown("ArrowDown"));

    expect(selection.focusedNibId).toBe("nibs-002");
  });

  it("ArrowUp moves focus to previous row", () => {
    const selection = new SelectionState();
    selection.focus("nibs-002");
    const nib1 = makeNib({ id: "nibs-001" });
    const nib2 = makeNib({ id: "nibs-002" });
    const rows = [makeRow(nib1), makeRow(nib2)];
    const { handleKeydown } = setup({ selection, rows });

    handleKeydown(keydown("ArrowUp"));

    expect(selection.focusedNibId).toBe("nibs-001");
  });

  it("ArrowDown at last row stays on last row", () => {
    const selection = new SelectionState();
    selection.focus("nibs-002");
    const nib1 = makeNib({ id: "nibs-001" });
    const nib2 = makeNib({ id: "nibs-002" });
    const rows = [makeRow(nib1), makeRow(nib2)];
    const { handleKeydown } = setup({ selection, rows });

    handleKeydown(keydown("ArrowDown"));

    expect(selection.focusedNibId).toBe("nibs-002");
  });

  it("ArrowUp at first row stays on first row", () => {
    const selection = new SelectionState();
    selection.focus("nibs-001");
    const nib1 = makeNib({ id: "nibs-001" });
    const nib2 = makeNib({ id: "nibs-002" });
    const rows = [makeRow(nib1), makeRow(nib2)];
    const { handleKeydown } = setup({ selection, rows });

    handleKeydown(keydown("ArrowUp"));

    expect(selection.focusedNibId).toBe("nibs-001");
  });

  it("Shift+ArrowDown creates range selection", () => {
    const selection = new SelectionState();
    selection.select("nibs-001"); // sets anchor
    const nib1 = makeNib({ id: "nibs-001" });
    const nib2 = makeNib({ id: "nibs-002" });
    const nib3 = makeNib({ id: "nibs-003" });
    const rows = [makeRow(nib1), makeRow(nib2), makeRow(nib3)];
    const { handleKeydown } = setup({ selection, rows });

    handleKeydown(keydown("ArrowDown", { shiftKey: true }));

    // Focus should have moved to nibs-002
    expect(selection.focusedNibId).toBe("nibs-002");
    // Range selection from anchor (nibs-001) to nibs-002
    expect(selection.selectedIds.has("nibs-001")).toBe(true);
    expect(selection.selectedIds.has("nibs-002")).toBe(true);
  });

  it("Shift+ArrowUp creates range selection upward", () => {
    const selection = new SelectionState();
    selection.select("nibs-003"); // sets anchor
    const nib1 = makeNib({ id: "nibs-001" });
    const nib2 = makeNib({ id: "nibs-002" });
    const nib3 = makeNib({ id: "nibs-003" });
    const rows = [makeRow(nib1), makeRow(nib2), makeRow(nib3)];
    const { handleKeydown } = setup({ selection, rows });

    handleKeydown(keydown("ArrowUp", { shiftKey: true }));

    expect(selection.focusedNibId).toBe("nibs-002");
    expect(selection.selectedIds.has("nibs-002")).toBe(true);
    expect(selection.selectedIds.has("nibs-003")).toBe(true);
  });

  it("Shift+ArrowDown across a synthetic bucket row keeps its id out of the selection", () => {
    const selection = new SelectionState();
    selection.select("nibs-e1"); // anchor
    selection.focus("nibs-e1");
    const header = makeNib({ id: "nibs-m1", type: "milestone" });
    const epic = makeNib({ id: "nibs-e1", type: "epic", parentId: "nibs-m1" });
    // Synthetic bucket row, interleaved between nib rows (as buildViewTree emits it).
    const bucket = makeNib({ id: "/__no_milestone__", type: "" });
    const loose = makeNib({ id: "nibs-loose" });
    const rows = [makeRow(header), makeRow(epic), makeRow(bucket, { hasChildren: true }), makeRow(loose)];
    const { handleKeydown } = setup({ selection, rows });

    // First Shift+ArrowDown lands focus on the bucket row...
    handleKeydown(keydown("ArrowDown", { shiftKey: true }));
    // ...second Shift+ArrowDown lands on the loose task, spanning the bucket.
    handleKeydown(keydown("ArrowDown", { shiftKey: true }));

    expect(selection.focusedNibId).toBe("nibs-loose");
    expect(selection.selectedIds.has("nibs-e1")).toBe(true);
    expect(selection.selectedIds.has("nibs-loose")).toBe(true);
    expect(selection.selectedIds.has("/__no_milestone__")).toBe(false);
  });

  it("double mode: Shift+ArrowDown collapsing to one row leaves the detail panel alone", () => {
    const selection = new SelectionState();
    selection.select("nibs-001"); // panel opens on nibs-001
    selection.deselectAll();
    selection.anchorId = "nibs-002";
    selection.focus("nibs-001");
    const rows = [makeRow(makeNib({ id: "nibs-001" })), makeRow(makeNib({ id: "nibs-002" }))];
    const { handleKeydown } = setup({ selection, rows, openDetailOn: "double" });

    handleKeydown(keydown("ArrowDown", { shiftKey: true }));

    expect(selection.selectedIds.has("nibs-002")).toBe(true);
    expect(selection.selectedNibId).toBe("nibs-001");
  });

  it("double mode: Space toggling a row leaves the detail panel alone", () => {
    const selection = new SelectionState();
    selection.select("nibs-001");
    selection.deselectAll();
    selection.focus("nibs-002");
    const rows = [makeRow(makeNib({ id: "nibs-001" })), makeRow(makeNib({ id: "nibs-002" }))];
    const { handleKeydown } = setup({ selection, rows, openDetailOn: "double" });

    handleKeydown(keydown(" "));

    expect(selection.selectedIds.has("nibs-002")).toBe(true);
    expect(selection.selectedNibId).toBe("nibs-001");
  });

  // The shift+arrow twins of the Space case below. A range that collapses to
  // exactly one row is an ordinary gesture (shift+ArrowDown then shift+ArrowUp
  // returns to the anchor), and in single mode the panel must follow it — the
  // pre-existing behavior the panel policy must not regress.
  it("single mode: Shift+ArrowDown collapsing to one row still opens the panel", () => {
    const selection = new SelectionState();
    selection.anchorId = "nibs-002";
    selection.focus("nibs-001");
    const rows = [makeRow(makeNib({ id: "nibs-001" })), makeRow(makeNib({ id: "nibs-002" }))];
    const { handleKeydown } = setup({ selection, rows }); // default mode = "single"

    handleKeydown(keydown("ArrowDown", { shiftKey: true }));

    expect(selection.selectedIds.has("nibs-002")).toBe(true);
    expect(selection.selectedNibId).toBe("nibs-002");
  });

  it("single mode: Shift+ArrowUp collapsing to one row still opens the panel", () => {
    const selection = new SelectionState();
    selection.anchorId = "nibs-001";
    selection.focus("nibs-002");
    const rows = [makeRow(makeNib({ id: "nibs-001" })), makeRow(makeNib({ id: "nibs-002" }))];
    const { handleKeydown } = setup({ selection, rows }); // default mode = "single"

    handleKeydown(keydown("ArrowUp", { shiftKey: true }));

    expect(selection.selectedIds.has("nibs-001")).toBe(true);
    expect(selection.selectedNibId).toBe("nibs-001");
  });

  it("single mode: Space toggling a single row still opens it in the panel", () => {
    const selection = new SelectionState();
    selection.focus("nibs-002");
    const rows = [makeRow(makeNib({ id: "nibs-001" })), makeRow(makeNib({ id: "nibs-002" }))];
    const { handleKeydown } = setup({ selection, rows });

    handleKeydown(keydown(" "));

    expect(selection.selectedNibId).toBe("nibs-002");
  });

  it("Enter on focused row selects it", () => {
    const selection = new SelectionState();
    selection.focus("nibs-002");
    const nib1 = makeNib({ id: "nibs-001" });
    const nib2 = makeNib({ id: "nibs-002" });
    const rows = [makeRow(nib1), makeRow(nib2)];
    const { handleKeydown, navigateToNib } = setup({ selection, rows });

    handleKeydown(keydown("Enter"));

    expect(selection.selectedNibId).toBe("nibs-002");
    // Enter must route through the injected navigateToNib (browser-history push /
    // URL sync), not call selection.select directly — a regression back to the latter
    // would still pass the assertion above but silently drop history.
    expect(navigateToNib).toHaveBeenCalledWith("nibs-002");
  });

  it("Space toggles focused row selection (fallback: event has no DOM row)", () => {
    const selection = new SelectionState();
    selection.focus("nibs-001");
    const nib1 = makeNib({ id: "nibs-001" });
    const rows = [makeRow(nib1)];
    const { handleKeydown } = setup({ selection, rows });

    // A bare KeyboardEvent has target=null (the arrow-key case, where DOM focus
    // is on the grid container), so the target resolves from focusedNibId.
    handleKeydown(keydown(" "));

    expect(selection.selectedIds.has("nibs-001")).toBe(true);
  });

  // Enter/Space resolve WHICH row to act on from the event's own `tr[data-nib-id]`
  // DOM ancestor (the Tab case), preferring it over a stale focusedNibId. These
  // dispatch a real event from a button inside a row so `event.target` is set.
  function dispatchFromRow(
    handleKeydown: (e: KeyboardEvent) => void,
    nibId: string,
    key: string,
  ): void {
    const host = document.createElement("div");
    host.innerHTML = `<table><tbody><tr data-nib-id="${nibId}"><td><button data-action="title">t</button></td></tr></tbody></table>`;
    document.body.appendChild(host);
    host.addEventListener("keydown", handleKeydown as EventListener);
    const btn = host.querySelector("button") as HTMLButtonElement;
    btn.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true }));
    host.remove();
  }

  it("Space resolves the target from the event's DOM row, not a stale focusedNibId", () => {
    const selection = new SelectionState();
    // Stale virtual focus on a DIFFERENT row than the one the event comes from.
    selection.focus("nibs-001");
    const rows = [makeRow(makeNib({ id: "nibs-001" })), makeRow(makeNib({ id: "nibs-002" }))];
    const { handleKeydown } = setup({ selection, rows });

    dispatchFromRow(handleKeydown, "nibs-002", " ");

    // The DOM row (002) is toggled, not the stale focusedNibId (001).
    expect(selection.selectedIds.has("nibs-002")).toBe(true);
    expect(selection.selectedIds.has("nibs-001")).toBe(false);
  });

  it("Enter on a DOM bucket row toggles its group instead of navigating", () => {
    const selection = new SelectionState();
    const toggleNode = vi.fn();
    const navigateToNib = vi.fn();
    const bucketId = "/__no_milestone__";
    const rows = [makeRow(makeNib({ id: bucketId, type: "" }), { hasChildren: true })];
    const { handleKeydown } = setup({ selection, rows, toggleNode, navigateToNib });

    dispatchFromRow(handleKeydown, bucketId, "Enter");

    expect(toggleNode).toHaveBeenCalledWith(bucketId);
    expect(navigateToNib).not.toHaveBeenCalled();
  });

  it("ArrowLeft on expanded parent collapses it", () => {
    const selection = new SelectionState();
    selection.focus("nibs-001");
    const toggleNode = vi.fn();
    const nib1 = makeNib({ id: "nibs-001" });
    const nib2 = makeNib({ id: "nibs-002", parentId: "nibs-001" });
    const rows = [
      makeRow(nib1, { hasChildren: true }),
      makeRow(nib2),
    ];
    // Not collapsed — so ArrowLeft should collapse
    const { handleKeydown } = setup({ selection, rows, toggleNode, collapsedIds: new Set() });

    handleKeydown(keydown("ArrowLeft"));

    expect(toggleNode).toHaveBeenCalledWith("nibs-001");
  });

  it("ArrowLeft on leaf/collapsed row moves focus to parent", () => {
    // The ordinary tree view: the row that draws a child IS its real parent, so
    // this is the plain case rather than a display-vs-real-parent guard.
    const selection = new SelectionState();
    selection.focus("nibs-002");
    const table = buildTableData(
      [makeNib({ id: "nibs-001" }), makeNib({ id: "nibs-002", parentId: "nibs-001" })],
      {},
      "none",
      new Set<string>(),
    );
    const { handleKeydown } = setup({ selection, rows: table.rows, containment: table.containment });

    handleKeydown(keydown("ArrowLeft"));

    expect(selection.focusedNibId).toBe("nibs-001");
  });

  // A row at the display root whose real parent is rendered elsewhere. The flat
  // lens emits it for every parented nib — no nesting, so nothing to walk out
  // of, and focus must stay put rather than jump across the list. Built through
  // `buildTableData` rather than by hand so the row shape is the producer's, not
  // this file's idea of it.
  it("ArrowLeft does nothing at the display root even when the real parent is rendered", () => {
    const selection = new SelectionState();
    selection.focus("nibs-002");
    const table = buildTableData(
      [makeNib({ id: "nibs-001" }), makeNib({ id: "nibs-002", parentId: "nibs-001" })],
      {},
      "flat",
      new Set<string>(),
    );
    expect(table.rows.map(r => [r.nib.id, table.containment.containerOf(r.nib.id), r.hasChildren])).toEqual([
      ["nibs-001", null, false],
      ["nibs-002", null, false],
    ]);
    const { handleKeydown } = setup({ selection, rows: table.rows, containment: table.containment });

    handleKeydown(keydown("ArrowLeft"));

    expect(selection.focusedNibId).toBe("nibs-002");
  });

  // The row a grouped view draws around a member is the SECTION, which is no
  // nib's parent — so `displayParentId` is null there and ArrowLeft used to do
  // nothing at all inside a section. The Backlog is the shipped instance.
  it("ArrowLeft walks out of a section, which is nobody's parent", () => {
    const selection = new SelectionState();
    selection.focus("nibs-002");
    const table = buildTableData(
      [makeNib({ id: "nibs-001", type: "milestone" }), makeNib({ id: "nibs-002" })],
      {},
      "milestones",
      new Set<string>(),
    );
    const backlogId = table.containment.containerOf("nibs-002");
    expect(backlogId).not.toBeNull();
    // Not the real parent, and not reachable by walking display parents.
    expect(table.rows.find(r => r.nib.id === "nibs-002")!.displayParentId).toBeNull();
    const { handleKeydown } = setup({ selection, rows: table.rows, containment: table.containment });

    handleKeydown(keydown("ArrowLeft"));

    expect(selection.focusedNibId).toBe(backlogId);
  });

  it("ArrowRight on collapsed parent expands it", () => {
    const selection = new SelectionState();
    selection.focus("nibs-001");
    const toggleNode = vi.fn();
    const nib1 = makeNib({ id: "nibs-001" });
    const rows = [makeRow(nib1, { hasChildren: true })];
    // Collapsed — ArrowRight should expand
    const { handleKeydown } = setup({
      selection, rows, toggleNode,
      collapsedIds: new Set(["nibs-001"]),
    });

    handleKeydown(keydown("ArrowRight"));

    expect(toggleNode).toHaveBeenCalledWith("nibs-001");
  });

  // A section row's `data-nib-id` is `sectionRowId(key)`, and the areas lens
  // keys each section on the area PATH — free-form config text. `ValidateAreas`
  // (internal/config/areas.go) constrains an area name, but admits both
  // characters below, so each reaches the attribute selector this scroll
  // builds. A quote ends the selector's string early and throws; a backslash
  // parses as a CSS escape and silently addresses a different id.
  describe.each([
    { what: "a quote", key: 'Web "Next"' },
    { what: "a backslash", key: "Web\\Next" },
  ])("a section row whose key carries $what", ({ key }) => {
    it("is found by the focus scroll", () => {
      const rowId = `/section:${key}_`;
      // Run the scroll on the spot: production defers it to the next frame and
      // the assertions below are synchronous. Without the escape the two cases
      // fail differently — the quote throws out of the selector, the backslash
      // merely matches nothing — so the call assertion is what catches both.
      const raf = vi
        .spyOn(globalThis, "requestAnimationFrame")
        .mockImplementation((cb: FrameRequestCallback) => {
          cb(0);
          return 0;
        });
      try {
        const selection = new SelectionState();
        selection.focus("nibs-001");
        const rows = [makeRow(makeNib({ id: "nibs-001" })), makeRow(makeNib({ id: rowId }))];
        const { handleKeydown, scrollContainer } = setup({ selection, rows });

        scrollContainer.innerHTML = "<table><tbody></tbody></table>";
        const tr = document.createElement("tr");
        tr.dataset.nibId = rowId;
        scrollContainer.querySelector("tbody")!.appendChild(tr);
        const scrollIntoView = vi.spyOn(tr, "scrollIntoView");

        handleKeydown(keydown("ArrowDown"));

        expect(selection.focusedNibId).toBe(rowId);
        expect(scrollIntoView).toHaveBeenCalledWith({ block: "nearest" });
      } finally {
        raf.mockRestore();
      }
    });
  });
});
