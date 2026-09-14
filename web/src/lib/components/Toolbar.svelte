<script lang="ts">
  import { VIEW_LEVELS, VIEW_LEVEL_LABELS, DEFAULT_THEME, DEFAULT_DETAIL_PANEL_POSITION, DEFAULT_OPEN_DETAIL_ON, DEFAULT_BLOCKED_EMPHASIS, DEFAULT_REGION_BAND_MODE, DEFAULT_FONT_SIZE } from "../types";
  import type { NibFilter, ViewLevel, RowDensity, FontSize, Theme, DetailPanelPosition, OpenDetailGesture, BlockedEmphasis, RegionBandMode } from "../types";
  import { ALL_COLUMN_KEYS, COLUMNS } from "../columns";
  import type { ColumnKey } from "../columns";
  import type { Preferences } from "../preferences.svelte";
  import { TYPES, STATUSES, PRIORITIES, ESTIMATES, ESTIMATE_LABELS, OPEN_STATUSES } from "../constants";
  import {
    Plus,
    ChevronDown,
    X,
    Columns3,
    Eye,
    ListTree,
    List,
    ListFilter,
    LayoutGrid,
    TriangleAlert,
  } from "@lucide/svelte";
  import { typeIcons } from "../icons";
  import { priorityIndicators } from "../badges";
  import type { TypeIconInfo } from "../icons";
  import { resolveFilter, resolveViewLevel, resolveVisibleColumns, resolveColumnOrder, emitFilter as emitFilterHelper, switchViewLevel } from "../resolvePrefs";
  import type { TreeViewState } from "../treeView.svelte";
  import { parseQuery, serializeQuery, getCompletion, tokenGroups, tokenSegments, relTokenValueContext, AREA_FIELD, isRefusedArea } from "../query";
  import type { Completion, QueryFilter, SpanKind, RelValueContext, NibSuggestion } from "../query";
  import type { AreaVocabulary } from "../areas";
  import { createNibSearch, type SearchNibsFn } from "../searchNibs";
  import { getContextClient } from "@urql/svelte";
  import { untrack, tick } from "svelte";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu/index.js";
  import { buttonVariants } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import NibsLogo from "./NibsLogo.svelte";
  import StatusIcon from "./StatusIcon.svelte";
  import TypeIcon from "./TypeIcon.svelte";
  import SuggestionList from "./SuggestionList.svelte";
  import QueryHelp from "./QueryHelp.svelte";
  import SettingsSheet from "./SettingsSheet.svelte";
  import TooltipButton from "./TooltipButton.svelte";
  import WithTooltip from "./WithTooltip.svelte";
  import ConnectionStatus from "./ConnectionStatus.svelte";
  import type { ConnectionStatus as ConnectionStatusValue } from "../connectionRecovery";

  let {
    prefs = undefined as Preferences | undefined,
    filter = undefined as NibFilter | undefined,
    onchange = undefined as ((filter: NibFilter) => void) | undefined,
    viewLevel = undefined as ViewLevel | undefined,
    onviewlevelchange = undefined as ((level: ViewLevel) => void) | undefined,
    treeView = undefined as TreeViewState | undefined,
    visibleColumns = undefined as ColumnKey[] | undefined,
    oncolumnschange = undefined as ((columns: ColumnKey[]) => void) | undefined,
    columnOrder = undefined as ColumnKey[] | undefined,
    oncreatenew = undefined as ((type: string) => void) | undefined,
    connectionStatus = "connected" as ConnectionStatusValue,
    rowDensity = "compact" as RowDensity,
    ondensitychange = undefined as ((density: RowDensity) => void) | undefined,
    fontSize = DEFAULT_FONT_SIZE as FontSize,
    onfontsizechange = undefined as ((fontSize: FontSize) => void) | undefined,
    blockedEmphasis = DEFAULT_BLOCKED_EMPHASIS as BlockedEmphasis,
    onemphasischange = undefined as ((emphasis: BlockedEmphasis) => void) | undefined,
    theme = DEFAULT_THEME as Theme,
    onthemechange = undefined as ((theme: Theme) => void) | undefined,
    detailPanelPosition = undefined as DetailPanelPosition | undefined,
    onpositionchange = undefined as ((p: DetailPanelPosition) => void) | undefined,
    openDetailOn = undefined as OpenDetailGesture | undefined,
    onopendetailchange = undefined as ((g: OpenDetailGesture) => void) | undefined,
    availableTags = [],
    areas = undefined,
    projectName = "",
    searchNibs = undefined,
  }: {
    prefs?: Preferences;
    filter?: NibFilter;
    onchange?: (filter: NibFilter) => void;
    viewLevel?: ViewLevel;
    onviewlevelchange?: (level: ViewLevel) => void;
    /** A prop, not context: `useTreeView()` throws without a provider, and tests
     *  render the toolbar standalone. Absent, the view switches unreconciled. */
    treeView?: TreeViewState;
    visibleColumns?: ColumnKey[];
    oncolumnschange?: (columns: ColumnKey[]) => void;
    columnOrder?: ColumnKey[];
    oncreatenew?: (type: string) => void;
    /** Live-socket state; drives the disconnected chip beside the project name. */
    connectionStatus?: ConnectionStatusValue;
    rowDensity?: RowDensity;
    ondensitychange?: (density: RowDensity) => void;
    fontSize?: FontSize;
    onfontsizechange?: (fontSize: FontSize) => void;
    blockedEmphasis?: BlockedEmphasis;
    onemphasischange?: (emphasis: BlockedEmphasis) => void;
    theme?: Theme;
    onthemechange?: (theme: Theme) => void;
    detailPanelPosition?: DetailPanelPosition;
    onpositionchange?: (p: DetailPanelPosition) => void;
    openDetailOn?: OpenDetailGesture;
    onopendetailchange?: (g: OpenDetailGesture) => void;
    availableTags?: string[];
    /** For `area:` completion and validity; a prop for the same reason as
     *  `treeView`. Absent, nothing is completed or flagged. */
    areas?: AreaVocabulary;
    projectName?: string;
    searchNibs?: SearchNibsFn;
  } = $props();

  // Relationship-id typeahead. Tests inject `searchNibs` with no urql provider,
  // so a missing context client is tolerated. Captured once, untracked.
  const injectedSearch = untrack(() => searchNibs);
  let contextSearchNibs: SearchNibsFn | null = null;
  if (!injectedSearch) {
    try {
      contextSearchNibs = createNibSearch(getContextClient());
    } catch {
      contextSearchNibs = null;
    }
  }
  const effectiveSearchNibs: SearchNibsFn = injectedSearch ?? contextSearchNibs ?? (async () => []);

  let resolvedDensity = $derived(prefs ? prefs.rowDensity : rowDensity);

  function handleSetDensity(density: RowDensity) {
    if (prefs) {
      prefs.rowDensity = density;
    } else {
      ondensitychange?.(density);
    }
  }

  let resolvedFontSize = $derived(prefs ? prefs.fontSize : fontSize);

  function handleSetFontSize(fs: FontSize) {
    if (prefs) {
      prefs.fontSize = fs;
    } else {
      onfontsizechange?.(fs);
    }
  }

  let resolvedBlockedEmphasis = $derived(prefs ? prefs.blockedEmphasis : blockedEmphasis);

  function handleSetBlockedEmphasis(emphasis: BlockedEmphasis) {
    if (prefs) {
      prefs.blockedEmphasis = emphasis;
    } else {
      onemphasischange?.(emphasis);
    }
  }

  let resolvedRegionBands = $derived(prefs ? prefs.regionBands : DEFAULT_REGION_BAND_MODE);

  function handleSetRegionBands(mode: RegionBandMode) {
    if (prefs) prefs.regionBands = mode;
  }

  let resolvedTheme = $derived(prefs ? prefs.theme : theme);

  function handleSetTheme(t: Theme) {
    if (prefs) {
      prefs.theme = t;
    } else {
      onthemechange?.(t);
    }
  }

  let resolvedPosition = $derived(prefs ? prefs.detailPanelPosition : (detailPanelPosition ?? DEFAULT_DETAIL_PANEL_POSITION));

  function handleSetPosition(p: DetailPanelPosition) {
    if (prefs) {
      prefs.detailPanelPosition = p;
    } else {
      onpositionchange?.(p);
    }
  }

  let resolvedOpenDetailOn = $derived(prefs ? prefs.openDetailOn : (openDetailOn ?? DEFAULT_OPEN_DETAIL_ON));

  function handleSetOpenDetailOn(g: OpenDetailGesture) {
    if (prefs) {
      prefs.openDetailOn = g;
    } else {
      onopendetailchange?.(g);
    }
  }

  const VIEW_LEVEL_ICON_INFO: Record<ViewLevel, TypeIconInfo> = {
    none: { icon: ListTree, color: "var(--muted-foreground)" },
    flat: { icon: List, color: "var(--muted-foreground)" },
    milestones: typeIcons.milestone,
    epics: typeIcons.epic,
    features: typeIcons.feature,
    // An area is config vocabulary, not a nib type: neutral color.
    areas: { icon: LayoutGrid, color: "var(--muted-foreground)" },
  };

  // Resolve values: prefs takes precedence over individual props
  let resolvedFilter = $derived(resolveFilter(prefs, filter));
  let resolvedViewLevel = $derived(resolveViewLevel(prefs, viewLevel));
  let resolvedVisibleColumns = $derived(resolveVisibleColumns(prefs, visibleColumns));
  let resolvedColumnOrder = $derived(resolveColumnOrder(prefs, columnOrder));
  let ViewLevelIcon = $derived(VIEW_LEVEL_ICON_INFO[resolvedViewLevel].icon);

  // Shared by aria-label and tooltip so the two cannot drift.
  const newItemLabel = "New item";
  const viewLabel = "View";
  const columnsLabel = "Columns";
  const clearKeywordLabel = "Clear keyword";
  // Tooltip text only, never an accessible name (its layer is aria-hidden). A
  // const because the token markup is whitespace-sensitive.
  const tokenHint = "Click to select · Delete to remove";

  let addMenuOpen = $state(false);
  let viewLevelOpen = $state(false);
  let columnsOpen = $state(false);
  let viewLevelIconInfo = $derived(VIEW_LEVEL_ICON_INFO[resolvedViewLevel]);

  let columnOptions = $derived(ALL_COLUMN_KEYS.map((key) => COLUMNS[key]));

  function emitFilter(updated: NibFilter) {
    emitFilterHelper(prefs, onchange, updated);
  }

  function handleSelectViewLevel(level: ViewLevel) {
    // Through switchViewLevel, not the preference, so the table reconciles the
    // selection for the new lens.
    switchViewLevel(prefs, onviewlevelchange, treeView, resolvedViewLevel, level);
    viewLevelOpen = false;
  }

  function handleColumnToggle(key: ColumnKey, checked: boolean) {
    let updated: ColumnKey[];
    if (checked) {
      updated = [...resolvedVisibleColumns, key];
    } else {
      updated = resolvedVisibleColumns.filter(k => k !== key);
    }
    // Per-view order, so a re-shown column returns to its chosen position.
    updated.sort((a, b) => resolvedColumnOrder.indexOf(a) - resolvedColumnOrder.indexOf(b));
    if (prefs) {
      prefs.visibility.setLevel(prefs.viewLevel, updated);
    } else {
      oncolumnschange?.(updated);
    }
  }

  // --- Filter bar logic ---
  type FilterField = "type" | "priority" | "status" | "estimate" | "tags";
  type DropdownId = "type" | "priority" | "status" | "estimate" | "tags";

  interface DropdownConfig {
    id: DropdownId;
    label: string;
    field: FilterField;
    values: readonly string[];
  }

  // Include/exclude pairs over static values. `area` has no dropdown: it is a
  // single path in a runtime tree, with no `excludeArea`.
  let dropdowns = $derived<DropdownConfig[]>([
    { id: "type", label: "Type", field: "type", values: TYPES },
    { id: "priority", label: "Priority", field: "priority", values: PRIORITIES },
    { id: "status", label: "Status", field: "status", values: STATUSES },
    { id: "estimate", label: "Estimate", field: "estimate", values: ESTIMATES },
    ...(availableTags.length > 0 ? [{ id: "tags" as DropdownId, label: "Tags", field: "tags" as FilterField, values: availableTags }] : []),
  ]);

  let filterOpenStates = $state<Record<DropdownId, boolean>>({
    type: false,
    priority: false,
    status: false,
    estimate: false,
    tags: false,
  });

  function handleFilterOpenChange(id: DropdownId, open: boolean) {
    if (open) {
      for (const key of Object.keys(filterOpenStates) as DropdownId[]) {
        if (key !== id) filterOpenStates[key] = false;
      }
    }
    filterOpenStates[id] = open;
  }

  // DOM ref to the keyword input so the clear button can refocus it.
  let keywordInput = $state<HTMLInputElement | null>(null);

  // --- Query box ↔ NibFilter reconciliation ---
  // Every keystroke parses into the filter. `keywordText` is the literal box text,
  // snapped to the canonical serialization only while unfocused.
  //
  // The slice of NibFilter the box owns; other fields survive box edits. A key
  // missing here is silently dropped on the next blur, so the check below
  // requires every QueryFilter key to be listed (`satisfies` checks only the
  // other direction).
  const BOX_FIELD_KEYS = [
    "type", "excludeType",
    "priority", "excludePriority",
    "status", "excludeStatus",
    "estimate", "excludeEstimate",
    "tags", "excludeTags",
    "search",
    // Relationship-id scalars.
    "parentId", "ancestorId", "descendantId", "siblingId",
    "blockingId", "blockedById", "mentionsId", "mentionedById",
    // The scheduling axis.
    "milestone",
    // The ownership axis.
    "area",
    // Existence/state booleans.
    "hasParent", "hasBlocking", "hasBlockedBy", "isBlocked", "noMilestone",
  ] as const satisfies readonly (keyof QueryFilter)[];

  type _BoxKeysCoverQueryFilter = keyof QueryFilter extends (typeof BOX_FIELD_KEYS)[number] ? true : never;
  const _boxKeysCheck: _BoxKeysCoverQueryFilter = true;
  void _boxKeysCheck;

  // QueryFilter is a Pick of NibFilter, so this copy stays fully typed.
  function assignBoxField<K extends keyof QueryFilter>(target: NibFilter, source: QueryFilter, key: K) {
    const value = source[key];
    if (value !== undefined) target[key] = value;
    else delete target[key];
  }

  // Invalid known-field tokens (e.g. `status:banana`), kept so the box can flag
  // and round-trip them. On `prefs` they persist and travel in `?q=`.
  let localInvalidTokens = $state<string[]>([]);
  let resolvedInvalidTokens = $derived(prefs ? prefs.invalidTokens : localInvalidTokens);
  function setInvalidTokens(tokens: string[]) {
    if (prefs) prefs.invalidTokens = tokens;
    else localInvalidTokens = tokens;
  }

  // The parked tokens plus an `area:` path the current vocabulary refuses.
  // `Preferences.setQuery` parses without a vocabulary, so a loaded undeclared
  // path is never parked, while `withSendableArea` withholds it from the query.
  // Display only: `canonicalQuery` reads the parked tokens alone, so the token is
  // serialized once. "unknown" (loading or failed config) is not flagged, and an
  // empty path serializes to no token.
  let flaggedTokens = $derived.by(() => {
    const area = resolvedFilter.area;
    if (typeof area !== "string" || area === "" || !isRefusedArea(area, areas)) {
      return resolvedInvalidTokens;
    }
    const token = `${AREA_FIELD}:${area}`;
    return resolvedInvalidTokens.includes(token)
      ? resolvedInvalidTokens
      : [...resolvedInvalidTokens, token];
  });

  let keywordFocused = $state(false);
  // Seeded once so first paint is right; the effect below keeps it in sync.
  let keywordText = $state(untrack(() => serializeQuery({ filter: resolvedFilter, invalidTokens: resolvedInvalidTokens })));
  let canonicalQuery = $derived(serializeQuery({ filter: resolvedFilter, invalidTokens: resolvedInvalidTokens }));
  let hasKeyword = $derived(keywordText.length > 0);

  $effect(() => {
    const next = canonicalQuery;
    if (!keywordFocused) {
      // Untracked: the box mirrors the filter, not the reverse.
      untrack(() => {
        keywordText = next;
      });
    }
  });

  // --- Syntax-highlight backdrop ---
  // A display-only colored layer behind the transparent-text input, which stays
  // the editor.
  let backdrop = $state<HTMLDivElement | null>(null);
  // Flattened, the groups' spans equal `tokenizeSpans`'s, so glyph offsets hold.
  let highlightGroups = $derived(tokenGroups(keywordText, areas));

  // --- Token-click affordances ---
  // A layer above the input with the backdrop's layout. Only token wrappers take
  // pointer events, so a click in a gap places the caret.
  let tokenLayer = $state<HTMLDivElement | null>(null);
  let tokenSegs = $derived(tokenSegments(keywordText, areas));

  // Removing a token is select, then Delete.
  function selectToken(start: number, end: number) {
    if (!keywordInput) return;
    keywordInput.focus();
    keywordInput.setSelectionRange(start, end);
  }

  // Field and operator are foreground, the value carries the accent, and free
  // text is muted so it reads as unparsed; keep structure distinct from
  // `freetext`. The accent's rationale is at `--query-value` in app.css.
  const SPAN_CLASS: Record<SpanKind, string> = {
    field: "text-foreground",
    operator: "text-foreground",
    value: "text-query-value",
    invalid: "text-destructive underline decoration-wavy",
    freetext: "text-muted-foreground",
    whitespace: "",
  };

  // The well behind a token's value run (`TokenGroup.valueRunStart`), one shape
  // through commas. Background and radius only: padding, borders or weight change
  // advance widths and drift the caret off the text.
  const VALUE_WELL = "rounded-[3px] bg-query-well";

  // Keeps the backdrop and token layer aligned with the input's horizontal scroll.
  function syncBackdropScroll() {
    if (!keywordInput) return;
    if (backdrop) backdrop.scrollLeft = keywordInput.scrollLeft;
    if (tokenLayer) tokenLayer.scrollLeft = keywordInput.scrollLeft;
  }

  // The input adjusts scrollLeft during layout, so read it on the next frame;
  // `onscroll` covers caret moves that do not change the text.
  $effect(() => {
    keywordText;
    requestAnimationFrame(syncBackdropScroll);
  });

  // Box-owned fields come from the parse or are dropped; the rest are kept.
  function emitFromText(text: string) {
    const parsed = parseQuery(text, areas);
    setInvalidTokens(parsed.invalidTokens);
    const updated: NibFilter = { ...resolvedFilter };
    for (const key of BOX_FIELD_KEYS) {
      assignBoxField(updated, parsed.filter, key);
    }
    emitFilter(updated);
  }

  function handleKeyword(event: Event) {
    const input = event.target as HTMLInputElement;
    keywordText = input.value;
    emitFromText(input.value);
    refreshCompletion();
  }

  function clearKeyword() {
    keywordText = "";
    setInvalidTokens([]);
    clearCompletion();
    const updated: NibFilter = { ...resolvedFilter };
    for (const key of BOX_FIELD_KEYS) delete updated[key];
    emitFilter(updated);
    keywordInput?.focus();
  }

  // --- Autocomplete ---
  // "static": synchronous field/enum/tag/area completion. "rel": the caret is in
  // a relationship-id value, and candidates are fetched (debounced) into `relResults`.
  type ActiveCompletion =
    | { kind: "static"; completion: Completion }
    | { kind: "rel"; ctx: RelValueContext };
  let active = $state<ActiveCompletion | null>(null);
  let suggestIndex = $state(-1);
  let suggestBlurTimer: ReturnType<typeof setTimeout> | null = null;

  const REL_SEARCH_DEBOUNCE_MS = 200;
  let relResults = $state<NibSuggestion[]>([]);
  // The fragment `relResults` answers. Rows are held across fragment changes to
  // avoid flicker, so accepting refuses rows for a stale fragment.
  let relResultsFragment = $state("");
  let relDebounceTimer: ReturnType<typeof setTimeout> | null = null;
  // Bumped per query; a response with an old seq is dropped.
  let relRequestSeq = 0;

  let activeItemCount = $derived(
    !active ? 0 : active.kind === "static" ? active.completion.items.length : relResults.length,
  );

  let relResultsAreCurrent = $derived(
    active?.kind === "rel" && relResultsFragment === active.ctx.fragment,
  );

  function cancelRelSearch() {
    if (relDebounceTimer) { clearTimeout(relDebounceTimer); relDebounceTimer = null; }
  }

  const sameItems = (a: readonly string[], b: readonly string[]) =>
    a.length === b.length && a.every((v, i) => v === b[i]);

  // `explicit` marks Ctrl+Space, the only way an empty token opens the field list.
  function refreshCompletion(explicit = false) {
    if (!keywordInput) { active = null; setRelResults([], ""); cancelRelSearch(); return; }
    const value = keywordInput.value;
    const caret = keywordInput.selectionStart ?? value.length;
    // An explicit refresh over an unchanged list keeps the highlight, or the next
    // accept would jump to row 0.
    const heldItems = explicit && active?.kind === "static" ? active.completion.items : null;
    const heldIndex = suggestIndex;
    suggestIndex = -1;

    const relCtx = relTokenValueContext(value, caret);
    if (relCtx) {
      active = { kind: "rel", ctx: relCtx };
      scheduleRelSearch(relCtx.fragment);
      return;
    }

    // Left any rel context: drop stale rich rows + any pending fetch.
    cancelRelSearch();
    setRelResults([], "");
    const c = getCompletion(value, caret, availableTags, { explicit, areas });
    active = c ? { kind: "static", completion: c } : null;
    if (heldItems && c && sameItems(heldItems, c.items)) suggestIndex = heldIndex;
  }

  // `active` is written on events, not derived, so recompute an open static list
  // when the areas vocabulary changes; a stale list would offer retired paths and
  // keep the warning chip hidden. `explicit` is safe: it matters only for an empty
  // token, whose open list came from Ctrl+Space. Untracked so only `areas` is a dependency.
  $effect(() => {
    void areas;
    untrack(() => {
      // Only a static list can go stale this way; rel rows answer the server.
      if (active?.kind === "static") refreshCompletion(true);
    });
  });

  // The rows and the fragment they answer move together — see `relResultsFragment`.
  function setRelResults(results: NibSuggestion[], fragment: string) {
    relResults = results;
    relResultsFragment = fragment;
  }

  function scheduleRelSearch(fragment: string) {
    cancelRelSearch();
    // No query until at least one character follows the colon.
    if (fragment === "") { setRelResults([], fragment); return; }
    relDebounceTimer = setTimeout(() => {
      relDebounceTimer = null;
      void runRelSearch(fragment);
    }, REL_SEARCH_DEBOUNCE_MS);
  }

  async function runRelSearch(fragment: string) {
    const seq = ++relRequestSeq;
    // A rejecting search fn degrades to no suggestions; the caller does not catch.
    let results: NibSuggestion[];
    try {
      results = await effectiveSearchNibs(fragment);
    } catch (err) {
      console.warn("rel-token search failed:", err);
      results = [];
    }
    // Drop the response if a newer query started or the caret's fragment changed.
    if (seq !== relRequestSeq) return;
    if (!active || active.kind !== "rel" || active.ctx.fragment !== fragment) return;
    setRelResults(results, fragment);
  }

  async function applyCompletion(item: string) {
    if (!active || active.kind !== "static" || !keywordInput) return;
    const { text, caret } = active.completion.apply(item);
    keywordText = text;
    emitFromText(text);
    await tick();
    keywordInput.focus();
    keywordInput.setSelectionRange(caret, caret);
    // Re-suggest for the new caret (e.g. `type:` → its enum values).
    refreshCompletion();
  }

  // Replaces the token's partial value with the chosen nib's id.
  async function applyRelSelection(nib: NibSuggestion) {
    if (!active || active.kind !== "rel" || !keywordInput) return;
    if (!relResultsAreCurrent) return;
    const { start, end } = active.ctx;
    const text = keywordText.slice(0, start) + nib.id + keywordText.slice(end);
    const caret = start + nib.id.length;
    keywordText = text;
    emitFromText(text);
    await tick();
    keywordInput.focus();
    keywordInput.setSelectionRange(caret, caret);
    refreshCompletion();
  }

  // The text accepting row `index` would produce, or null when it would not change
  // the text (an inserted value matches itself) or the rel rows are stale.
  function acceptedText(index: number): string | null {
    if (!active) return null;
    let text: string;
    if (active.kind === "static") {
      text = active.completion.apply(active.completion.items[index]).text;
    } else {
      if (!relResultsAreCurrent) return null;
      const { start, end } = active.ctx;
      text = keywordText.slice(0, start) + relResults[index].id + keywordText.slice(end);
    }
    return text === keywordText ? null : text;
  }

  function handleKeywordKeydown(event: KeyboardEvent) {
    // Before the early return: Ctrl+Space opens a completion when none is active.
    // Match the chord exactly: AltGr reports Control+Alt on Windows, and
    // AltGr+Space types a non-breaking space.
    if (event.ctrlKey && !event.altKey && !event.shiftKey && !event.metaKey
        && (event.key === " " || event.code === "Space")) {
      event.preventDefault();
      refreshCompletion(true);
      return;
    }
    if (!active || activeItemCount === 0) return;
    if (event.key === "ArrowDown") {
      event.preventDefault();
      suggestIndex = (suggestIndex + 1) % activeItemCount;
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      suggestIndex = suggestIndex <= 0 ? activeItemCount - 1 : suggestIndex - 1;
    } else if (event.key === "Enter") {
      if (suggestIndex >= 0 && suggestIndex < activeItemCount) {
        event.preventDefault();
        if (active.kind === "static") applyCompletion(active.completion.items[suggestIndex]);
        else applyRelSelection(relResults[suggestIndex]);
      }
    } else if (event.key === "Tab" && !event.shiftKey) {
      // Accept the highlighted row, or the first. Swallow Tab only when accepting
      // rewrites the text, or forward Tab could never leave the box.
      const index = suggestIndex >= 0 && suggestIndex < activeItemCount ? suggestIndex : 0;
      if (acceptedText(index) === null) return;
      event.preventDefault();
      if (active.kind === "static") applyCompletion(active.completion.items[index]);
      else applyRelSelection(relResults[index]);
    } else if (event.key === "Escape") {
      event.preventDefault();
      clearCompletion();
    }
  }

  function clearCompletion() {
    active = null;
    setRelResults([], "");
    cancelRelSearch();
    suggestIndex = -1;
  }

  function handleKeywordFocus() {
    if (suggestBlurTimer) { clearTimeout(suggestBlurTimer); suggestBlurTimer = null; }
    keywordFocused = true;
    refreshCompletion();
  }

  function handleKeywordBlur() {
    keywordFocused = false;
    // Deferred so a suggestion click after blur still lands.
    if (suggestBlurTimer) clearTimeout(suggestBlurTimer);
    suggestBlurTimer = setTimeout(() => {
      suggestBlurTimer = null;
      clearCompletion();
    }, 150);
  }

  // Cancel a pending search on unmount.
  $effect(() => () => cancelRelSearch());

  function toggleArrayValue(arr: string[] | undefined, value: string): string[] | undefined {
    if (!arr) return [value];
    if (arr.includes(value)) {
      const result = arr.filter((v) => v !== value);
      return result.length > 0 ? result : undefined;
    }
    return [...arr, value];
  }

  function handleToggle(field: FilterField, value: string) {
    const updated: NibFilter = { ...resolvedFilter };
    updated[field] = toggleArrayValue(resolvedFilter[field], value);
    if (updated[field] === undefined) {
      delete updated[field];
    }
    emitFilter(updated);
  }

  // Dropdowns write include-lists, but the box also writes excludes, so Clear and
  // the status preset drop both.
  const EXCLUDE_KEY = {
    type: "excludeType",
    priority: "excludePriority",
    status: "excludeStatus",
    estimate: "excludeEstimate",
    tags: "excludeTags",
  } as const satisfies Record<FilterField, keyof NibFilter>;

  // Replaces the status include-list rather than merging.
  function applyStatusPreset(statuses: readonly string[]) {
    const updated: NibFilter = { ...resolvedFilter, status: [...statuses] };
    delete updated.excludeStatus;
    emitFilter(updated);
    filterOpenStates.status = false;
  }

  function handleClearField(field: FilterField, id: DropdownId) {
    const updated = { ...resolvedFilter };
    delete updated[field];
    delete updated[EXCLUDE_KEY[field]];
    emitFilter(updated);
    filterOpenStates[id] = false;
  }

  function isChecked(field: FilterField, value: string): boolean {
    return resolvedFilter[field]?.includes(value) ?? false;
  }

  // The trigger badge counts TICKED boxes, so it stays include-only.
  function getCount(field: FilterField): number {
    return resolvedFilter[field]?.length ?? 0;
  }

  // Include plus exclude, so Clear enables for an exclusion the badge does not count.
  function getClearableCount(field: FilterField): number {
    return getCount(field) + (resolvedFilter[EXCLUDE_KEY[field]]?.length ?? 0);
  }

</script>

<!-- This <header> and the filter band below are both root children of App's flex
     column. Do not wrap them in a gapped container. -->
<header class="flex flex-wrap items-center justify-between gap-3 border-b border-border px-6 py-3">
  <!-- The logo carries "Nibs". Its height is in em to track font scale; above
       1.6em it makes the header taller. -->
  <h1 class="flex min-w-0 items-center gap-2.5 text-xl font-semibold">
    <NibsLogo class="h-[1.6em] w-auto shrink-0" />
    {#if projectName}
      <span aria-hidden="true" class="shrink-0 text-muted-foreground">·</span>
      <span class="min-w-0 max-w-[28ch] lg:max-w-none truncate">{projectName}</span>
    {/if}
    <ConnectionStatus status={connectionStatus} />
  </h1>

  <div class="flex shrink-0 items-center gap-1">
    <!-- New button. The trigger chains the tooltip's handlers with the menu's. -->
    <DropdownMenu.Root bind:open={addMenuOpen}>
      <WithTooltip tooltip={newItemLabel}>
        {#snippet trigger({ props })}
          <DropdownMenu.Trigger
            {...props}
            aria-label={newItemLabel}
            data-testid="toolbar-add"
            class={buttonVariants({ variant: "default", size: "default" })}
          >
            <Plus size={16} />
            New
          </DropdownMenu.Trigger>
        {/snippet}
      </WithTooltip>

      <DropdownMenu.Content align="start" class="w-40">
        {#each TYPES as nibType}
          {@const iconInfo = typeIcons[nibType]}
          {@const TypeIconComponent = iconInfo.icon}
          <DropdownMenu.Item
            data-testid="toolbar-add-{nibType}"
            class="flex items-center gap-2 text-sm"
            onclick={() => { oncreatenew?.(nibType); }}
          >
            <TypeIconComponent size={14} style="color: {iconInfo.color};" />
            {nibType}
          </DropdownMenu.Item>
        {/each}
      </DropdownMenu.Content>
    </DropdownMenu.Root>

    <!-- Separator -->
    <div class="mx-1 h-5 w-px bg-border shrink-0"></div>

    <!-- View selector -->
    <DropdownMenu.Root bind:open={viewLevelOpen}>
      <WithTooltip tooltip={viewLabel}>
        {#snippet trigger({ props })}
          <DropdownMenu.Trigger
            {...props}
            aria-label={`${viewLabel}: ${VIEW_LEVEL_LABELS[resolvedViewLevel]}`}
            class={buttonVariants({ variant: "outline", size: "default" })}
          >
            <ViewLevelIcon size={14} style="color: {viewLevelIconInfo.color};" />
            {VIEW_LEVEL_LABELS[resolvedViewLevel]}
            <ChevronDown size={14} />
          </DropdownMenu.Trigger>
        {/snippet}
      </WithTooltip>

      <DropdownMenu.Content align="end" class="w-40">
        <DropdownMenu.RadioGroup value={resolvedViewLevel} onValueChange={(v) => { if (v) handleSelectViewLevel(v as ViewLevel); }}>
          {#each VIEW_LEVELS as level}
            {@const iconInfo = VIEW_LEVEL_ICON_INFO[level]}
            {@const LevelIcon = iconInfo.icon}
            <DropdownMenu.RadioItem value={level} class="flex items-center gap-2 text-sm">
              <LevelIcon size={14} style="color: {iconInfo.color};" />
              {VIEW_LEVEL_LABELS[level]}
            </DropdownMenu.RadioItem>
          {/each}
        </DropdownMenu.RadioGroup>
      </DropdownMenu.Content>
    </DropdownMenu.Root>

    <!-- Columns dropdown -->
    <DropdownMenu.Root bind:open={columnsOpen}>
      <WithTooltip tooltip={columnsLabel}>
        {#snippet trigger({ props })}
          <DropdownMenu.Trigger
            {...props}
            aria-label={columnsLabel}
            aria-expanded={columnsOpen}
            class={buttonVariants({ variant: "ghost", size: "icon" })}
          >
            <Columns3 size={16} />
          </DropdownMenu.Trigger>
        {/snippet}
      </WithTooltip>

      <DropdownMenu.Content align="end" class="w-44">
        {#each columnOptions as col}
          <DropdownMenu.CheckboxItem
            checked={resolvedVisibleColumns.includes(col.key)}
            disabled={col.alwaysVisible}
            onCheckedChange={(checked) => handleColumnToggle(col.key, checked)}
            class="flex items-center gap-2.5 text-sm"
          >
            {col.label}
          </DropdownMenu.CheckboxItem>
        {/each}
      </DropdownMenu.Content>
    </DropdownMenu.Root>

    <!-- Settings sheet; its trigger and tooltip live in SettingsSheet.svelte. -->
    <SettingsSheet
      rowDensity={resolvedDensity}
      ondensitychange={handleSetDensity}
      fontSize={resolvedFontSize}
      onfontsizechange={handleSetFontSize}
      blockedEmphasis={resolvedBlockedEmphasis}
      onemphasischange={handleSetBlockedEmphasis}
      regionBands={resolvedRegionBands}
      onregionbandschange={handleSetRegionBands}
      theme={resolvedTheme}
      onthemechange={handleSetTheme}
      detailPanelPosition={resolvedPosition}
      onpositionchange={handleSetPosition}
      openDetailOn={resolvedOpenDetailOn}
      onopendetailchange={handleSetOpenDetailOn}
    />
  </div>
</header>

<!-- Filter band. role="search" gives these controls a landmark outside <header>
     and <main>. The z-index puts the autocomplete and warning chip over the
     table, below --z-drag-ghost and --z-modal. -->
<div class="relative flex flex-wrap items-center gap-2 border-b border-border px-6 py-2" role="search" aria-label="Filters" style="z-index: var(--z-toolbar);">
  <!-- Keyword search. Input has no adornment slot, so the icon and clear button
       are positioned over it. Full row below md. -->
  <div class="relative isolate w-full min-w-0 md:w-auto md:flex-1 md:min-w-[22rem] md:max-w-[36rem]">
    <ListFilter
      size={16}
      class="pointer-events-none absolute left-2.5 top-1/2 z-20 -translate-y-1/2 text-muted-foreground"
    />
    <!-- Behind the input (z-0 vs z-10) with identical box metrics, so spans line
         up glyph-for-glyph. -->
    <div
      bind:this={backdrop}
      aria-hidden="true"
      data-testid="filter-highlight"
      class="pointer-events-none absolute inset-0 z-0 flex items-center overflow-hidden rounded-lg border border-transparent bg-popover pl-8 {hasKeyword ? 'pr-8' : 'pr-2.5'} text-sm"
    >
      <!-- Spans from `cut` on sit in the value well; with no run, `cut` is the
           group's length and nothing is filled. Neither wrapper adds metrics. -->
      <div class="shrink-0 whitespace-pre"
        >{#each highlightGroups as g (g.start)}{@const cut = g.valueRunStart < 0 ? g.spans.length : g.valueRunStart}<span data-structured={g.structured}
          >{#each g.spans.slice(0, cut) as s (s.start)}<span class={SPAN_CLASS[s.kind]} data-kind={s.kind}>{keywordText.slice(s.start, s.end)}</span>{/each}{#if g.valueRunStart >= 0}<span class={VALUE_WELL} data-testid="value-well"
            >{#each g.spans.slice(g.valueRunStart) as s (s.start)}<span class={SPAN_CLASS[s.kind]} data-kind={s.kind}>{keywordText.slice(s.start, s.end)}</span>{/each}</span
          >{/if}</span
        >{/each}</div>
    </div>
    <Input
      bind:ref={keywordInput}
      type="text"
      placeholder="Filter by keyword, type:bug, -tags:wip"
      value={keywordText}
      oninput={handleKeyword}
      onkeydown={handleKeywordKeydown}
      onfocus={handleKeywordFocus}
      onblur={handleKeywordBlur}
      onscroll={syncBackdropScroll}
      autocomplete="off"
      aria-autocomplete="list"
      data-testid="filter-keyword"
      style="caret-color: var(--foreground);"
      class="relative z-10 bg-transparent text-transparent pl-8 {hasKeyword ? 'pr-8' : ''}"
    />
    <!-- Token layer: the backdrop's metrics and character stream, above the z-10
         input. Only token wrappers take pointer events. -->
    <div
      bind:this={tokenLayer}
      aria-hidden="true"
      data-testid="filter-tokens"
      class="pointer-events-none absolute inset-0 z-20 flex items-center overflow-hidden rounded-lg border border-transparent pl-8 {hasKeyword ? 'pr-8' : 'pr-2.5'} text-sm text-transparent"
    >
      <!-- Pointer-only affordance over the accessible input, so no key handler. No
           remove button: the layer reserves no width, so it would overlap the
           token's last glyph. The span's attributes follow the spread, so tabindex
           stays -1 in this aria-hidden layer; triggerElement="other" strips `type`.
           Each token mounts its own Tooltip.Root, keyed by `seg.start`.
           Keep the tags jammed together: in this whitespace-pre flow a newline
           becomes a stray text node. -->
      <div class="shrink-0 whitespace-pre"
        >{#each tokenSegs as seg (seg.start)}{#if seg.kind === "token"}<WithTooltip tooltip={tokenHint} ariaHidden triggerElement="other">{#snippet trigger({ props })}<!-- svelte-ignore a11y_click_events_have_key_events --><span
              {...props}
              role="button"
              tabindex="-1"
              data-testid="filter-token"
              data-token-start={seg.start}
              data-token-end={seg.end}
              class="cursor-pointer rounded-sm pointer-events-auto hover:bg-accent/60"
              onclick={() => selectToken(seg.start, seg.end)}
            >{keywordText.slice(seg.start, seg.end)}</span>{/snippet}</WithTooltip>{:else}<span>{keywordText.slice(seg.start, seg.end)}</span>{/if}{/each}</div>
    </div>
    {#if hasKeyword}
      <!-- The explicit onclick overrides the tooltip's close-on-click; clearing
           unmounts the button. -->
      <TooltipButton
        label={clearKeywordLabel}
        variant="ghost"
        size="icon-xs"
        data-testid="filter-keyword-clear"
        class="absolute right-1 inset-y-0 z-20 my-auto text-muted-foreground"
        onclick={clearKeyword}
      >
        <X size={14} />
      </TooltipButton>
    {/if}

    <!-- Autocomplete popover; rel rows appear once results land. -->
    {#if active?.kind === "static"}
      <SuggestionList
        items={active.completion.items}
        activeIndex={suggestIndex}
        onselect={(item) => applyCompletion(item)}
        testId="filter-suggestions"
        itemTestId="filter-suggestion"
      />
    {:else if active?.kind === "rel" && relResults.length > 0}
      <SuggestionList
        items={relResults}
        activeIndex={suggestIndex}
        onselect={(nib) => applyRelSelection(nib)}
        itemKey={(nib) => nib.id}
        testId="filter-suggestions"
        itemTestId="filter-suggestion"
      >
        {#snippet item(nib)}
          <span class="flex w-full items-center gap-2" data-nib-type={nib.type}>
            <TypeIcon type={nib.type} size={14} />
            <span class="min-w-0 flex-1 truncate">{nib.title}</span>
            <span class="ml-auto flex shrink-0 items-center gap-1.5 text-muted-foreground">
              <span class="font-mono text-caption">{nib.id}</span>
              <StatusIcon status={nib.status} />
            </span>
          </span>
        {/snippet}
      </SuggestionList>
    {/if}

    <!-- Warning chip for invalid values. Hidden while the autocomplete is open:
         both anchor below the input, and a valid suggestion supersedes it. -->
    {#if flaggedTokens.length > 0 && activeItemCount === 0}
      <div
        data-testid="filter-invalid"
        role="status"
        class="absolute left-0 top-full z-10 mt-1 flex max-w-full items-center gap-1 rounded-md border border-warning/40 bg-popover px-2 py-1 text-caption text-warning shadow-md"
      >
        <TriangleAlert size={12} />
        <span>Unrecognized: {flaggedTokens.join(" ")}</span>
      </div>
    {/if}
  </div>

  <!-- Filter dropdowns -->
  {#each dropdowns as dd}
    {@const count = getCount(dd.field)}
    <DropdownMenu.Root open={filterOpenStates[dd.id]} onOpenChange={(open) => handleFilterOpenChange(dd.id, open)}>
      <DropdownMenu.Trigger
        class="{buttonVariants({ variant: 'outline', size: 'default' })} shrink-0 text-muted-foreground"
      >
        {dd.label}
        <span class="ml-0.5 inline-flex h-4.5 min-w-4.5 items-center justify-center rounded-full px-1 text-label {count ? 'bg-primary text-primary-foreground' : 'invisible'}">{count || 0}</span>
        <ChevronDown size={14} class="text-muted-foreground" />
      </DropdownMenu.Trigger>
      <DropdownMenu.Content align="start">
        {#if dd.id === "status"}
          <!-- "Open" is every non-closed status. Deferred is closed, so a "not
               finished" preset would name the same set. -->
          <DropdownMenu.Label class="text-label text-muted-foreground">Presets</DropdownMenu.Label>
          <DropdownMenu.Item
            data-testid="status-preset-open"
            onSelect={() => applyStatusPreset(OPEN_STATUSES)}
          >
            <Eye size={14} />
            Open
          </DropdownMenu.Item>
          <DropdownMenu.Separator />
        {/if}
        {#each dd.values as value}
          <DropdownMenu.CheckboxItem
            checked={isChecked(dd.field, value)}
            onCheckedChange={() => handleToggle(dd.field, value)}
            aria-label={value}
          >
            {#if dd.field === "type"}
              <TypeIcon type={value} size={14} />
            {:else if dd.field === "priority"}
              {@const ind = priorityIndicators[value]}
              {#if ind}
                <span class="inline-block w-3.5 text-center text-xs font-bold" style="color: {ind.color};">{ind.symbol}</span>
              {:else}
                <span class="inline-block w-3.5"></span>
              {/if}
            {:else if dd.field === "status"}
              <StatusIcon status={value} />
            {:else if dd.field === "estimate"}
              <span class="inline-block w-3.5 text-center text-xs font-semibold text-muted-foreground">{value.toUpperCase()}</span>
            {/if}
            {#if dd.field === "estimate"}{ESTIMATE_LABELS[value] ?? value}{:else}{value}{/if}
          </DropdownMenu.CheckboxItem>
        {/each}
        <DropdownMenu.Separator />
        <DropdownMenu.Item
          disabled={getClearableCount(dd.field) === 0}
          onSelect={() => handleClearField(dd.field, dd.id)}
        >
          <X size={13} />
          Clear
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu.Root>
  {/each}
  <!-- Last in the row: help for the whole band, not the query box. -->
  <QueryHelp />
</div>
