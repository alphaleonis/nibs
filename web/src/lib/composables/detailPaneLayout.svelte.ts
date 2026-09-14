/**
 * Binds the pure `detailPaneLayout.ts` math to App's reactive inputs and
 * exposes the percents and handlers App wires into PaneForge. App owns the
 * ResizeObserver measurement.
 *
 * Inputs are getters so every read tracks inside App's reactive scopes. The
 * handlers close over `deps` and need no receiver.
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
  prefs: PaneSizePrefs;
  position: () => DetailPanelPosition;
  /** Split-axis extent in px: height for a bottom dock, width for a right dock. */
  containerSize: () => number;
}

export interface DetailPaneLayout {
  /** PaneForge split direction for the active dock. */
  readonly direction: "horizontal" | "vertical";
  /** Current pane size as a percent of the container (clamped to [min, max]). */
  readonly defaultPercent: number;
  readonly minPercent: number;
  readonly maxPercent: number;
  /** PaneForge `onResize`: persists a user drag as px on the active axis. */
  onResize(sizePercent: number): void;
  /** PaneForge `onDraggingChange`: flushes the persisted size when a drag ends. */
  onDraggingChange(dragging: boolean): void;
  /** Resets to the default size, persists it, and returns the percent to `resize()` to. */
  reset(): number;
}

export function createDetailPaneLayout(deps: DetailPaneLayoutDeps): DetailPaneLayout {
  const { prefs } = deps;
  // PaneForge also fires onResize for mount and programmatic resizes; persisting
  // those lets a floored mount size overwrite the stored one. Persist drags only.
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
      return initialPercentOf(orient, orient.getRawSizePx(prefs), deps.containerSize());
    },
    onResize(sizePercent) {
      if (!dragging) return;
      const cs = deps.containerSize();
      if (cs <= 0) return; // Not measured yet.
      const orient = orientationOf(deps.position());
      orient.setSizePx(prefs, percentToPixel(sizePercent, cs, orient.defaultPx));
    },
    onDraggingChange(isDragging) {
      dragging = isDragging;
      if (!isDragging) orientationOf(deps.position()).flushSizePx(prefs);
    },
    reset() {
      const orient = orientationOf(deps.position());
      const cs = deps.containerSize();
      const pct = initialPercentOf(orient, undefined, cs);
      orient.setSizePx(prefs, percentToPixel(pct, cs, orient.defaultPx));
      orient.flushSizePx(prefs);
      return pct;
    },
  };
}
