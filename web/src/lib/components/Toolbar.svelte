<script lang="ts">
  import { VIEW_LEVELS, DEFAULT_THEME, DEFAULT_DETAIL_PANEL_POSITION, DEFAULT_BLOCKED_EMPHASIS, DEFAULT_FONT_SIZE } from "../types";
  import type { NibFilter, ViewLevel, RowDensity, FontSize, Theme, DetailPanelPosition, BlockedEmphasis } from "../types";
  import { ALL_COLUMN_KEYS, COLUMNS } from "../columns";
  import type { ColumnKey } from "../columns";
  import type { Preferences } from "../preferences.svelte";
  import { TYPES, STATUSES, PRIORITIES, ESTIMATES, ESTIMATE_LABELS, OPEN_STATUSES, OPEN_PLUS_DEFERRED_STATUSES } from "../constants";
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
  import { resolveFilter, resolveViewLevel, resolveVisibleColumns, resolveColumnOrder, emitFilter as emitFilterHelper } from "../resolvePrefs";
  import { parseQuery, serializeQuery, getCompletion, tokenizeSpans } from "../query";
  import type { Completion, QueryFilter, SpanKind } from "../query";
  import { untrack, tick } from "svelte";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu/index.js";
  import { buttonVariants } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import StatusIcon from "./StatusIcon.svelte";
  import TypeIcon from "./TypeIcon.svelte";
  import SuggestionList from "./SuggestionList.svelte";
  import SettingsSheet from "./SettingsSheet.svelte";
  import TooltipButton from "./TooltipButton.svelte";
  import TooltipDropdownTrigger from "./TooltipDropdownTrigger.svelte";

  let {
    prefs = undefined as Preferences | undefined,
    filter = undefined as NibFilter | undefined,
    onchange = undefined as ((filter: NibFilter) => void) | undefined,
    viewLevel = undefined as ViewLevel | undefined,
    onviewlevelchange = undefined as ((level: ViewLevel) => void) | undefined,
    visibleColumns = undefined as ColumnKey[] | undefined,
    oncolumnschange = undefined as ((columns: ColumnKey[]) => void) | undefined,
    columnOrder = undefined as ColumnKey[] | undefined,
    oncreatenew = undefined as ((type: string) => void) | undefined,
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
    availableTags = [],
    projectName = "",
  }: {
    prefs?: Preferences;
    filter?: NibFilter;
    onchange?: (filter: NibFilter) => void;
    viewLevel?: ViewLevel;
    onviewlevelchange?: (level: ViewLevel) => void;
    visibleColumns?: ColumnKey[];
    oncolumnschange?: (columns: ColumnKey[]) => void;
    columnOrder?: ColumnKey[];
    oncreatenew?: (type: string) => void;
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
    availableTags?: string[];
    projectName?: string;
  } = $props();

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
  const groupByLabel = "Group by";
  const columnsLabel = "Columns";
  const clearKeywordLabel = "Clear keyword";
  let addMenuOpen = $state(false);
  let viewLevelOpen = $state(false);
  let columnsOpen = $state(false);
  let viewLevelIconInfo = $derived(VIEW_LEVEL_ICON_INFO[resolvedViewLevel]);

  // The Columns dropdown derives directly from the column model (single source):
  // canonical order, each option carrying label + alwaysVisible. Parent is a
  // normal toggleable column in every lens now.
  let columnOptions = $derived(ALL_COLUMN_KEYS.map((key) => COLUMNS[key]));

  const VIEW_LEVEL_LABELS: Record<ViewLevel, string> = {
    none: "Tree",
    flat: "Flat",
    milestones: "Milestones",
    epics: "Epics",
    features: "Features & Bugs",
  };

  function emitFilter(updated: NibFilter) {
    emitFilterHelper(prefs, onchange, updated);
  }

  function handleSelectViewLevel(level: ViewLevel) {
    if (prefs) {
      prefs.viewLevel = level;
    } else {
      onviewlevelchange?.(level);
    }
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
  type DropdownId = "type" | "priority" | "state" | "effort" | "tags";

  interface DropdownConfig {
    id: DropdownId;
    label: string;
    field: FilterField;
    values: readonly string[];
  }

  let dropdowns = $derived<DropdownConfig[]>([
    { id: "type", label: "Type", field: "type", values: TYPES },
    { id: "priority", label: "Priority", field: "priority", values: PRIORITIES },
    { id: "state", label: "State", field: "status", values: STATUSES },
    { id: "effort", label: "Effort", field: "estimate", values: ESTIMATES },
    ...(availableTags.length > 0 ? [{ id: "tags" as DropdownId, label: "Tags", field: "tags" as FilterField, values: availableTags }] : []),
  ]);

  let filterOpenStates = $state<Record<DropdownId, boolean>>({
    type: false,
    priority: false,
    state: false,
    effort: false,
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
  // The box OWNS this slice of NibFilter; other fields (relationships/existence)
  // are preserved untouched across box edits.
  const BOX_FIELD_KEYS = [
    "type", "excludeType",
    "priority", "excludePriority",
    "status", "excludeStatus",
    "estimate", "excludeEstimate",
    "tags", "excludeTags",
    "search",
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
  // They contribute nothing to the filter but are preserved here so the box can
  // flag them and round-trip them across canonicalization and dropdown edits.
  let invalidTokens = $state<string[]>([]);

  let keywordFocused = $state(false);
  // Seed from the canonical query so the clear button / placeholder state is
  // correct on first paint (before the effect runs). `untrack` makes the one-time
  // read explicit — the $effect below owns keeping it in sync thereafter.
  let keywordText = $state(untrack(() => serializeQuery({ filter: resolvedFilter, invalidTokens: [] })));
  let canonicalQuery = $derived(serializeQuery({ filter: resolvedFilter, invalidTokens }));
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
  let spans = $derived(tokenizeSpans(keywordText));

  // Per-kind highlight colors, all shadcn semantic tokens (theme-aware): field
  // names read as links, values as normal foreground, punctuation + free text
  // muted, invalid values in the destructive color with a wavy red underline.
  const SPAN_CLASS: Record<SpanKind, string> = {
    field: "text-link",
    operator: "text-muted-foreground",
    value: "text-foreground",
    invalid: "text-destructive underline decoration-wavy",
    freetext: "text-muted-foreground",
    whitespace: "",
  };

  function syncBackdropScroll() {
    if (backdrop && keywordInput) backdrop.scrollLeft = keywordInput.scrollLeft;
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
    invalidTokens = parsed.invalidTokens;
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
    invalidTokens = [];
    completion = null;
    const updated: NibFilter = { ...resolvedFilter };
    for (const key of BOX_FIELD_KEYS) delete updated[key];
    emitFilter(updated);
    keywordInput?.focus();
  }

  // --- Static autocomplete (field names / enum values / existing tags) ---
  let completion = $state<Completion | null>(null);
  let suggestIndex = $state(-1);
  let suggestBlurTimer: ReturnType<typeof setTimeout> | null = null;

  // Recompute the caret-token suggestions from the input's current value + caret.
  function refreshCompletion() {
    if (!keywordInput) { completion = null; return; }
    const caret = keywordInput.selectionStart ?? keywordInput.value.length;
    completion = getCompletion(keywordInput.value, caret, availableTags);
    suggestIndex = -1;
  }

  async function applyCompletion(item: string) {
    if (!completion || !keywordInput) return;
    const { text, caret } = completion.apply(item);
    keywordText = text;
    emitFromText(text);
    await tick();
    keywordInput.focus();
    keywordInput.setSelectionRange(caret, caret);
    // Re-suggest for the new caret (e.g. `type:` → its enum values).
    refreshCompletion();
  }

  function handleKeywordKeydown(event: KeyboardEvent) {
    if (!completion || completion.items.length === 0) return;
    if (event.key === "ArrowDown") {
      event.preventDefault();
      suggestIndex = (suggestIndex + 1) % completion.items.length;
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      suggestIndex = suggestIndex <= 0 ? completion.items.length - 1 : suggestIndex - 1;
    } else if (event.key === "Enter") {
      if (suggestIndex >= 0 && suggestIndex < completion.items.length) {
        event.preventDefault();
        applyCompletion(completion.items[suggestIndex]);
      }
    } else if (event.key === "Escape") {
      event.preventDefault();
      completion = null;
      suggestIndex = -1;
    }
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
      completion = null;
      suggestIndex = -1;
    }, 150);
  }

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

  // State-facet quick presets: OVERWRITE the status include-list in
  // one click. The include-list is the single source of truth for status
  // visibility, so a preset replaces (not merges with) the current selection; the
  // per-status checkboxes below remain for precise tweaking afterward.
  function applyStatusPreset(statuses: readonly string[]) {
    emitFilter({ ...resolvedFilter, status: [...statuses] });
    filterOpenStates.state = false;
  }

  function handleClearField(field: FilterField, id: DropdownId) {
    const updated = { ...resolvedFilter };
    delete updated[field];
    emitFilter(updated);
    filterOpenStates[id] = false;
  }

  function isChecked(field: FilterField, value: string): boolean {
    return resolvedFilter[field]?.includes(value) ?? false;
  }

  function getCount(field: FilterField): number {
    return resolvedFilter[field]?.length ?? 0;
  }

</script>

<!-- App header chrome: two sibling root bands (this <header> + the filter band below)
     mount directly as flex-column children of App's h-screen shell; do not wrap them in a
     single gapped container (a gap-y there would insert a visible gap between two bands that
     read as one chrome unit). -->
<header class="flex flex-wrap items-center justify-between gap-3 border-b border-border px-6 py-3">
  <h1 class="min-w-0 max-w-[28ch] lg:max-w-none truncate text-xl font-semibold">Nibs{projectName ? ` - ${projectName}` : ""}</h1>

  <div class="flex shrink-0 items-center gap-1">
    <!-- New button. TooltipDropdownTrigger wraps the tooltip; the DropdownMenu.Trigger
         it renders CHAINS the tooltip's handlers with the menu's open handlers. -->
    <DropdownMenu.Root bind:open={addMenuOpen}>
      <TooltipDropdownTrigger tooltip={newItemLabel}>
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
      </TooltipDropdownTrigger>

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

    <!-- View selector (group-by) -->
    <DropdownMenu.Root bind:open={viewLevelOpen}>
      <TooltipDropdownTrigger tooltip={groupByLabel}>
        {#snippet trigger({ props })}
          <DropdownMenu.Trigger
            {...props}
            aria-label={`${groupByLabel}: ${VIEW_LEVEL_LABELS[resolvedViewLevel]}`}
            class={buttonVariants({ variant: "outline", size: "default" })}
          >
            <ViewLevelIcon size={14} style="color: {viewLevelIconInfo.color};" />
            {VIEW_LEVEL_LABELS[resolvedViewLevel]}
            <ChevronDown size={14} />
          </DropdownMenu.Trigger>
        {/snippet}
      </TooltipDropdownTrigger>

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
      <TooltipDropdownTrigger tooltip={columnsLabel}>
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
      </TooltipDropdownTrigger>

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
    />
  </div>
</header>

<!-- Filter band: search + filters. role="search" restores a landmark for these
     controls, which sit between the <header> band and <main> (outside both). -->
<div class="flex flex-wrap items-center gap-2 border-b border-border px-6 py-2" role="search" aria-label="Filters">
  <!-- Keyword search. Input is a bare primitive with no adornment slot, so wrap
       it in a relative container with an absolutely-positioned left icon and a
       right clear button, padding the input to make room for both. Capped at
       ~400px (no flex-1) so the facet dropdowns cluster next to it on the left. -->
  <div class="relative isolate w-[400px] max-w-full min-w-0">
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
      <div class="shrink-0 whitespace-pre"
        >{#each spans as s (s.start)}<span class={SPAN_CLASS[s.kind]} data-kind={s.kind}>{keywordText.slice(s.start, s.end)}</span>{/each}</div>
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

    <!-- Static autocomplete popover, anchored below the input. Shown while the
         box is focused and the caret token has suggestions. -->
    {#if completion}
      <SuggestionList
        items={completion.items}
        activeIndex={suggestIndex}
        onselect={(item) => applyCompletion(item)}
        testId="filter-suggestions"
        itemTestId="filter-suggestion"
      />
    {/if}

    <!-- Simple, non-overlay marker for known-field tokens with an invalid value
         (e.g. status:banana). The token stays in the box and results reflect only
         the valid tokens; the fancy overlay rendering is a later phase. -->
    {#if invalidTokens.length > 0}
      <div
        data-testid="filter-invalid"
        role="status"
        class="absolute left-0 top-full mt-0.5 flex items-center gap-1 text-caption text-warning"
      >
        <TriangleAlert size={12} />
        <span>Unrecognized: {invalidTokens.join(" ")}</span>
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
        {#if dd.id === "state"}
          <!-- Quick presets that OVERWRITE the status include-list. "Open" shows
               active work; "Open + deferred" shows everything except completed +
               scrapped. The per-status checkboxes below remain for precise
               tweaking. -->
          <DropdownMenu.Label class="text-label text-muted-foreground">Presets</DropdownMenu.Label>
          <DropdownMenu.Item
            data-testid="state-preset-open"
            onSelect={() => applyStatusPreset(OPEN_STATUSES)}
          >
            <Eye size={14} />
            Open
          </DropdownMenu.Item>
          <DropdownMenu.Item
            data-testid="state-preset-open-deferred"
            onSelect={() => applyStatusPreset(OPEN_PLUS_DEFERRED_STATUSES)}
          >
            <Eye size={14} />
            Open + deferred
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
          disabled={count === 0}
          onSelect={() => handleClearField(dd.field, dd.id)}
        >
          <X size={13} />
          Clear
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu.Root>
  {/each}
</div>
