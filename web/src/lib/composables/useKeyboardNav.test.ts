import { describe, it, expect, vi } from "vitest";
import { SelectionState } from "../selection.svelte";
import type { RowData } from "../tableData";
import type { TreeTableNib } from "../types";
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
    updatedAt: "",
    parentId: null,
    blockingIds: [],
    blockedByIds: [],
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
    ...opts,
  };
}

describe("useKeyboardNav", () => {
  function setup(overrides: {
    selection?: SelectionState;
    rows?: RowData[];
    collapsedIds?: Set<string>;
    toggleNode?: (id: string) => void;
    onDragKeyDown?: (e: KeyboardEvent) => void;
    navigateToNib?: (id: string) => void;
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
      toggleNode,
      getScrollContainer: () => scrollContainer,
      onDragKeyDown,
      navigateToNib,
    });

    return { ...result, selection, toggleNode, onDragKeyDown, navigateToNib };
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
    // would still pass the assertion above but silently drop history. nibs-58c3.
    expect(navigateToNib).toHaveBeenCalledWith("nibs-002");
  });

  it("Space toggles focused row selection", () => {
    const selection = new SelectionState();
    selection.focus("nibs-001");
    const nib1 = makeNib({ id: "nibs-001" });
    const rows = [makeRow(nib1)];
    const { handleKeydown } = setup({ selection, rows });

    handleKeydown(keydown(" "));

    // toggleFocusedSelection should add the focused item to selection
    expect(selection.selectedIds.has("nibs-001")).toBe(true);
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
    const selection = new SelectionState();
    selection.focus("nibs-002");
    const nib1 = makeNib({ id: "nibs-001" });
    const nib2 = makeNib({ id: "nibs-002", parentId: "nibs-001" });
    const rows = [
      makeRow(nib1, { hasChildren: true }),
      makeRow(nib2),
    ];
    const { handleKeydown } = setup({ selection, rows });

    handleKeydown(keydown("ArrowLeft"));

    expect(selection.focusedNibId).toBe("nibs-001");
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
});
