import { untrack } from "svelte";
import { loadPreferences, savePreferences } from "./storage";
import { DEFAULT_VISIBLE_COLUMNS, DEFAULT_COLUMN_WIDTHS, DEFAULT_DETAIL_PANEL_WIDTH, MIN_DETAIL_PANEL_WIDTH, DEFAULT_DETAIL_PANEL_HEIGHT, MIN_DETAIL_PANEL_HEIGHT, DEFAULT_DETAIL_PANEL_POSITION, DEFAULT_BLOCKED_EMPHASIS, DEFAULT_FONT_SIZE, DEFAULT_THEME, DEFAULT_PREVIEW_OPEN } from "./types";
import type { NibFilter, ViewLevel, ColumnKey, RowDensity, Theme, DetailPanelPosition, BlockedEmphasis, FontSize } from "./types";

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
  // Discrete toggle → auto-saved (like theme/rowDensity). Scales the UI type
  // scale via --font-scale; decoupled from rowDensity (spacing).
  fontSize: FontSize = $state(DEFAULT_FONT_SIZE);
  blockedEmphasis: BlockedEmphasis = $state(DEFAULT_BLOCKED_EMPHASIS);
  theme: Theme = $state(DEFAULT_THEME);
  // Discrete toggle → auto-saved (like theme/rowDensity/detailPanelPosition).
  previewOpen: boolean = $state(DEFAULT_PREVIEW_OPEN);

  visibleColumns: ColumnKey[] = $derived(
    this.columnVisibility[this.viewLevel] ?? [...DEFAULT_VISIBLE_COLUMNS]
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

  /** Raw persisted sizes — `undefined` until the user resizes. The pane layout
   *  uses these to tell a user-set size from the default: unset opens at a
   *  percent of the container instead of a fixed px. */
  get detailPanelWidthRaw(): number | undefined {
    return this.#detailPanelWidth;
  }

  get detailPanelHeightRaw(): number | undefined {
    return this.#detailPanelHeight;
  }

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
    this.fontSize = initial.fontSize ?? DEFAULT_FONT_SIZE;
    this.blockedEmphasis = initial.blockedEmphasis ?? DEFAULT_BLOCKED_EMPHASIS;
    this.theme = initial.theme ?? DEFAULT_THEME;
    this.previewOpen = initial.previewOpen ?? DEFAULT_PREVIEW_OPEN;

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
        this.fontSize;
        this.blockedEmphasis;
        this.theme;
        this.detailPanelPosition;
        this.previewOpen;
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
      fontSize: this.fontSize,
      blockedEmphasis: this.blockedEmphasis,
      theme: this.theme,
      previewOpen: this.previewOpen,
    });
  }
}
