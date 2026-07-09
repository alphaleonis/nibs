import { untrack } from "svelte";
import { loadPreferences, savePreferences } from "./storage";
import { ALL_COLUMN_KEYS, DEFAULT_COLUMN_WIDTHS, DEFAULT_DETAIL_PANEL_WIDTH, MIN_DETAIL_PANEL_WIDTH, DEFAULT_DETAIL_PANEL_HEIGHT, MIN_DETAIL_PANEL_HEIGHT, DEFAULT_DETAIL_PANEL_POSITION, DEFAULT_THEME } from "./types";
import type { NibFilter, ViewLevel, ColumnKey, RowDensity, Theme, DetailPanelPosition } from "./types";

export class Preferences {
  filter: NibFilter = $state({});
  viewLevel: ViewLevel = $state("none");
  columnVisibility: Partial<Record<ViewLevel, ColumnKey[]>> = $state({});
  columnWidths: Partial<Record<ViewLevel, Partial<Record<ColumnKey, number>>>> = $state({});
  #detailPanelWidth: number | undefined = $state(undefined);
  // Discrete toggle → auto-saved (like theme/rowDensity).
  detailPanelPosition: DetailPanelPosition = $state(DEFAULT_DETAIL_PANEL_POSITION);
  // Pointer-pattern → excluded from auto-save, flushed like width.
  #detailPanelHeight: number | undefined = $state(undefined);
  rowDensity: RowDensity = $state("compact");
  theme: Theme = $state(DEFAULT_THEME);

  visibleColumns: ColumnKey[] = $derived(
    this.columnVisibility[this.viewLevel] ?? [...ALL_COLUMN_KEYS]
  );

  currentColumnWidths: Record<ColumnKey, number> = $derived({
    ...DEFAULT_COLUMN_WIDTHS,
    ...(this.columnWidths[this.viewLevel] ?? {}),
  });

  detailPanelWidth: number = $derived(
    this.#detailPanelWidth ?? DEFAULT_DETAIL_PANEL_WIDTH
  );

  detailPanelHeight: number = $derived(
    this.#detailPanelHeight ?? DEFAULT_DETAIL_PANEL_HEIGHT
  );

  constructor() {
    const initial = loadPreferences();
    this.filter = initial.filter;
    this.viewLevel = initial.viewLevel;
    this.columnVisibility = initial.columnVisibility ?? {};
    this.columnWidths = initial.columnWidths ?? {};
    this.#detailPanelWidth = initial.detailPanelWidth;
    this.detailPanelPosition = initial.detailPanelPosition ?? DEFAULT_DETAIL_PANEL_POSITION;
    this.#detailPanelHeight = initial.detailPanelHeight;
    this.rowDensity = initial.rowDensity ?? "compact";
    this.theme = initial.theme ?? DEFAULT_THEME;

    // Auto-save when filter, viewLevel, or columnVisibility change.
    // columnWidths and detailPanelWidth are excluded (untracked) — use flush*() methods instead.
    // $effect requires component context; gracefully skip if instantiated outside one (e.g. tests).
    try {
      let initialized = false;
      $effect(() => {
        // Touch reactive fields to subscribe to them
        this.filter;
        this.viewLevel;
        this.columnVisibility;
        this.rowDensity;
        this.theme;
        this.detailPanelPosition;
        // Skip the initial save that fires on construction (we just loaded these values)
        if (!initialized) {
          initialized = true;
          return;
        }
        // Save but don't track columnWidths (saves happen via flushColumnWidths)
        untrack(() => this.save());
      });
    } catch {
      // Outside component context — auto-save is not wired (callers use save() explicitly)
    }
  }

  setDetailPanelWidth(width: number): void {
    if (!isFinite(width) || width <= 0) return;
    this.#detailPanelWidth = Math.max(MIN_DETAIL_PANEL_WIDTH, width);
  }

  setDetailPanelHeight(height: number): void {
    if (!isFinite(height) || height <= 0) return;
    this.#detailPanelHeight = Math.max(MIN_DETAIL_PANEL_HEIGHT, height);
  }

  setColumnWidth(key: ColumnKey, width: number): void {
    this.columnWidths = {
      ...this.columnWidths,
      [this.viewLevel]: {
        ...(this.columnWidths[this.viewLevel] ?? {}),
        [key]: width,
      },
    };
  }

  flushColumnWidths(): void {
    this.save();
  }

  flushDetailPanelWidth(): void {
    this.save();
  }

  flushDetailPanelHeight(): void {
    this.save();
  }

  save(): void {
    savePreferences({
      filter: this.filter,
      viewLevel: this.viewLevel,
      columnVisibility: this.columnVisibility,
      columnWidths: this.columnWidths,
      detailPanelWidth: this.#detailPanelWidth,
      detailPanelPosition: this.detailPanelPosition,
      detailPanelHeight: this.#detailPanelHeight,
      rowDensity: this.rowDensity,
      theme: this.theme,
    });
  }
}
