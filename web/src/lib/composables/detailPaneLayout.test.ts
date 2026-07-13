import { describe, it, expect, vi } from "vitest";
import {
  FALLBACK_DETAIL_SIZE_PERCENT,
  ORIENTATIONS,
  orientationOf,
  pixelToPercent,
  percentToPixel,
  clampPercent,
  minPercent,
  maxPercent,
  defaultPercent,
  initialPercent,
  type PaneSizePrefs,
} from "./detailPaneLayout";
import {
  DEFAULT_DETAIL_PANEL_WIDTH,
  DEFAULT_DETAIL_PANEL_PERCENT,
  MIN_DETAIL_PANEL_WIDTH,
  DEFAULT_DETAIL_PANEL_HEIGHT,
  MIN_DETAIL_PANEL_HEIGHT,
  MAX_DETAIL_PANEL_PERCENT,
} from "../types";

/** Minimal in-memory prefs stub satisfying `PaneSizePrefs`, with call spies. */
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

describe("initialPercent", () => {
  const right = orientationOf("right");

  it("opens at DEFAULT_DETAIL_PANEL_PERCENT when unset, independent of screen size", () => {
    // The whole point: same percent at a small and a large container.
    expect(initialPercent(right, undefined, 1000)).toBe(DEFAULT_DETAIL_PANEL_PERCENT);
    expect(initialPercent(right, undefined, 2400)).toBe(DEFAULT_DETAIL_PANEL_PERCENT);
  });

  it("anchors to the stored px (px -> percent) once set", () => {
    expect(initialPercent(right, 600, 1200)).toBe(50); // 600 / 1200
  });

  it("clamps the unset default up to the min band on a tiny container", () => {
    // min% = 200/300 ≈ 66.7 > 40, so the floor wins over the default percent.
    expect(initialPercent(right, undefined, 300)).toBeCloseTo((200 / 300) * 100, 5);
  });
});

describe("detailPaneLayout px<->% conversion", () => {
  it("round-trips px -> % -> px against a measured container", () => {
    const container = 1000;
    const px = 350;
    const pct = pixelToPercent(px, container);
    expect(pct).toBe(35);
    // percentToPixel's defaultPx fallback is irrelevant while the container is measured.
    expect(percentToPixel(pct, container, DEFAULT_DETAIL_PANEL_WIDTH)).toBe(px);
  });

  it("round-trips a range of sizes exactly", () => {
    const container = 1280;
    for (const px of [200, 400, 640, 900]) {
      const pct = pixelToPercent(px, container);
      expect(percentToPixel(pct, container, DEFAULT_DETAIL_PANEL_WIDTH)).toBeCloseTo(px, 6);
    }
  });

  it("pixelToPercent falls back before the container is measured (size <= 0)", () => {
    expect(pixelToPercent(400, 0)).toBe(FALLBACK_DETAIL_SIZE_PERCENT);
    expect(pixelToPercent(400, -5)).toBe(FALLBACK_DETAIL_SIZE_PERCENT);
  });

  it("percentToPixel falls back to the axis default before the container is measured", () => {
    expect(percentToPixel(50, 0, DEFAULT_DETAIL_PANEL_WIDTH)).toBe(DEFAULT_DETAIL_PANEL_WIDTH);
    expect(percentToPixel(50, -1, DEFAULT_DETAIL_PANEL_HEIGHT)).toBe(DEFAULT_DETAIL_PANEL_HEIGHT);
  });
});

describe("detailPaneLayout clampPercent", () => {
  it("passes through values inside [min, max]", () => {
    expect(clampPercent(40, 20, 75)).toBe(40);
  });

  it("clamps below min up to min", () => {
    expect(clampPercent(10, 20, 75)).toBe(20);
  });

  it("clamps above max down to max", () => {
    expect(clampPercent(90, 20, 75)).toBe(75);
  });

  it("prefers max when min > max (degenerate tiny-container band)", () => {
    // Math.min(max, Math.max(min, x)) resolves to max when min exceeds max.
    expect(clampPercent(50, 80, 75)).toBe(75);
  });
});

describe("detailPaneLayout orientation mapping", () => {
  it("orientationOf('right') maps to the horizontal/width axis", () => {
    const orient = orientationOf("right");
    expect(orient).toBe(ORIENTATIONS.right);
    expect(orient.direction).toBe("horizontal");
    expect(orient.minPx).toBe(MIN_DETAIL_PANEL_WIDTH);
    expect(orient.defaultPx).toBe(DEFAULT_DETAIL_PANEL_WIDTH);

    const prefs = makePrefs({ width: 512 });
    expect(orient.getSizePx(prefs)).toBe(512);

    orient.setSizePx(prefs, 480);
    expect(prefs.setDetailPanelWidth).toHaveBeenCalledWith(480);
    expect(prefs.setDetailPanelHeight).not.toHaveBeenCalled();

    orient.flushSizePx(prefs);
    expect(prefs.flushDetailPanelWidth).toHaveBeenCalledOnce();
    expect(prefs.flushDetailPanelHeight).not.toHaveBeenCalled();
  });

  it("orientationOf('bottom') maps to the vertical/height axis", () => {
    const orient = orientationOf("bottom");
    expect(orient).toBe(ORIENTATIONS.bottom);
    expect(orient.direction).toBe("vertical");
    expect(orient.minPx).toBe(MIN_DETAIL_PANEL_HEIGHT);
    expect(orient.defaultPx).toBe(DEFAULT_DETAIL_PANEL_HEIGHT);

    const prefs = makePrefs({ height: 360 });
    expect(orient.getSizePx(prefs)).toBe(360);

    orient.setSizePx(prefs, 320);
    expect(prefs.setDetailPanelHeight).toHaveBeenCalledWith(320);
    expect(prefs.setDetailPanelWidth).not.toHaveBeenCalled();

    orient.flushSizePx(prefs);
    expect(prefs.flushDetailPanelHeight).toHaveBeenCalledOnce();
    expect(prefs.flushDetailPanelWidth).not.toHaveBeenCalled();
  });
});

describe("detailPaneLayout percent derivation", () => {
  it("derives min/max/default percent for a measured container (right)", () => {
    const orient = orientationOf("right");
    const container = 1000;
    // min = MIN_WIDTH (200) / 1000 = 20%
    expect(minPercent(orient, container)).toBe(20);
    // max is the shared cap
    expect(maxPercent()).toBe(MAX_DETAIL_PANEL_PERCENT);
    // default = current width (400) / 1000 = 40%
    expect(defaultPercent(orient, 400, container)).toBe(40);
  });

  it("derives min/default percent for a measured container (bottom)", () => {
    const orient = orientationOf("bottom");
    const container = 600;
    // min = MIN_HEIGHT (150) / 600 = 25%
    expect(minPercent(orient, container)).toBe(25);
    // default = current height (300) / 600 = 50%
    expect(defaultPercent(orient, 300, container)).toBe(50);
  });

  it("clamps defaultPercent down to the max band when the pane exceeds it", () => {
    const orient = orientationOf("right");
    const container = 500;
    // 450 / 500 = 90% would exceed the 75% cap
    expect(defaultPercent(orient, 450, container)).toBe(MAX_DETAIL_PANEL_PERCENT);
  });

  it("falls back to FALLBACK percent before the container is measured", () => {
    const orient = orientationOf("right");
    // Every conversion falls back to 30%, so min == default == FALLBACK.
    expect(minPercent(orient, 0)).toBe(FALLBACK_DETAIL_SIZE_PERCENT);
    expect(defaultPercent(orient, 400, 0)).toBe(FALLBACK_DETAIL_SIZE_PERCENT);
  });
});
