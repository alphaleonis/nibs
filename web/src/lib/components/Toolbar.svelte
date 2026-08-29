<script lang="ts">
  import { VIEW_LEVELS, VIEW_LEVEL_LABELS, DEFAULT_THEME, DEFAULT_DETAIL_PANEL_POSITION, DEFAULT_OPEN_DETAIL_ON, DEFAULT_BLOCKED_EMPHASIS, DEFAULT_FONT_SIZE } from "../types";
  import type { NibFilter, ViewLevel, RowDensity, FontSize, Theme, DetailPanelPosition, OpenDetailGesture, BlockedEmphasis } from "../types";
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
    TriangleAlert,
  } from "@lucide/svelte";
  import { typeIcons } from "../icons";
  import { priorityIndicators } from "../badges";
  import type { TypeIconInfo } from "../icons";
  import { resolveFilter, resolveViewLevel, resolveVisibleColumns, resolveColumnOrder, emitFilter as emitFilterHelper, switchViewLevel } from "../resolvePrefs";
  import type { TreeViewState } from "../treeView.svelte";
  import { parseQuery, serializeQuery, getCompletion, tokenGroups, tokenSegments, relTokenValueContext } from "../query";
  import type { Completion, QueryFilter, SpanKind, RelValueContext, NibSuggestion } from "../query";
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
    projectName = "",
    searchNibs = undefined,
  }: {
    prefs?: Preferences;
    filter?: NibFilter;
    onchange?: (filter: NibFilter) => void;
    viewLevel?: ViewLevel;
    onviewlevelchange?: (level: ViewLevel) => void;
    /** Threaded from App rather than read from context: `useTreeView()` throws
     *  when absent, and the toolbar renders standalone in its own tests with no
     *  table to reconcile. Absent, the view still switches — nothing reconciles
     *  it. */
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
    projectName?: string;
    searchNibs?: SearchNibsFn;
  } = $props();

  // Relationship-id typeahead search. Defaults to one built from the urql context
  // client; tests inject `searchNibs` and render Toolbar without a urql provider,
  // so a missing context here is tolerated (the default is never used then). The
  // prop is set once — read it untracked (a plain init-time capture, like the
  // `keywordText` seed below) to avoid a reactivity warning.
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
  };

  // Resolve values: prefs takes precedence over individual props
  let resolvedFilter = $derived(resolveFilter(prefs, filter));
  let resolvedViewLevel = $derived(resolveViewLevel(prefs, viewLevel));
  let resolvedVisibleColumns = $derived(resolveVisibleColumns(prefs, visibleColumns));
  // The per-view column order — the visible set is re-sorted by it on toggle so a
  // re-shown column lands in its user-chosen position, not the canonical one.
  let resolvedColumnOrder = $derived(resolveColumnOrder(prefs, columnOrder));
  let ViewLevelIcon = $derived(VIEW_LEVEL_ICON_INFO[resolvedViewLevel].icon);

  // Static trigger labels shared by each control's aria-label and its Tooltip.Content,
  // defined once so the accessible name and the visible tooltip can't drift apart.
  const newItemLabel = "New item";
  const viewLabel = "View";
  const columnsLabel = "Columns";
  const clearKeywordLabel = "Clear keyword";
  // The token hint is tooltip text only, never an accessible name: the layer it
  // lives in is aria-hidden, so AT users reach the tokens through the input instead.
  // Held here rather than inline because the token markup is whitespace-sensitive.
  const tokenHint = "Click to select · Delete to remove";

  let addMenuOpen = $state(false);
  let viewLevelOpen = $state(false);
  let columnsOpen = $state(false);
  let viewLevelIconInfo = $derived(VIEW_LEVEL_ICON_INFO[resolvedViewLevel]);

  // The Columns dropdown derives directly from the column model (single source):
  // canonical order, each option carrying label + alwaysVisible. Parent is a
  // normal toggleable column in every lens now.
  let columnOptions = $derived(ALL_COLUMN_KEYS.map((key) => COLUMNS[key]));

  function emitFilter(updated: NibFilter) {
    emitFilterHelper(prefs, onchange, updated);
  }

  function handleSelectViewLevel(level: ViewLevel) {
    // Through the seam, never straight at the preference: switching lenses can
    // leave the focused/selected rows without a row to be on, and the write alone
    // cannot tell the table that happened.
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
    // Keep the visible set ordered by the per-view columnOrder (falls back to the
    // canonical ALL_COLUMN_KEYS order when no custom order is set).
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
  // The box parses its text into structured filter fields (the five metadata
  // facets + their exclusions + free text) on every keystroke, so every matching
  // dropdown ticks live. NibFilter stays canonical: `keywordText` is the LITERAL
  // text the box shows, reconciled to the filter's canonical serialization ONLY
  // while the box is unfocused — never rewritten under the cursor. Blur flips
  // `keywordFocused` false, which re-runs the effect and snaps the box to
  // canonical (field order, casing, whitespace normalized).
  //
  // The box OWNS this slice of NibFilter — the five metadata facets, free text,
  // and (phase 5) the relationship-id scalars + existence/state booleans. Fields
  // outside this set are preserved untouched across box edits.
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
    // Existence/state booleans.
    "hasParent", "hasBlocking", "hasBlockedBy", "isBlocked",
  ] as const satisfies readonly (keyof QueryFilter)[];

  // Copy one box field from the parsed slice onto a NibFilter, or delete it when
  // the parse yields nothing for it. QueryFilter[K] === NibFilter[K] for these
  // keys (QueryFilter is a Pick of NibFilter), so this stays fully typed.
  function assignBoxField<K extends keyof QueryFilter>(target: NibFilter, source: QueryFilter, key: K) {
    const value = source[key];
    if (value !== undefined) target[key] = value;
    else delete target[key];
  }

  // Invalid known-field tokens (e.g. `status:banana`) parsed out of the box text.
  // They contribute nothing to the filter but are preserved so the box can flag
  // them and round-trip them across canonicalization and dropdown edits. When a
  // `prefs` is present they live on it (persisted + shared as part of the
  // canonical `?q=` query, so a reload / shared link reproduces the parked
  // tokens); the prefs-less callback mode keeps them in local component state.
  let localInvalidTokens = $state<string[]>([]);
  let resolvedInvalidTokens = $derived(prefs ? prefs.invalidTokens : localInvalidTokens);
  function setInvalidTokens(tokens: string[]) {
    if (prefs) prefs.invalidTokens = tokens;
    else localInvalidTokens = tokens;
  }

  let keywordFocused = $state(false);
  // Seed from the canonical query so the clear button / placeholder state is
  // correct on first paint (before the effect runs), including any parked invalid
  // tokens restored from storage / a shared link. `untrack` makes the one-time
  // read explicit — the $effect below owns keeping it in sync thereafter.
  let keywordText = $state(untrack(() => serializeQuery({ filter: resolvedFilter, invalidTokens: resolvedInvalidTokens })));
  let canonicalQuery = $derived(serializeQuery({ filter: resolvedFilter, invalidTokens: resolvedInvalidTokens }));
  let hasKeyword = $derived(keywordText.length > 0);

  $effect(() => {
    const next = canonicalQuery;
    if (!keywordFocused) {
      // Untracked write: the box mirrors the filter, not the other way around, so
      // this must not re-subscribe on `keywordText` and self-trigger.
      untrack(() => {
        keywordText = next;
      });
    }
  });

  // --- Syntax-highlight backdrop (Phase 3) ---
  // The box paints a colored, per-token backdrop layer BEHIND a transparent-text
  // <input>: the input stays the editor (native caret, selection, paste, undo,
  // horizontal scroll) and the backdrop is display-only (`aria-hidden`, no pointer
  // events). `spans` tiles the literal box text contiguously so every glyph lines
  // up with the input; `syncBackdropScroll` locks the backdrop's horizontal scroll
  // to the input's so a long query stays aligned as it scrolls out of view.
  let backdrop = $state<HTMLDivElement | null>(null);
  // Grouped per token so a chip can wrap field + operator + value as one unit. The
  // flattened spans are identical to `tokenizeSpans(keywordText)`, so the glyph
  // flow and offsets are unchanged by the grouping.
  let highlightGroups = $derived(tokenGroups(keywordText));

  // --- Token-click affordances (Phase 7) ---
  // A thin interaction layer ABOVE the input mirrors the backdrop's token layout
  // (same box metrics, `whitespace-pre` flow and horizontal scroll) so its per-token
  // hit-regions align glyph-for-glyph. The container is `pointer-events-none`; only
  // the token wrappers opt back in, so clicking a whitespace gap still falls through
  // to the input and places the caret normally. `tokenSegs` groups the highlight
  // spans into one segment per filter token (plus the gaps).
  let tokenLayer = $state<HTMLDivElement | null>(null);
  let tokenSegs = $derived(tokenSegments(keywordText));

  // Click a token → select its full range in the input for quick editing, which is
  // also how a token gets removed (select, then Delete). No caret math beyond the
  // segment offsets; the input stays the sole editor.
  function selectToken(start: number, end: number) {
    if (!keywordInput) return;
    keywordInput.focus();
    keywordInput.setSelectionRange(start, end);
  }

  // Per-kind highlight colors, all shadcn semantic tokens (theme-aware).
  //
  // The split is STRUCTURE vs CONTENT: the field name and the punctuation that
  // joins it to its value are plain foreground, and the accent is spent on the
  // value. Free text stays muted, so it reads as the one thing the parser did not
  // act on, and invalid values keep the destructive color plus a wavy underline.
  //
  // The value accent is `--query-value`, an alias of the tag accent rather than
  // `--link`: link is a step darker (indigo-400 vs 300) and graphite never
  // overrides it, so values landed at 5.36:1 there — the dimmest blue in the
  // palette carrying the part of a token you most need to read.
  //
  // Punctuation is foreground rather than muted for a measured reason. Muted put
  // the comma at 5.58:1 between two 14.88:1 values — the lowest-contrast glyph in
  // the brightest neighborhood, and the smallest patch of ink in the string, so it
  // vanished. It is now the brightest thing there instead of the dimmest.
  //
  // Sharing one color with `field` is intended: both are structure. What must stay
  // distinct is structure vs `freetext` — those were previously the SAME muted
  // color, so punctuation inside a working token looked like text the parser had
  // ignored.
  const SPAN_CLASS: Record<SpanKind, string> = {
    field: "text-foreground",
    operator: "text-foreground",
    value: "text-query-value",
    invalid: "text-destructive underline decoration-wavy",
    freetext: "text-muted-foreground",
    whitespace: "",
  };

  // The well drawn behind a token's VALUE RUN (see `TokenGroup.valueRunStart`) —
  // everything after the field's colon. The field name stays on the plain surface,
  // so the fill marks exactly the part that carries meaning, and it runs THROUGH a
  // comma rather than breaking at it (filling value spans one by one made
  // `status:todo,in-progress` read as two separate pills).
  //
  // Metrics are locked to the transparent input glyph-for-glyph, so this may only
  // use background and radius. Padding, borders and font-weight all change advance
  // widths and would drift the caret off the text — which is why the outlined chip
  // this replaces was abandoned: an outline wants padding it cannot be given, so it
  // read as a border crowding the glyphs.
  //
  // `--query-well` is that theme's box surface darkened by a fixed OKLCH lightness
  // step. Darkening is the point: the accent tint it replaces LIGHTENED the surface
  // toward the value text, while a well pushes away from it and raises contrast.
  const VALUE_WELL = "rounded-[3px] bg-query-well";

  // Lock the display backdrop AND the token-affordance layer to the input's
  // horizontal scroll so both stay glyph-aligned as a long query scrolls.
  function syncBackdropScroll() {
    if (!keywordInput) return;
    if (backdrop) backdrop.scrollLeft = keywordInput.scrollLeft;
    if (tokenLayer) tokenLayer.scrollLeft = keywordInput.scrollLeft;
  }

  // Re-sync after any value change (typing, completion insert, clear, or a
  // dropdown-driven canonicalization). The input adjusts its own scrollLeft during
  // layout, so read it on the next frame; the `onscroll` handler covers caret moves
  // that scroll without changing the text.
  $effect(() => {
    keywordText;
    requestAnimationFrame(syncBackdropScroll);
  });

  // Parse the box text into the canonical filter + invalid sidecar, then emit.
  // Box-owned fields are set from the parse or dropped; everything else is kept.
  function emitFromText(text: string) {
    const parsed = parseQuery(text);
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

  // --- Autocomplete: static metadata completion + async relationship typeahead ---
  // `active` unifies the two suggestion sources behind one popover + keyboard nav:
  //  - "static": the synchronous metadata / enum / tag completion (phases 2–3).
  //  - "rel":    the caret sits in a relationship-id token's VALUE; candidate nibs
  //              are fetched asynchronously (debounced) and held in `relResults`.
  type ActiveCompletion =
    | { kind: "static"; completion: Completion }
    | { kind: "rel"; ctx: RelValueContext };
  let active = $state<ActiveCompletion | null>(null);
  let suggestIndex = $state(-1);
  let suggestBlurTimer: ReturnType<typeof setTimeout> | null = null;

  // Async rel-token typeahead: debounced search + in-flight/stale-response guard.
  const REL_SEARCH_DEBOUNCE_MS = 200;
  let relResults = $state<NibSuggestion[]>([]);
  // The fragment `relResults` answers. Rows are deliberately HELD across a fragment
  // change so the list does not flicker while the next query debounces — so what is
  // on screen can lag the typed text. Every write to `relResults` sets this too, and
  // the accept path refuses rows whose fragment no longer matches the caret's: the
  // stale guard in `runRelSearch` protects the WRITE, this protects the COMMIT.
  let relResultsFragment = $state("");
  let relDebounceTimer: ReturnType<typeof setTimeout> | null = null;
  // Bumped per fired query; a resolved response whose seq is no longer current is
  // dropped (a newer keystroke superseded it).
  let relRequestSeq = 0;

  // Row count for whichever source is active — keyboard nav + render read this.
  let activeItemCount = $derived(
    !active ? 0 : active.kind === "static" ? active.completion.items.length : relResults.length,
  );

  // Rel rows are committable only while they answer the fragment now under the caret.
  let relResultsAreCurrent = $derived(
    active?.kind === "rel" && relResultsFragment === active.ctx.fragment,
  );

  function cancelRelSearch() {
    if (relDebounceTimer) { clearTimeout(relDebounceTimer); relDebounceTimer = null; }
  }

  const sameItems = (a: readonly string[], b: readonly string[]) =>
    a.length === b.length && a.every((v, i) => v === b[i]);

  // Recompute the caret-token suggestions. A rel-token value context routes to the
  // async path (debounced fetch); otherwise fall back to static completion.
  // `explicit` marks a Ctrl+Space request: only then does an empty token open the
  // full field list. Typing (and focus) must leave an empty box alone.
  function refreshCompletion(explicit = false) {
    if (!keywordInput) { active = null; setRelResults([], ""); cancelRelSearch(); return; }
    const value = keywordInput.value;
    const caret = keywordInput.selectionStart ?? value.length;
    // An explicit refresh over an unchanged list changes nothing, so the highlight
    // survives it — wiping it there would silently redirect the next accept to row 0.
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
    const c = getCompletion(value, caret, availableTags, { explicit });
    active = c ? { kind: "static", completion: c } : null;
    if (heldItems && c && sameItems(heldItems, c.items)) suggestIndex = heldIndex;
  }

  // The rows and the fragment they answer move together — see `relResultsFragment`.
  function setRelResults(results: NibSuggestion[], fragment: string) {
    relResults = results;
    relResultsFragment = fragment;
  }

  function scheduleRelSearch(fragment: string) {
    cancelRelSearch();
    // Empty fragment (`parent:` with nothing typed yet): don't query — show
    // nothing until the user types at least one character.
    if (fragment === "") { setRelResults([], fragment); return; }
    relDebounceTimer = setTimeout(() => {
      relDebounceTimer = null;
      void runRelSearch(fragment);
    }, REL_SEARCH_DEBOUNCE_MS);
  }

  async function runRelSearch(fragment: string) {
    const seq = ++relRequestSeq;
    // A rejecting search fn (injected/derived one whose promise rejects, or a
    // real transport error `createNibSearch` doesn't swallow) degrades to "no
    // suggestions" rather than surfacing as an unhandled rejection — the caller
    // invokes this as `void runRelSearch(...)` with no `.catch`.
    let results: NibSuggestion[];
    try {
      results = await effectiveSearchNibs(fragment);
    } catch (err) {
      console.warn("rel-token search failed:", err);
      results = [];
    }
    // Stale guard: drop the response if a newer query started, or the caret has
    // since left a rel context / moved to a different fragment.
    if (seq !== relRequestSeq) return;
    if (!active || active.kind !== "rel" || active.ctx.fragment !== fragment) return;
    setRelResults(results, fragment);
  }

  // Insert a chosen static suggestion (field name / enum value / tag).
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

  // Insert a chosen candidate nib's id, replacing the token's partial value run.
  async function applyRelSelection(nib: NibSuggestion) {
    if (!active || active.kind !== "rel" || !keywordInput) return;
    // Never commit a row fetched for a fragment the user has since moved past.
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

  // The text accepting row `index` would produce, or `null` when there is nothing to
  // accept. Two rows are not accepts: one that rewrites the text to what is already
  // there (an inserted value substring-matches ITSELF, so the popover reopens holding
  // it), and a rel row answering a superseded fragment. Tab consults this before
  // swallowing the key, so a non-accept keeps its native focus move.
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
    // Ctrl+Space sits ABOVE the early return: its whole job is opening a completion
    // when none is active. Mid-token it recomputes the same filtered list, so
    // pressing it over an open popover leaves it open, highlight and all. The chord
    // is required EXACTLY: AltGr reports both Control and Alt on Windows, and
    // AltGr+Space types a non-breaking space that must reach the box.
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
      // Accept in one keystroke: the highlighted row, or the first when none is
      // (`suggestIndex === -1` — the state after every refresh). Taking a row rather
      // than inserting the rows' common prefix is the decided design: values match by
      // substring, where there often is no common prefix at all, and field names
      // (which do match by prefix) follow the same rule for one behavior. Tab is only
      // swallowed when accepting actually rewrites the text — otherwise forward Tab
      // could never leave the box, since accepting reopens the popover on the
      // inserted value. Shift+Tab is not intercepted at all.
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

  // Drop the active popover + any pending/held rel results.
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
    // Defer clearing so a suggestion click (which fires after blur) still lands;
    // the option's mousedown preventDefault keeps focus for a keyboard/click pick.
    if (suggestBlurTimer) clearTimeout(suggestBlurTimer);
    suggestBlurTimer = setTimeout(() => {
      suggestBlurTimer = null;
      clearCompletion();
    }, 150);
  }

  // Cancel a pending debounced search on unmount so a fake-timer test (or a real
  // teardown) can't fire a fetch after the component is gone.
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

  // The exclude-list twin of each dropdown's include-list. The dropdowns only
  // ever WRITE include-lists, but the query box writes both (`-status:closed`),
  // so a dropdown that touched one key alone could not undo what the box did.
  const EXCLUDE_KEY = {
    type: "excludeType",
    priority: "excludePriority",
    status: "excludeStatus",
    estimate: "excludeEstimate",
    tags: "excludeTags",
  } as const satisfies Record<FilterField, keyof NibFilter>;

  // State-facet quick presets: OVERWRITE the status include-list in
  // one click. The include-list is the single source of truth for status
  // visibility, so a preset replaces (not merges with) the current selection; the
  // per-status checkboxes below remain for precise tweaking afterward.
  //
  // The exclusion goes with it: `status:open` alongside a surviving
  // `-status:open` (one `-status:open` completion away in the box) is an empty
  // result set, and the preset is the obvious thing to click to recover from it.
  function applyStatusPreset(statuses: readonly string[]) {
    const updated: NibFilter = { ...resolvedFilter, status: [...statuses] };
    delete updated.excludeStatus;
    emitFilter(updated);
    filterOpenStates.status = false;
  }

  // Clear means "stop filtering on this facet", so it drops the exclude-list as
  // well — otherwise a `-status:completed` typed in the box is unreachable from
  // the dropdown that owns that facet.
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

  // What Clear would actually remove — include plus exclude. Kept separate from
  // getCount so the badge does not claim an exclusion as a checked box, while
  // Clear still enables itself when there is an exclusion to clear.
  function getClearableCount(field: FilterField): number {
    return getCount(field) + (resolvedFilter[EXCLUDE_KEY[field]]?.length ?? 0);
  }

</script>

<!-- App header chrome: two sibling root bands (this <header> + the filter band below)
     mount directly as flex-column children of App's h-screen shell; do not wrap them in a
     single gapped container (a gap-y there would insert a visible gap between two bands that
     read as one chrome unit). -->
<header class="flex flex-wrap items-center justify-between gap-3 border-b border-border px-6 py-3">
  <!-- The banner carries the "Nibs" half of the title, so only the project name
       is text here. Its height is in `em` rather than a fixed rung so it tracks
       the font-scale setting along with the h1 it sits in.
       The mark fills the banner's full height, so the rendered height set here —
       nothing inside the SVG — is what decides how large the mark reads. 1.6em
       is the largest value that leaves the header at its natural height; above
       it the banner outgrows the row and drags the header taller. -->
  <h1 class="flex min-w-0 items-center gap-2.5 text-xl font-semibold">
    <NibsLogo class="h-[1.6em] w-auto shrink-0" />
    {#if projectName}
      <span aria-hidden="true" class="shrink-0 text-muted-foreground">·</span>
      <span class="min-w-0 max-w-[28ch] lg:max-w-none truncate">{projectName}</span>
    {/if}
    <ConnectionStatus status={connectionStatus} />
  </h1>

  <div class="flex shrink-0 items-center gap-1">
    <!-- New button. WithTooltip wraps the tooltip; the DropdownMenu.Trigger
         it renders CHAINS the tooltip's handlers with the menu's open handlers. -->
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

    <!-- Settings sheet (far-right). The gear button + its own Tooltip live inside
         SettingsSheet.svelte. -->
    <SettingsSheet
      rowDensity={resolvedDensity}
      ondensitychange={handleSetDensity}
      fontSize={resolvedFontSize}
      onfontsizechange={handleSetFontSize}
      blockedEmphasis={resolvedBlockedEmphasis}
      onemphasischange={handleSetBlockedEmphasis}
      theme={resolvedTheme}
      onthemechange={handleSetTheme}
      detailPanelPosition={resolvedPosition}
      onpositionchange={handleSetPosition}
      openDetailOn={resolvedOpenDetailOn}
      onopendetailchange={handleSetOpenDetailOn}
    />
  </div>
</header>

<!-- Filter band: search + filters. role="search" restores a landmark for these
     controls, which sit between the <header> band and <main> (outside both). -->
<!-- `relative` + z-index lifts the whole filter band (a root sibling of <main>)
     into a stacking context above the table, so the box's absolutely-positioned
     autocomplete dropdown and invalid marker paint OVER the rows below rather than
     behind them. Kept below --z-drag-ghost/--z-modal. -->
<div class="relative flex flex-wrap items-center gap-2 border-b border-border px-6 py-2" role="search" aria-label="Filters" style="z-index: var(--z-toolbar);">
  <!-- Keyword search. Input is a bare primitive with no adornment slot, so wrap
       it in a relative container with an absolutely-positioned left icon and a
       right clear button, padding the input to make room for both. The box now
       holds full `field:value` condition strings, so it wants width: on md+ it
       grows (flex-1, floored/capped so it stays readable without hogging the row)
       and the facet dropdowns sit to its right, wrapping below only when they no
       longer fit. Below md it takes the WHOLE row (w-full) so the dropdowns stack
       onto the line(s) below instead of squeezing it. The overlay backdrop +
       token layer are inset-0 on this wrapper, so they track its width at any
       size (scroll-sync is width-independent). -->
  <div class="relative isolate w-full min-w-0 md:w-auto md:flex-1 md:min-w-[22rem] md:max-w-[36rem]">
    <ListFilter
      size={16}
      class="pointer-events-none absolute left-2.5 top-1/2 z-20 -translate-y-1/2 text-muted-foreground"
    />
    <!-- Syntax-highlight backdrop: sits BEHIND the transparent-text input (z-0 vs
         z-10) and mirrors its box exactly — same border width, padding, font-size
         and line box — so the colored per-token spans line up glyph-for-glyph with
         what the user types. `overflow-hidden` + scrollLeft sync keep it aligned as
         a long query scrolls. Display-only: aria-hidden, no pointer events. -->
    <div
      bind:this={backdrop}
      aria-hidden="true"
      data-testid="filter-highlight"
      class="pointer-events-none absolute inset-0 z-0 flex items-center overflow-hidden rounded-lg border border-transparent bg-popover pl-8 {hasKeyword ? 'pr-8' : 'pr-2.5'} text-sm"
    >
      <!-- One wrapper per token group, with a second wrapper around the value run so
           the well covers field-colon-onward as ONE shape rather than each value
           separately. Neither wrapper adds metrics (see VALUE_WELL), so the glyph
           flow is identical to the flat span list this replaced.
           `head` is everything before the run; when there is no run (a gap, a bare
           word, a parked whole-token invalid) it is the entire group and nothing is
           filled. -->
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
    <!-- Token-click affordance layer (Phase 7): mirrors the backdrop's box metrics,
         `whitespace-pre` flow and horizontal scroll so its hit-regions line up with
         the input's glyphs. `aria-hidden` (the input is the accessible editor) and
         `pointer-events-none` on the container: only the per-token wrappers re-enable
         pointer events, so a click in a whitespace gap falls through to the input
         (caret placement) and the search icon / clear button (both z-20) stay
         clickable. z-20 puts the token wrappers above the z-10 input; they render the
         SAME character stream as the backdrop (tokens wrapped, gaps as inert text) so
         alignment is glyph-for-glyph. -->
    <div
      bind:this={tokenLayer}
      aria-hidden="true"
      data-testid="filter-tokens"
      class="pointer-events-none absolute inset-0 z-20 flex items-center overflow-hidden rounded-lg border border-transparent pl-8 {hasKeyword ? 'pr-8' : 'pr-2.5'} text-sm text-transparent"
    >
      <!-- Token wrapper carries onclick (select the token). It is a pointer-only
           affordance layered over the accessible input — keyboard users edit the
           input directly and the layer is aria-hidden — so the missing key handler
           is intentional; the inline svelte-ignore stays glued to the <span> to keep
           the whitespace-pre flow free of stray text nodes.

           The wrapper holds NO button: the layer is `overflow-hidden` (for the
           horizontal scroll) and reserves no width, so any in-box remove control
           would have to overlap the token's own trailing glyph — `type:bug` reading
           as `type:b×g`. Removal is therefore click-to-select + Delete, advertised
           by the chip tint, the pointer cursor and the hover tooltip.

           WithTooltip in OVERRIDE mode: the spread comes first so the span's own
           attributes win over the ones bits-ui merges in for a <button> trigger.
           `tabindex` stays -1 that way (the merged 0 would make this focusable
           inside an aria-hidden layer, a tab stop that announces nothing); `type`
           has no valid value to override it with on a <span>, so `triggerElement`
           has WithTooltip strip it before the snippet runs.

           The hint does not survive the click it invites. The tooltip's own
           close-on-click handler is already inert — the shared Tooltip.Provider
           sets `disableCloseOnTriggerClick` — so the span's `onclick` overriding it
           is a no-op. It closes by a different path: clicking this `tabindex="-1"`
           span focuses it, selectToken() then moves focus to the input, and
           bits-ui's spread `onblur` closes the tooltip. Accepted, because the hint
           has been read by the time the click lands, and one that outlives its own
           trigger's focus is its own oddity.

           Each token mounts its own Tooltip.Root, and this repo's Root wraps a
           per-instance Provider that registers a window scroll listener — so those
           listeners scale with the token count, and any non-tail edit shifts the
           `seg.start` keys below it and rebuilds every downstream Root. Accepted: a
           query holds a handful of tokens, and appending (the common edit) leaves
           the keys stable. bits-ui's tooltip tether — one shared Root behind many
           triggers — is the primitive to reach for if that stops holding.

           Every tag below is jammed against its neighbor: this is a
           `whitespace-pre` flow that must reproduce the input's character stream
           glyph-for-glyph, and any newline between tags would become a stray text
           node. -->
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
      <!-- Clear button. Plain action button: TooltipButton spreads the tooltip
           props then lets our explicit onclick OVERRIDE. This drops the tooltip's
           own close-on-click handler,
           which is safe here: clearKeyword sets search=undefined, so this button
           unmounts via {#if hasKeyword} and bits-ui's trigger-unregister cleanup
           closes the tooltip anyway. Styled via the shared buttonVariants primitive
           (focus ring, active press, radius); the absolute-positioning utilities
           are layered on via the class prop. -->
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

    <!-- Autocomplete popover, anchored below the input. Static path: plain-string
         metadata/enum/tag rows. Rel path: rich candidate-nib rows (type icon +
         title + id + status) from the debounced search, shown only once results
         land. Both share the keyboard nav + active-highlight. -->
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

    <!-- Attached warning chip for known-field tokens with an invalid value (e.g.
         status:banana). The token stays in the box and results reflect only the
         valid tokens. Suppressed while the autocomplete dropdown is open: both
         anchor below the input, and offering the valid value already supersedes
         the "unrecognized" nag (e.g. typing `status:dra` suggests `draft`). -->
    {#if resolvedInvalidTokens.length > 0 && activeItemCount === 0}
      <div
        data-testid="filter-invalid"
        role="status"
        class="absolute left-0 top-full z-10 mt-1 flex max-w-full items-center gap-1 rounded-md border border-warning/40 bg-popover px-2 py-1 text-caption text-warning shadow-md"
      >
        <TriangleAlert size={12} />
        <span>Unrecognized: {resolvedInvalidTokens.join(" ")}</span>
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
          <!-- One quick preset that OVERWRITES the status include-list: "Open"
               shows everything that is not closed. There is no second preset
               because deferred is a closed status, so "open" and "not finished"
               name the same set. The per-status checkboxes below remain for
               precise tweaking, including showing deferred work on its own. -->
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
  <!-- Syntax help sits at the END of the control row, after every facet, so it
       reads as help for the whole band rather than as an adornment on the query
       box — and it takes width from neither the box nor a facet when the row
       wraps on a narrow screen. -->
  <QueryHelp />
</div>
