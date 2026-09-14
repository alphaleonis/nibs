/**
 * Pure layout math for the docked detail pane: the per-dock sizing table and
 * px<->% conversions. `detailPaneLayout.svelte.ts` binds it to reactive inputs.
 */

import {
  DEFAULT_DETAIL_PANEL_WIDTH,
  DEFAULT_DETAIL_PANEL_PERCENT,
  MIN_DETAIL_PANEL_WIDTH,
  DEFAULT_DETAIL_PANEL_HEIGHT,
  MIN_DETAIL_PANEL_HEIGHT,
  MAX_DETAIL_PANEL_PERCENT,
} from "../types";
import type { DetailPanelPosition } from "../types";

/** Used before the container has been measured. */
export const FALLBACK_DETAIL_SIZE_PERCENT = 30;

/** The subset of `Preferences` the layout reads and writes. */
export interface PaneSizePrefs {
  readonly detailPanelWidth: number;
  readonly detailPanelHeight: number;
  /** `undefined` until the user has resized. */
  readonly detailPanelWidthRaw: number | undefined;
  readonly detailPanelHeightRaw: number | undefined;
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
  getRawSizePx(p: PaneSizePrefs): number | undefined;
  setSizePx(p: PaneSizePrefs, px: number): void;
  flushSizePx(p: PaneSizePrefs): void;
}

/**
 * Sizing per dock position. A new position also needs an entry in
 * SettingsSheet's `positionOptions`.
 */
export const ORIENTATIONS: Record<DetailPanelPosition, OrientationDescriptor> = {
  right: {
    direction: "horizontal",
    minPx: MIN_DETAIL_PANEL_WIDTH,
    defaultPx: DEFAULT_DETAIL_PANEL_WIDTH,
    getSizePx: (p) => p.detailPanelWidth,
    getRawSizePx: (p) => p.detailPanelWidthRaw,
    setSizePx: (p, px) => p.setDetailPanelWidth(px),
    flushSizePx: (p) => p.flushDetailPanelWidth(),
  },
  bottom: {
    direction: "vertical",
    minPx: MIN_DETAIL_PANEL_HEIGHT,
    defaultPx: DEFAULT_DETAIL_PANEL_HEIGHT,
    getSizePx: (p) => p.detailPanelHeight,
    getRawSizePx: (p) => p.detailPanelHeightRaw,
    setSizePx: (p, px) => p.setDetailPanelHeight(px),
    flushSizePx: (p) => p.flushDetailPanelHeight(),
  },
};

export function orientationOf(position: DetailPanelPosition): OrientationDescriptor {
  return ORIENTATIONS[position];
}

/** Returns `FALLBACK_DETAIL_SIZE_PERCENT` until the container is measured. */
export function pixelToPercent(px: number, containerSize: number): number {
  if (containerSize <= 0) return FALLBACK_DETAIL_SIZE_PERCENT;
  return (px / containerSize) * 100;
}

/** Returns `defaultPx` until the container is measured. */
export function percentToPixel(pct: number, containerSize: number, defaultPx: number): number {
  if (containerSize <= 0) return defaultPx;
  return (pct / 100) * containerSize;
}

export function clampPercent(pct: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, pct));
}

export function minPercent(orient: OrientationDescriptor, containerSize: number): number {
  return pixelToPercent(orient.minPx, containerSize);
}

/** Shared by both orientations. */
export function maxPercent(): number {
  return MAX_DETAIL_PANEL_PERCENT;
}

/** A px size as a percent of the container, clamped to [minPercent, maxPercent]. */
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

/**
 * Initial pane size: DEFAULT_DETAIL_PANEL_PERCENT until the user has resized,
 * then the stored px. Clamped to [minPercent, maxPercent].
 */
export function initialPercent(
  orient: OrientationDescriptor,
  rawSizePx: number | undefined,
  containerSize: number,
): number {
  if (rawSizePx === undefined) {
    return clampPercent(
      DEFAULT_DETAIL_PANEL_PERCENT,
      minPercent(orient, containerSize),
      maxPercent(),
    );
  }
  return defaultPercent(orient, rawSizePx, containerSize);
}
