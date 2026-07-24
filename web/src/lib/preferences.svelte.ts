import { untrack } from "svelte";
import { loadPreferences, savePreferences } from "./storage";
import { PerViewColumnMap } from "./perViewColumnMap.svelte";
import type { SaveMode } from "./perViewColumnMap.svelte";
import { DEFAULT_VISIBLE_COLUMNS, DEFAULT_COLUMN_WIDTHS, DEFAULT_DETAIL_PANEL_WIDTH, MIN_DETAIL_PANEL_WIDTH, DEFAULT_DETAIL_PANEL_HEIGHT, MIN_DETAIL_PANEL_HEIGHT, DEFAULT_DETAIL_PANEL_POSITION, DEFAULT_BLOCKED_EMPHASIS, DEFAULT_FONT_SIZE, DEFAULT_THEME, DEFAULT_PREVIEW_OPEN } from "./types";
import type { NibFilter, ViewLevel, ColumnKey, RowDensity, Theme, DetailPanelPosition, BlockedEmphasis, FontSize, TableSort } from "./types";

export class Preferences {
  filter: NibFilter = $state({});
  viewLevel: ViewLevel = $state("none");

  // Per-view column state, unified behind one primitive. Each concern stays a
  // separate reactive slice with its own serialized field; the only differences
  // are the default, resolve combinator, and save timing (injected here).
  //   - visibility REPLACES the default (stored value used whole); auto-saved.
  //   - widths MERGE over the full default; flush-saved (excluded from auto-save
  //     so a drag never persists mid-gesture — persisted on pointerup instead).
  // nibs-46c1 adds a third `order` instance (storageKey "columnOrder") here.
  readonly visibility = new PerViewColumnMap<ColumnKey[]>({
    storageKey: "columnVisibility",
    defaultValue: [...DEFAULT_VISIBLE_COLUMNS],
    resolve: (stored, dflt) => stored ?? [...dflt],
    saveMode: "auto",
    requestSave: () => this.save(),
  });
  readonly widths = new PerViewColumnMap<Partial<Record<ColumnKey, number>>, Record<ColumnKey, number>>({
    storageKey: "columnWidths",
    defaultValue: { ...DEFAULT_COLUMN_WIDTHS },
    resolve: (stored, dflt) => ({ ...dflt, ...(stored ?? {}) }),
    saveMode: "flush",
    requestSave: () => this.save(),
  });
  // The auto-save $effect subscribes only to the "auto" instances; iterating one
  // list keeps the save-timing split driven by a single explicit flag. Typed to
  // the members the effect touches so the differing T/R generics can share a list.
  readonly #perViewMaps: readonly { readonly saveMode: SaveMode; track(): void }[] = [
    this.visibility,
    this.widths,
  ];

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
  // Table column sort. null = off (manual `order`). Discrete toggle →
  // auto-saved. Applied in every view (flat list in Flat, sibling-sort elsewhere).
  tableSort: TableSort | null = $state(null);

  visibleColumns: ColumnKey[] = $derived(this.visibility.resolve(this.viewLevel));

  currentColumnWidths: Record<ColumnKey, number> = $derived(this.widths.resolve(this.viewLevel));

  // Serialized per-view maps, exposed for save() and read-only consumers (tests,
  // the Toolbar visibility toggle writes through `visibility` directly).
  get columnVisibility(): Partial<Record<ViewLevel, ColumnKey[]>> {
    return this.visibility.serialize();
  }

  get columnWidths(): Partial<Record<ViewLevel, Partial<Record<ColumnKey, number>>>> {
    return this.widths.serialize();
  }

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
    this.visibility.hydrate(initial.columnVisibility);
    this.widths.hydrate(initial.columnWidths);
    this.#detailPanelWidth = initial.detailPanelWidth;
    this.detailPanelPosition = initial.detailPanelPosition ?? DEFAULT_DETAIL_PANEL_POSITION;
    this.#detailPanelHeight = initial.detailPanelHeight;
    this.rowDensity = initial.rowDensity ?? "compact";
    this.fontSize = initial.fontSize ?? DEFAULT_FONT_SIZE;
    this.blockedEmphasis = initial.blockedEmphasis ?? DEFAULT_BLOCKED_EMPHASIS;
    this.theme = initial.theme ?? DEFAULT_THEME;
    this.previewOpen = initial.previewOpen ?? DEFAULT_PREVIEW_OPEN;
    this.tableSort = initial.tableSort ?? null;

    // Auto-save when filter, viewLevel, or an "auto" per-view map (columnVisibility)
    // change. The "flush" per-view maps (columnWidths) and detailPanelWidth are
    // excluded (never subscribed here) — use flush*() methods instead.
    // $effect requires component context; gracefully skip if instantiated outside one (e.g. tests).
    try {
      let initialized = false;
      $effect(() => {
        // Touch reactive fields to subscribe to them
        this.filter;
        this.viewLevel;
        // Subscribe only the "auto" per-view maps; "flush" maps stay untracked
        // so their mutations (width drags) don't trigger auto-save.
        for (const map of this.#perViewMaps) {
          if (map.saveMode === "auto") map.track();
        }
        this.rowDensity;
        this.fontSize;
        this.blockedEmphasis;
        this.theme;
        this.detailPanelPosition;
        this.previewOpen;
        this.tableSort;
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
    this.widths.updateLevel(this.viewLevel, (current) => ({ ...(current ?? {}), [key]: width }));
  }

  flushColumnWidths(): void {
    this.widths.flush();
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
      columnVisibility: this.visibility.serialize(),
      columnWidths: this.widths.serialize(),
      detailPanelWidth: this.#detailPanelWidth,
      detailPanelPosition: this.detailPanelPosition,
      detailPanelHeight: this.#detailPanelHeight,
      rowDensity: this.rowDensity,
      fontSize: this.fontSize,
      blockedEmphasis: this.blockedEmphasis,
      theme: this.theme,
      previewOpen: this.previewOpen,
      tableSort: this.tableSort ?? undefined,
    });
  }
}
