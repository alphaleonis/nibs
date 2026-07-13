import { describe, it, expect, vi } from "vitest";
import { createDetailPaneLayout } from "./detailPaneLayout.svelte";
import type { PaneSizePrefs } from "./detailPaneLayout";
import type { DetailPanelPosition } from "../types";
import {
  DEFAULT_DETAIL_PANEL_WIDTH,
  DEFAULT_DETAIL_PANEL_HEIGHT,
  MAX_DETAIL_PANEL_PERCENT,
} from "../types";

function makePrefs(overrides: { width?: number; height?: number } = {}) {
  return {
    detailPanelWidth: overrides.width ?? DEFAULT_DETAIL_PANEL_WIDTH,
    detailPanelHeight: overrides.height ?? DEFAULT_DETAIL_PANEL_HEIGHT,
    detailPanelWidthRaw: overrides.width,
    detailPanelHeightRaw: overrides.height,
    setDetailPanelWidth: vi.fn(),
    setDetailPanelHeight: vi.fn(),
    flushDetailPanelWidth: vi.fn(),
    flushDetailPanelHeight: vi.fn(),
  } satisfies PaneSizePrefs;
}

/** Build a layout over mutable inputs so we can simulate reactive changes. */
function setup(opts: { position?: DetailPanelPosition; containerSize?: number; prefs?: ReturnType<typeof makePrefs> } = {}) {
  const state = {
    position: (opts.position ?? "right") as DetailPanelPosition,
    containerSize: opts.containerSize ?? 1000,
  };
  const prefs = opts.prefs ?? makePrefs();
  const layout = createDetailPaneLayout({
    prefs,
    position: () => state.position,
    containerSize: () => state.containerSize,
  });
  return { layout, prefs, state };
}

describe("createDetailPaneLayout getters", () => {
  it("exposes the split direction for the active dock and tracks position changes", () => {
    const { layout, state } = setup({ position: "right" });
    expect(layout.direction).toBe("horizontal");

    state.position = "bottom";
    expect(layout.direction).toBe("vertical");
  });

  it("derives min/max/default percent for the right dock", () => {
    const { layout } = setup({ position: "right", containerSize: 1000, prefs: makePrefs({ width: 400 }) });
    expect(layout.minPercent).toBe(20); // 200 / 1000
    expect(layout.maxPercent).toBe(MAX_DETAIL_PANEL_PERCENT);
    expect(layout.defaultPercent).toBe(40); // 400 / 1000
  });

  it("derives percents against the height axis for the bottom dock", () => {
    const { layout } = setup({ position: "bottom", containerSize: 600, prefs: makePrefs({ height: 300 }) });
    expect(layout.minPercent).toBe(25); // 150 / 600
    expect(layout.defaultPercent).toBe(50); // 300 / 600
  });

  it("clamps defaultPercent to the max band", () => {
    const { layout } = setup({ position: "right", containerSize: 500, prefs: makePrefs({ width: 450 }) });
    expect(layout.defaultPercent).toBe(MAX_DETAIL_PANEL_PERCENT); // 90% -> 75%
  });

  it("opens unset panes at the default percent, independent of container width", () => {
    // makePrefs() leaves the size unset (raw undefined) → screen-relative default.
    const small = setup({ position: "right", containerSize: 1000 });
    const large = setup({ position: "right", containerSize: 2400 });
    expect(small.layout.defaultPercent).toBe(40); // DEFAULT_DETAIL_PANEL_PERCENT
    expect(large.layout.defaultPercent).toBe(40); // NOT ~17% as a fixed 400px would give
  });
});

describe("createDetailPaneLayout.onResize", () => {
  it("persists the dragged percent as px along the width axis (right)", () => {
    const { layout, prefs } = setup({ position: "right", containerSize: 1000 });
    layout.onDraggingChange(true);
    layout.onResize(35);
    expect(prefs.setDetailPanelWidth).toHaveBeenCalledWith(350);
    expect(prefs.setDetailPanelHeight).not.toHaveBeenCalled();
  });

  it("persists along the height axis for the bottom dock", () => {
    const { layout, prefs } = setup({ position: "bottom", containerSize: 800 });
    layout.onDraggingChange(true);
    layout.onResize(25);
    expect(prefs.setDetailPanelHeight).toHaveBeenCalledWith(200);
    expect(prefs.setDetailPanelWidth).not.toHaveBeenCalled();
  });

  it("ignores resizes that are not from a user drag (programmatic/mount resize)", () => {
    // PaneForge fires onResize during mount/expand/reset; persisting those would
    // clobber a stored width with a transient (min-floored) size on reload.
    const { layout, prefs } = setup({ position: "right", containerSize: 1000 });
    layout.onResize(35); // no drag in progress
    expect(prefs.setDetailPanelWidth).not.toHaveBeenCalled();
  });

  it("skips persistence until the container is measured (size <= 0)", () => {
    const { layout, prefs } = setup({ position: "right", containerSize: 0 });
    layout.onDraggingChange(true);
    layout.onResize(35);
    expect(prefs.setDetailPanelWidth).not.toHaveBeenCalled();
  });
});

describe("createDetailPaneLayout.onDraggingChange", () => {
  it("flushes the active axis only when a drag ends", () => {
    const { layout, prefs } = setup({ position: "right" });

    layout.onDraggingChange(true);
    expect(prefs.flushDetailPanelWidth).not.toHaveBeenCalled();

    layout.onDraggingChange(false);
    expect(prefs.flushDetailPanelWidth).toHaveBeenCalledOnce();
    expect(prefs.flushDetailPanelHeight).not.toHaveBeenCalled();
  });

  it("flushes the height axis for the bottom dock", () => {
    const { layout, prefs } = setup({ position: "bottom" });
    layout.onDraggingChange(false);
    expect(prefs.flushDetailPanelHeight).toHaveBeenCalledOnce();
    expect(prefs.flushDetailPanelWidth).not.toHaveBeenCalled();
  });
});

describe("createDetailPaneLayout.reset", () => {
  it("resets to the default percent, persisted as px for the axis (right)", () => {
    const { layout, prefs } = setup({ position: "right", containerSize: 1000, prefs: makePrefs({ width: 700 }) });
    const pct = layout.reset();
    expect(pct).toBe(40); // DEFAULT_DETAIL_PANEL_PERCENT
    expect(prefs.setDetailPanelWidth).toHaveBeenCalledWith(400); // 40% of 1000
    expect(prefs.flushDetailPanelWidth).toHaveBeenCalledOnce();
  });

  it("resets against the height axis for the bottom dock", () => {
    const { layout, prefs } = setup({ position: "bottom", containerSize: 600, prefs: makePrefs({ height: 500 }) });
    const pct = layout.reset();
    expect(pct).toBe(40); // DEFAULT_DETAIL_PANEL_PERCENT (was 50 when reset used the fixed 300px default)
    expect(prefs.setDetailPanelHeight).toHaveBeenCalledWith(240); // 40% of 600
    expect(prefs.flushDetailPanelHeight).toHaveBeenCalledOnce();
    expect(prefs.setDetailPanelWidth).not.toHaveBeenCalled();
  });
});
