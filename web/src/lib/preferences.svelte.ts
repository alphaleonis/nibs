import { untrack } from "svelte";
import { loadPreferences, savePreferences } from "./storage";
import { parseQuery, serializeQuery } from "./query";
import { persistedPerViewMap } from "./perViewMap.svelte";
import type { PerViewPersistence } from "./perViewMap.svelte";
import { ALL_COLUMN_KEYS, DEFAULT_VISIBLE_COLUMNS, DEFAULT_COLUMN_WIDTHS, DEFAULT_DETAIL_PANEL_WIDTH, MIN_DETAIL_PANEL_WIDTH, DEFAULT_DETAIL_PANEL_HEIGHT, MIN_DETAIL_PANEL_HEIGHT, DEFAULT_DETAIL_PANEL_POSITION, DEFAULT_OPEN_DETAIL_ON, DEFAULT_BLOCKED_EMPHASIS, DEFAULT_REGION_BAND_MODE, DEFAULT_FONT_SIZE, DEFAULT_THEME, DEFAULT_PREVIEW_OPEN, DEFAULT_VIEW_LEVEL } from "./types";
import type { NibFilter, ViewLevel, ColumnKey, RowDensity, Theme, DetailPanelPosition, OpenDetailGesture, BlockedEmphasis, RegionBandMode, FontSize, TableSort } from "./types";

export class Preferences {
  // Together `filter` and `invalidTokens` are the query: `query` serializes them
  // and `setQuery` parses into them.
  filter: NibFilter = $state({});
  invalidTokens: string[] = $state([]);
  viewLevel: ViewLevel = $state(DEFAULT_VIEW_LEVEL);

  // Per-view column state. Visibility and order replace the default and auto-save.
  // Widths merge over the default and are not tracked by auto-save, so a drag does
  // not persist mid-gesture; flush() saves them.
  readonly visibility = persistedPerViewMap<ColumnKey[]>({
    defaultValue: [...DEFAULT_VISIBLE_COLUMNS],
    resolve: (stored, dflt) => stored ?? [...dflt],
    persistence: { storageKey: "columnVisibility", saveMode: "auto", requestSave: () => this.save() },
  });
  readonly widths = persistedPerViewMap<Partial<Record<ColumnKey, number>>, Record<ColumnKey, number>>({
    defaultValue: { ...DEFAULT_COLUMN_WIDTHS },
    resolve: (stored, dflt) => ({ ...dflt, ...(stored ?? {}) }),
    persistence: { storageKey: "columnWidths", saveMode: "flush", requestSave: () => this.save() },
  });
  readonly order = persistedPerViewMap<ColumnKey[]>({
    defaultValue: [...ALL_COLUMN_KEYS],
    resolve: (stored, dflt) => stored ?? [...dflt],
    persistence: { storageKey: "columnOrder", saveMode: "auto", requestSave: () => this.save() },
  });
  // The auto-save effect tracks the "auto" entries. `persistence` is required in
  // this type so a map with nothing to save cannot be enrolled.
  readonly #perViewMaps: readonly { readonly persistence: PerViewPersistence; track(): void }[] = [
    this.visibility,
    this.widths,
    this.order,
  ];

  #detailPanelWidth: number | undefined = $state(undefined);
  detailPanelPosition: DetailPanelPosition = $state(DEFAULT_DETAIL_PANEL_POSITION);
  openDetailOn: OpenDetailGesture = $state(DEFAULT_OPEN_DETAIL_ON);
  #detailPanelHeight: number | undefined = $state(undefined);
  rowDensity: RowDensity = $state("compact");
  fontSize: FontSize = $state(DEFAULT_FONT_SIZE);
  blockedEmphasis: BlockedEmphasis = $state(DEFAULT_BLOCKED_EMPHASIS);
  regionBands: RegionBandMode = $state(DEFAULT_REGION_BAND_MODE);
  theme: Theme = $state(DEFAULT_THEME);
  previewOpen: boolean = $state(DEFAULT_PREVIEW_OPEN);
  // null = off (manual order).
  tableSort: TableSort | null = $state(null);

  // The canonical query string, persisted and shared as `?q=`. setQuery(query)
  // restores `filter` and `invalidTokens`.
  query: string = $derived(serializeQuery({ filter: this.filter, invalidTokens: this.invalidTokens }));

  visibleColumns: ColumnKey[] = $derived(this.visibility.resolve(this.viewLevel));

  currentColumnWidths: Record<ColumnKey, number> = $derived(this.widths.resolve(this.viewLevel));

  // Every column key for the current view; render it filtered to visibleColumns.
  currentColumnOrder: ColumnKey[] = $derived(this.order.resolve(this.viewLevel));

  get columnVisibility(): Partial<Record<ViewLevel, ColumnKey[]>> {
    return this.visibility.serialize();
  }

  get columnWidths(): Partial<Record<ViewLevel, Partial<Record<ColumnKey, number>>>> {
    return this.widths.serialize();
  }

  get columnOrder(): Partial<Record<ViewLevel, ColumnKey[]>> {
    return this.order.serialize();
  }

  detailPanelWidth: number = $derived(
    this.#detailPanelWidth ?? DEFAULT_DETAIL_PANEL_WIDTH
  );

  detailPanelHeight: number = $derived(
    this.#detailPanelHeight ?? DEFAULT_DETAIL_PANEL_HEIGHT
  );

  /** Persisted sizes, `undefined` until the user resizes. The pane layout opens
   *  an unset pane at a percent of the container instead of a fixed px. */
  get detailPanelWidthRaw(): number | undefined {
    return this.#detailPanelWidth;
  }

  get detailPanelHeightRaw(): number | undefined {
    return this.#detailPanelHeight;
  }

  constructor() {
    const initial = loadPreferences();
    this.setQuery(initial.query);
    this.viewLevel = initial.viewLevel;
    this.visibility.hydrate(initial.columnVisibility);
    this.widths.hydrate(initial.columnWidths);
    this.order.hydrate(initial.columnOrder);
    this.#detailPanelWidth = initial.detailPanelWidth;
    this.detailPanelPosition = initial.detailPanelPosition ?? DEFAULT_DETAIL_PANEL_POSITION;
    this.openDetailOn = initial.openDetailOn ?? DEFAULT_OPEN_DETAIL_ON;
    this.#detailPanelHeight = initial.detailPanelHeight;
    this.rowDensity = initial.rowDensity ?? "compact";
    this.fontSize = initial.fontSize ?? DEFAULT_FONT_SIZE;
    this.blockedEmphasis = initial.blockedEmphasis ?? DEFAULT_BLOCKED_EMPHASIS;
    this.regionBands = initial.regionBands ?? DEFAULT_REGION_BAND_MODE;
    this.theme = initial.theme ?? DEFAULT_THEME;
    this.previewOpen = initial.previewOpen ?? DEFAULT_PREVIEW_OPEN;
    this.tableSort = initial.tableSort ?? null;

    // Save on a change to any persisted field except column widths and the detail
    // panel size, whose changes save through the flush methods. $effect throws
    // outside a component context (e.g. tests); callers there use save() explicitly.
    try {
      let initialized = false;
      $effect(() => {
        this.filter;
        // Tracked separately: an invalid token can change without touching `filter`.
        this.invalidTokens;
        this.viewLevel;
        for (const map of this.#perViewMaps) {
          if (map.persistence.saveMode === "auto") map.track();
        }
        this.rowDensity;
        this.fontSize;
        this.blockedEmphasis;
        this.regionBands;
        this.theme;
        this.detailPanelPosition;
        this.openDetailOn;
        this.previewOpen;
        this.tableSort;
        // Skip the run on construction; these values were just loaded.
        if (!initialized) {
          initialized = true;
          return;
        }
        // untrack so save()'s reads of the flush-saved fields do not subscribe.
        untrack(() => this.save());
      });
    } catch {
      // Outside a component context.
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

  /** Replace `filter` and `invalidTokens` from a query string. Inverse of `query`. */
  setQuery(q: string): void {
    const parsed = parseQuery(q);
    this.filter = parsed.filter;
    this.invalidTokens = parsed.invalidTokens;
  }

  save(): void {
    savePreferences({
      query: this.query,
      viewLevel: this.viewLevel,
      columnVisibility: this.visibility.serialize(),
      columnWidths: this.widths.serialize(),
      columnOrder: this.order.serialize(),
      detailPanelWidth: this.#detailPanelWidth,
      detailPanelPosition: this.detailPanelPosition,
      openDetailOn: this.openDetailOn,
      detailPanelHeight: this.#detailPanelHeight,
      rowDensity: this.rowDensity,
      fontSize: this.fontSize,
      blockedEmphasis: this.blockedEmphasis,
      regionBands: this.regionBands,
      theme: this.theme,
      previewOpen: this.previewOpen,
      tableSort: this.tableSort ?? undefined,
    });
  }
}
