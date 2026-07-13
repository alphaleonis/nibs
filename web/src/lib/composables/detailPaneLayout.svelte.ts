/**
 * Reactive shell over the pure pane-layout math (#nibs-dkm8).
 *
 * `createDetailPaneLayout` binds the pure `detailPaneLayout.ts` helpers to the
 * app's reactive inputs (size prefs, dock position, measured container size) and
 * exposes the derived percents plus the resize / drag-flush / reset handlers App
 * wires into PaneForge. App still owns the `ResizeObserver` measurement and the
 * PaneForge / Resizable wiring; this module owns the sizing math around them.
 *
 * The inputs are passed as getters so every read happens inside App's reactive
 * scopes: the exposed getters recompute (and their callers re-run) whenever the
 * dock position, the measured container size, or the relevant size pref changes.
 * The handlers are plain closures over `deps` — safe to pass detached to
 * PaneForge, which invokes them without a receiver.
 */

import type { DetailPanelPosition } from "../types";
import {
  orientationOf,
  percentToPixel,
  minPercent as minPercentOf,
  maxPercent as maxPercentOf,
  initialPercent as initialPercentOf,
  type PaneSizePrefs,
} from "./detailPaneLayout";

export interface DetailPaneLayoutDeps {
  /** Size prefs (width/height + setters/flush). The `Preferences` class satisfies this. */
  prefs: PaneSizePrefs;
  /** Active dock position (reactive). */
  position: () => DetailPanelPosition;
  /**
   * Measured extent of the split axis in px: height for a vertical (bottom)
   * split, width for a horizontal (right) split. Reactive; App owns the
   * measurement.
   */
  containerSize: () => number;
}

export interface DetailPaneLayout {
  /** PaneForge split direction for the active dock. */
  readonly direction: "horizontal" | "vertical";
  /** Current/default pane size as a percent of the container (clamped to [min, max]). */
  readonly defaultPercent: number;
  /** Minimum pane size as a percent of the container. */
  readonly minPercent: number;
  /** Maximum pane size as a percent of the container. */
  readonly maxPercent: number;
  /** PaneForge `onResize`: persist the dragged percent as px along the active axis. */
  onResize(sizePercent: number): void;
  /** PaneForge `onDraggingChange`: flush the persisted size when a drag ends. */
  onDraggingChange(dragging: boolean): void;
  /**
   * Reset the pane to its default size (dbl-click). Persists + flushes the
   * default px for the active axis and returns the percent App should
   * `resize()` the pane to.
   */
  reset(): number;
}

export function createDetailPaneLayout(deps: DetailPaneLayoutDeps): DetailPaneLayout {
  const { prefs } = deps;
  // Only a user drag should persist a size. PaneForge also fires onResize for
  // programmatic/mount resizes (initial layout, expand, reset) — persisting those
  // lets a transient mount size (floored to the min) clobber a stored width on
  // reload (nibs-lcyo). Track drag state and gate onResize on it.
  let dragging = false;

  return {
    get direction() {
      return orientationOf(deps.position()).direction;
    },
    get minPercent() {
      return minPercentOf(orientationOf(deps.position()), deps.containerSize());
    },
    get maxPercent() {
      return maxPercentOf();
    },
    get defaultPercent() {
      const orient = orientationOf(deps.position());
      // Unset → screen-relative default percent; user-set → anchored px → percent.
      return initialPercentOf(orient, orient.getRawSizePx(prefs), deps.containerSize());
    },
    onResize(sizePercent) {
      if (!dragging) return; // Ignore programmatic/mount resizes; persist user drags only.
      const cs = deps.containerSize();
      if (cs <= 0) return; // Skip until the ResizeObserver has measured the container.
      const orient = orientationOf(deps.position());
      orient.setSizePx(prefs, percentToPixel(sizePercent, cs, orient.defaultPx));
    },
    onDraggingChange(isDragging) {
      dragging = isDragging;
      // Drag ended — persist the size for the active orientation.
      if (!isDragging) orientationOf(deps.position()).flushSizePx(prefs);
    },
    reset() {
      const orient = orientationOf(deps.position());
      const cs = deps.containerSize();
      // Reset to the screen-relative default percent, persisted as px for the axis.
      const pct = initialPercentOf(orient, undefined, cs);
      orient.setSizePx(prefs, percentToPixel(pct, cs, orient.defaultPx));
      orient.flushSizePx(prefs);
      return pct;
    },
  };
}
