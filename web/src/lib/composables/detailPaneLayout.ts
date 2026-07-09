/**
 * Pure layout math for the docked detail pane (#nibs-dkm8).
 *
 * No DOM, no runes — just the orientation mapping and the px<->% conversions
 * that the reactive shell (`detailPaneLayout.svelte.ts`) and the App component
 * wire into PaneForge. Extracted from App.svelte so the sizing math is
 * unit-testable without mounting a component.
 *
 * The detail pane docks either at the RIGHT (horizontal split, size axis =
 * width) or the BOTTOM (vertical split, size axis = height). Every
 * position-dependent value routes through the `ORIENTATIONS` descriptor so
 * adding a third dock is one table entry and a missing key is a compile-time
 * exhaustiveness error — instead of silently falling through to the
 * "right"/horizontal branch across the many per-axis sites.
 */

import {
  DEFAULT_DETAIL_PANEL_WIDTH,
  MIN_DETAIL_PANEL_WIDTH,
  DEFAULT_DETAIL_PANEL_HEIGHT,
  MIN_DETAIL_PANEL_HEIGHT,
  MAX_DETAIL_PANEL_PERCENT,
} from "../types";
import type { DetailPanelPosition } from "../types";

/** Sensible default when the container hasn't been measured yet (~30%). */
export const FALLBACK_DETAIL_SIZE_PERCENT = 30;

/**
 * Structural subset of `Preferences` the layout needs: the size along each
 * axis, plus its setter and flush. The `Preferences` class satisfies this, and
 * a plain stub satisfies it in tests (no component / runes required).
 */
export interface PaneSizePrefs {
  readonly detailPanelWidth: number;
  readonly detailPanelHeight: number;
  setDetailPanelWidth(px: number): void;
  setDetailPanelHeight(px: number): void;
  flushDetailPanelWidth(): void;
  flushDetailPanelHeight(): void;
}

/** Per-position sizing descriptor: split direction + the axis's px accessors. */
export interface OrientationDescriptor {
  readonly direction: "horizontal" | "vertical";
  readonly minPx: number;
  readonly defaultPx: number;
  getSizePx(p: PaneSizePrefs): number;
  setSizePx(p: PaneSizePrefs, px: number): void;
  flushSizePx(p: PaneSizePrefs): void;
}

/**
 * Single source of truth for pane sizing per dock position. Keyed by
 * `DetailPanelPosition` so a new position is one entry (and a missing key is a
 * compile error). NOTE: this table covers only the sizing math — a new position
 * must ALSO be handled at the other position-dependent surfaces, which are NOT
 * keyed off this record: SettingsSheet's `positionOptions` list.
 */
export const ORIENTATIONS: Record<DetailPanelPosition, OrientationDescriptor> = {
  right: {
    direction: "horizontal",
    minPx: MIN_DETAIL_PANEL_WIDTH,
    defaultPx: DEFAULT_DETAIL_PANEL_WIDTH,
    getSizePx: (p) => p.detailPanelWidth,
    setSizePx: (p, px) => p.setDetailPanelWidth(px),
    flushSizePx: (p) => p.flushDetailPanelWidth(),
  },
  bottom: {
    direction: "vertical",
    minPx: MIN_DETAIL_PANEL_HEIGHT,
    defaultPx: DEFAULT_DETAIL_PANEL_HEIGHT,
    getSizePx: (p) => p.detailPanelHeight,
    setSizePx: (p, px) => p.setDetailPanelHeight(px),
    flushSizePx: (p) => p.flushDetailPanelHeight(),
  },
};

/** Orientation descriptor for a dock position. */
export function orientationOf(position: DetailPanelPosition): OrientationDescriptor {
  return ORIENTATIONS[position];
}

/**
 * Convert a pixel size along the split axis to a percentage of the container.
 * Falls back to `FALLBACK_DETAIL_SIZE_PERCENT` before the container is measured
 * (size <= 0).
 */
export function pixelToPercent(px: number, containerSize: number): number {
  if (containerSize <= 0) return FALLBACK_DETAIL_SIZE_PERCENT;
  return (px / containerSize) * 100;
}

/**
 * Convert a percentage back to a pixel size along the split axis. Before the
 * container is measured (size <= 0), returns the axis's default px (not a
 * hardcoded width).
 */
export function percentToPixel(pct: number, containerSize: number, defaultPx: number): number {
  if (containerSize <= 0) return defaultPx;
  return (pct / 100) * containerSize;
}

/** Clamp a percentage to the inclusive [min, max] range. */
export function clampPercent(pct: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, pct));
}

/** Minimum pane size as a percent of the container (the active axis's min px). */
export function minPercent(orient: OrientationDescriptor, containerSize: number): number {
  return pixelToPercent(orient.minPx, containerSize);
}

/** Maximum pane size as a percent — shared across both orientations. */
export function maxPercent(): number {
  return MAX_DETAIL_PANEL_PERCENT;
}

/**
 * Current/default pane size as a percent of the container, clamped to the
 * [minPercent, maxPercent] band. Preferences already floor the stored px at the
 * axis's min, and PaneForge clamps `defaultSize`/`resize()` to the same band, so
 * clamping here is behavior-preserving and keeps the value the module hands to
 * `resize()` consistent with the pane's own bounds.
 */
export function defaultPercent(
  orient: OrientationDescriptor,
  currentSizePx: number,
  containerSize: number,
): number {
  return clampPercent(
    pixelToPercent(currentSizePx, containerSize),
    minPercent(orient, containerSize),
    maxPercent(),
  );
}
