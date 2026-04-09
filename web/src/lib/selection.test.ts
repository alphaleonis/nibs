import { describe, it, expect } from "vitest";
import { SelectionState } from "./selection.svelte";

describe("SelectionState", () => {
  it("starts with no selection and panel closed", () => {
    const state = new SelectionState();
    expect(state.selectedNibId).toBeNull();
    expect(state.panelOpen).toBe(false);
  });

  it("select() sets selectedNibId and panelOpen becomes true", () => {
    const state = new SelectionState();
    state.select("nibs-abc1");
    expect(state.selectedNibId).toBe("nibs-abc1");
    expect(state.panelOpen).toBe(true);
  });

  it("close() clears selectedNibId and panelOpen becomes false", () => {
    const state = new SelectionState();
    state.select("nibs-abc1");
    state.close();
    expect(state.selectedNibId).toBeNull();
    expect(state.panelOpen).toBe(false);
  });

  it("select() can change selection to a different nib", () => {
    const state = new SelectionState();
    state.select("nibs-abc1");
    state.select("nibs-xyz2");
    expect(state.selectedNibId).toBe("nibs-xyz2");
    expect(state.panelOpen).toBe(true);
  });

  it("focusedNibId starts as null", () => {
    const state = new SelectionState();
    expect(state.focusedNibId).toBeNull();
  });

  it("focus(nibId) sets focusedNibId", () => {
    const state = new SelectionState();
    state.focus("nibs-abc1");
    expect(state.focusedNibId).toBe("nibs-abc1");
  });

  it("clearFocus() resets focusedNibId to null", () => {
    const state = new SelectionState();
    state.focus("nibs-abc1");
    state.clearFocus();
    expect(state.focusedNibId).toBeNull();
  });

  it("select(nibId) also sets focusedNibId", () => {
    const state = new SelectionState();
    state.select("nibs-abc1");
    expect(state.focusedNibId).toBe("nibs-abc1");
  });

  it("close() does NOT clear focusedNibId", () => {
    const state = new SelectionState();
    state.select("nibs-abc1");
    expect(state.focusedNibId).toBe("nibs-abc1");
    state.close();
    expect(state.focusedNibId).toBe("nibs-abc1");
    expect(state.selectedNibId).toBeNull();
  });

  describe("multi-select", () => {
    it("starts with empty selectedIds and no anchor", () => {
      const state = new SelectionState();
      expect(state.selectedIds.size).toBe(0);
      expect(state.anchorId).toBeNull();
      expect(state.hasMultiSelect).toBe(false);
    });

    it("select() sets selectedIds to the single nib and sets anchor", () => {
      const state = new SelectionState();
      state.select("nibs-abc1");
      expect(state.selectedIds.has("nibs-abc1")).toBe(true);
      expect(state.selectedIds.size).toBe(1);
      expect(state.anchorId).toBe("nibs-abc1");
      expect(state.hasMultiSelect).toBe(false);
    });

    it("select() clears previous multi-selection", () => {
      const state = new SelectionState();
      state.toggleSelect("nibs-abc1");
      state.toggleSelect("nibs-xyz2");
      expect(state.selectedIds.size).toBe(2);
      expect(state.hasMultiSelect).toBe(true);

      state.select("nibs-new1");
      expect(state.selectedIds.size).toBe(1);
      expect(state.selectedIds.has("nibs-new1")).toBe(true);
      expect(state.hasMultiSelect).toBe(false);
    });

    it("toggleSelect() adds a nib to selectedIds", () => {
      const state = new SelectionState();
      state.select("nibs-abc1");
      state.toggleSelect("nibs-xyz2");
      expect(state.selectedIds.has("nibs-abc1")).toBe(true);
      expect(state.selectedIds.has("nibs-xyz2")).toBe(true);
      expect(state.selectedIds.size).toBe(2);
      expect(state.hasMultiSelect).toBe(true);
    });

    it("toggleSelect() removes a nib from selectedIds if already present", () => {
      const state = new SelectionState();
      state.select("nibs-abc1");
      state.toggleSelect("nibs-xyz2");
      state.toggleSelect("nibs-abc1"); // remove it
      expect(state.selectedIds.has("nibs-abc1")).toBe(false);
      expect(state.selectedIds.has("nibs-xyz2")).toBe(true);
      expect(state.selectedIds.size).toBe(1);
    });

    it("toggleSelect() updates anchor and focus", () => {
      const state = new SelectionState();
      state.toggleSelect("nibs-abc1");
      expect(state.anchorId).toBe("nibs-abc1");
      expect(state.focusedNibId).toBe("nibs-abc1");
    });

    it("toggleSelect() with single remaining item sets selectedNibId", () => {
      const state = new SelectionState();
      state.toggleSelect("nibs-abc1");
      expect(state.selectedNibId).toBe("nibs-abc1");
      expect(state.panelOpen).toBe(true);
    });

    it("toggleSelect() with multiple items clears selectedNibId", () => {
      const state = new SelectionState();
      state.toggleSelect("nibs-abc1");
      state.toggleSelect("nibs-xyz2");
      expect(state.selectedNibId).toBeNull();
      expect(state.panelOpen).toBe(false);
    });

    it("rangeSelect() selects range from anchor to target", () => {
      const state = new SelectionState();
      const visibleIds = ["nibs-001", "nibs-002", "nibs-003", "nibs-004", "nibs-005"];
      state.select("nibs-002"); // sets anchor to nibs-002

      state.rangeSelect("nibs-004", visibleIds);
      expect(state.selectedIds.size).toBe(3);
      expect(state.selectedIds.has("nibs-002")).toBe(true);
      expect(state.selectedIds.has("nibs-003")).toBe(true);
      expect(state.selectedIds.has("nibs-004")).toBe(true);
    });

    it("rangeSelect() works backwards (target before anchor)", () => {
      const state = new SelectionState();
      const visibleIds = ["nibs-001", "nibs-002", "nibs-003", "nibs-004", "nibs-005"];
      state.select("nibs-004"); // sets anchor to nibs-004

      state.rangeSelect("nibs-002", visibleIds);
      expect(state.selectedIds.size).toBe(3);
      expect(state.selectedIds.has("nibs-002")).toBe(true);
      expect(state.selectedIds.has("nibs-003")).toBe(true);
      expect(state.selectedIds.has("nibs-004")).toBe(true);
    });

    it("rangeSelect() preserves anchor", () => {
      const state = new SelectionState();
      const visibleIds = ["nibs-001", "nibs-002", "nibs-003"];
      state.select("nibs-001");
      state.rangeSelect("nibs-003", visibleIds);
      expect(state.anchorId).toBe("nibs-001"); // anchor unchanged
    });

    it("rangeSelect() sets focus to target", () => {
      const state = new SelectionState();
      const visibleIds = ["nibs-001", "nibs-002", "nibs-003"];
      state.select("nibs-001");
      state.rangeSelect("nibs-003", visibleIds);
      expect(state.focusedNibId).toBe("nibs-003");
    });

    it("rangeSelect() with no anchor uses target as anchor", () => {
      const state = new SelectionState();
      const visibleIds = ["nibs-001", "nibs-002", "nibs-003"];
      state.rangeSelect("nibs-002", visibleIds);
      expect(state.selectedIds.size).toBe(1);
      expect(state.selectedIds.has("nibs-002")).toBe(true);
    });

    it("rangeSelect() with multi-item range clears selectedNibId", () => {
      const state = new SelectionState();
      const visibleIds = ["nibs-001", "nibs-002", "nibs-003"];
      state.select("nibs-001");
      state.rangeSelect("nibs-003", visibleIds);
      expect(state.selectedNibId).toBeNull();
      expect(state.hasMultiSelect).toBe(true);
    });

    it("rangeSelect() with single item sets selectedNibId", () => {
      const state = new SelectionState();
      const visibleIds = ["nibs-001", "nibs-002", "nibs-003"];
      state.select("nibs-002");
      state.rangeSelect("nibs-002", visibleIds);
      expect(state.selectedNibId).toBe("nibs-002");
      expect(state.selectedIds.size).toBe(1);
    });

    it("toggleFocusedSelection() toggles focused row in selectedIds", () => {
      const state = new SelectionState();
      state.select("nibs-abc1"); // sets focus + selected
      state.focus("nibs-xyz2");

      state.toggleFocusedSelection();
      expect(state.selectedIds.has("nibs-xyz2")).toBe(true);
      expect(state.selectedIds.has("nibs-abc1")).toBe(true);
      expect(state.selectedIds.size).toBe(2);

      // Toggle again to remove
      state.toggleFocusedSelection();
      expect(state.selectedIds.has("nibs-xyz2")).toBe(false);
      expect(state.selectedIds.has("nibs-abc1")).toBe(true);
      expect(state.selectedIds.size).toBe(1);
    });

    it("toggleFocusedSelection() does nothing when no focus", () => {
      const state = new SelectionState();
      state.toggleFocusedSelection();
      expect(state.selectedIds.size).toBe(0);
    });

    it("isSelected() returns correct value", () => {
      const state = new SelectionState();
      state.select("nibs-abc1");
      expect(state.isSelected("nibs-abc1")).toBe(true);
      expect(state.isSelected("nibs-xyz2")).toBe(false);
    });

    it("deselectAll() clears selectedIds and anchor", () => {
      const state = new SelectionState();
      state.select("nibs-abc1");
      state.toggleSelect("nibs-xyz2");
      state.deselectAll();
      expect(state.selectedIds.size).toBe(0);
      expect(state.anchorId).toBeNull();
      // Does NOT clear selectedNibId or focusedNibId
      expect(state.focusedNibId).toBe("nibs-xyz2");
    });

    it("clearAll() clears everything", () => {
      const state = new SelectionState();
      state.select("nibs-abc1");
      state.toggleSelect("nibs-xyz2");
      state.ensureVisible("nibs-abc1");
      state.clearAll();
      expect(state.selectedIds.size).toBe(0);
      expect(state.selectedNibId).toBeNull();
      expect(state.focusedNibId).toBeNull();
      expect(state.anchorId).toBeNull();
      expect(state.pendingEnsureVisibleId).toBeNull();
      expect(state.panelOpen).toBe(false);
      expect(state.hasMultiSelect).toBe(false);
    });

    it("select() clears multi-selection and resets to single item", () => {
      const state = new SelectionState();
      state.toggleSelect("nibs-abc1");
      state.toggleSelect("nibs-xyz2");

      state.select("nibs-new1");
      expect(state.selectedIds.size).toBe(1);
      expect(state.selectedIds.has("nibs-new1")).toBe(true);
      expect(state.selectedNibId).toBe("nibs-new1");
      expect(state.focusedNibId).toBe("nibs-new1");
      expect(state.anchorId).toBe("nibs-new1");
    });

    it("rangeSelect() is a no-op when target is not in visibleIds", () => {
      const state = new SelectionState();
      const visibleIds = ["nibs-001", "nibs-002"];
      state.select("nibs-001");
      state.rangeSelect("nibs-not-found", visibleIds);
      // Selection unchanged — still just the anchor
      expect(state.selectedIds.size).toBe(1);
      expect(state.selectedIds.has("nibs-001")).toBe(true);
    });
  });

  describe("ensureVisible", () => {
    it("ensureVisible sets pendingEnsureVisibleId", () => {
      const state = new SelectionState();
      expect(state.pendingEnsureVisibleId).toBeNull();
      state.ensureVisible("nibs-abc1");
      expect(state.pendingEnsureVisibleId).toBe("nibs-abc1");
    });

    it("clearEnsureVisible resets pendingEnsureVisibleId to null", () => {
      const state = new SelectionState();
      state.ensureVisible("nibs-abc1");
      state.clearEnsureVisible();
      expect(state.pendingEnsureVisibleId).toBeNull();
    });
  });
});
