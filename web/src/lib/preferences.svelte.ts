import { untrack } from "svelte";
import { loadPreferences, savePreferences } from "./storage";
import { parseQuery, serializeQuery } from "./query";
import { PerViewColumnMap } from "./perViewColumnMap.svelte";
import type { SaveMode } from "./perViewColumnMap.svelte";
import { ALL_COLUMN_KEYS, DEFAULT_VISIBLE_COLUMNS, DEFAULT_COLUMN_WIDTHS, DEFAULT_DETAIL_PANEL_WIDTH, MIN_DETAIL_PANEL_WIDTH, DEFAULT_DETAIL_PANEL_HEIGHT, MIN_DETAIL_PANEL_HEIGHT, DEFAULT_DETAIL_PANEL_POSITION, DEFAULT_OPEN_DETAIL_ON, DEFAULT_BLOCKED_EMPHASIS, DEFAULT_FONT_SIZE, DEFAULT_THEME, DEFAULT_PREVIEW_OPEN, DEFAULT_VIEW_LEVEL } from "./types";
import type { NibFilter, ViewLevel, ColumnKey, RowDensity, Theme, DetailPanelPosition, OpenDetailGesture, BlockedEmphasis, FontSize, TableSort } from "./types";

export class Preferences {
  // The structured filter and its invalid-token sidecar. Together they ARE the
  // query: `query` (below) serializes them to the canonical string that is the
  // persisted + shared unit; `setQuery` reconstructs them from such a string.
  filter: NibFilter = $state({});
  invalidTokens: string[] = $state([]);
  viewLevel: ViewLevel = $state(DEFAULT_VIEW_LEVEL);

  // Per-view column state, unified behind one primitive. Each concern stays a
  // separate reactive slice with its own serialized field; the only differences
  // are the default, resolve combinator, and save timing (injected here).
  //   - visibility REPLACES the default (stored value used whole); auto-saved.
  //   - widths MERGE over the full default; flush-saved (excluded from auto-save
  //     so a drag never persists mid-gesture — persisted on pointerup instead).
  //   - order REPLACES the default (stored value used whole); auto-saved. The
  //     stored value is already the full canonical set (parseColumnOrder appends
  //     any missing key on load), so a permutation persists intact.
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
  readonly order = new PerViewColumnMap<ColumnKey[]>({
    storageKey: "columnOrder",
    defaultValue: [...ALL_COLUMN_KEYS],
    resolve: (stored, dflt) => stored ?? [...dflt],
    saveMode: "auto",
    requestSave: () => this.save(),
  });
  // The auto-save $effect subscribes only to the "auto" instances; iterating one
  // list keeps the save-timing split driven by a single explicit flag. Typed to
  // the members the effect touches so the differing T/R generics can share a list.
  readonly #perViewMaps: readonly { readonly saveMode: SaveMode; track(): void }[] = [
    this.visibility,
    this.widths,
    this.order,
  ];

  #detailPanelWidth: number | undefined = $state(undefined);
  // Discrete toggle → auto-saved (like theme/rowDensity).
  detailPanelPosition: DetailPanelPosition = $state(DEFAULT_DETAIL_PANEL_POSITION);
  // Which row gesture opens the detail panel. Discrete toggle → auto-saved.
  openDetailOn: OpenDetailGesture = $state(DEFAULT_OPEN_DETAIL_ON);
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

  // The canonical query STRING — the persisted (localStorage) + shared (`?q=`)
  // representation of the filter, derived from the structured filter + invalid
  // sidecar. `serializeQuery(parseQuery(s)) === s` for any canonical `s`, so this
  // round-trips through storage/URL and back into `filter`/`invalidTokens`.
  query: string = $derived(serializeQuery({ filter: this.filter, invalidTokens: this.invalidTokens }));

  visibleColumns: ColumnKey[] = $derived(this.visibility.resolve(this.viewLevel));

  currentColumnWidths: Record<ColumnKey, number> = $derived(this.widths.resolve(this.viewLevel));

  // The full canonical column order for the current view (all keys), used as the
  // render order (filtered to the visible set downstream). Reordering writes
  // through `order.setLevel`.
  currentColumnOrder: ColumnKey[] = $derived(this.order.resolve(this.viewLevel));

  // Serialized per-view maps, exposed for save() and read-only consumers (tests,
  // the Toolbar visibility toggle writes through `visibility` directly).
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
        // Invalid tokens are part of the persisted query, and a token can change
        // WITHOUT touching `filter` (e.g. typing `status:banana` while the filter
        // is empty), so subscribe to them independently.
        this.invalidTokens;
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
        this.openDetailOn;
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

  /** Replace the filter + invalid sidecar from a canonical query string (as
   *  loaded from localStorage or a shared `?q=` link). Inverse of `query`. */
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
      theme: this.theme,
      previewOpen: this.previewOpen,
      tableSort: this.tableSort ?? undefined,
    });
  }
}
