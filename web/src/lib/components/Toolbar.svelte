<script lang="ts">
  import { VIEW_LEVELS, DEFAULT_COLUMNS, ALL_COLUMN_KEYS, DEFAULT_THEME, DEFAULT_DETAIL_PANEL_POSITION, DEFAULT_BLOCKED_EMPHASIS, DEFAULT_FONT_SIZE } from "../types";
  import type { NibFilter, ViewLevel, ColumnKey, RowDensity, FontSize, Theme, DetailPanelPosition, BlockedEmphasis } from "../types";
  import type { Preferences } from "../preferences.svelte";
  import { TYPES, STATUSES, PRIORITIES, ESTIMATES, ESTIMATE_LABELS, OPEN_STATUSES, OPEN_PLUS_DEFERRED_STATUSES } from "../constants";
  import {
    Plus,
    ChevronDown,
    X,
    Columns3,
    Eye,
    ListTree,
    ListFilter,
  } from "@lucide/svelte";
  import { typeIcons } from "../icons";
  import { priorityIndicators } from "../badges";
  import type { TypeIconInfo } from "../icons";
  import { resolveFilter, resolveViewLevel, resolveVisibleColumns, emitFilter as emitFilterHelper } from "../resolvePrefs";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu/index.js";
  import * as Tooltip from "$lib/components/ui/tooltip/index.js";
  import { buttonVariants } from "$lib/components/ui/button/index.js";
  import { cn } from "$lib/utils.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import StatusIcon from "./StatusIcon.svelte";
  import TypeIcon from "./TypeIcon.svelte";
  import SettingsSheet from "./SettingsSheet.svelte";

  let {
    prefs = undefined as Preferences | undefined,
    filter = undefined as NibFilter | undefined,
    onchange = undefined as ((filter: NibFilter) => void) | undefined,
    viewLevel = undefined as ViewLevel | undefined,
    onviewlevelchange = undefined as ((level: ViewLevel) => void) | undefined,
    visibleColumns = undefined as ColumnKey[] | undefined,
    oncolumnschange = undefined as ((columns: ColumnKey[]) => void) | undefined,
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
    milestones: typeIcons.milestone,
    epics: typeIcons.epic,
    features: typeIcons.feature,
  };

  // Resolve values: prefs takes precedence over individual props
  let resolvedFilter = $derived(resolveFilter(prefs, filter));
  let resolvedViewLevel = $derived(resolveViewLevel(prefs, viewLevel));
  let resolvedVisibleColumns = $derived(resolveVisibleColumns(prefs, visibleColumns));
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

  // Parent is a normal toggleable column in every lens now.
  let columnOptions = $derived(DEFAULT_COLUMNS);

  const VIEW_LEVEL_LABELS: Record<ViewLevel, string> = {
    none: "None",
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
    // Maintain canonical column order
    updated.sort((a, b) => ALL_COLUMN_KEYS.indexOf(a) - ALL_COLUMN_KEYS.indexOf(b));
    if (prefs) {
      prefs.columnVisibility = { ...prefs.columnVisibility, [prefs.viewLevel]: updated };
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
  let hasKeyword = $derived(!!resolvedFilter.search);

  function handleKeyword(event: Event) {
    const value = (event.target as HTMLInputElement).value;
    emitFilter({ ...resolvedFilter, search: value || undefined });
  }

  function clearKeyword() {
    emitFilter({ ...resolvedFilter, search: undefined });
    keywordInput?.focus();
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

  // State-facet quick presets (nibs-ni1v): OVERWRITE the status include-list in
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
    <!-- New button. Wrapped via Tooltip.Trigger's `child` snippet so the tooltip
         and the DropdownMenu triggers merge onto a SINGLE button element. -->
    <Tooltip.Root>
      <DropdownMenu.Root bind:open={addMenuOpen}>
        <Tooltip.Trigger>
          {#snippet child({ props })}
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
        </Tooltip.Trigger>

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
      <Tooltip.Content side="bottom">{newItemLabel}</Tooltip.Content>
    </Tooltip.Root>

    <!-- Separator -->
    <div class="mx-1 h-5 w-px bg-border shrink-0"></div>

    <!-- View selector (group-by) -->
    <Tooltip.Root>
      <DropdownMenu.Root bind:open={viewLevelOpen}>
        <Tooltip.Trigger>
          {#snippet child({ props })}
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
        </Tooltip.Trigger>

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
      <Tooltip.Content side="bottom">{groupByLabel}</Tooltip.Content>
    </Tooltip.Root>

    <!-- Columns dropdown -->
    <Tooltip.Root>
      <DropdownMenu.Root bind:open={columnsOpen}>
        <Tooltip.Trigger>
          {#snippet child({ props })}
            <DropdownMenu.Trigger
              {...props}
              aria-label={columnsLabel}
              aria-expanded={columnsOpen}
              class={buttonVariants({ variant: "ghost", size: "icon" })}
            >
              <Columns3 size={16} />
            </DropdownMenu.Trigger>
          {/snippet}
        </Tooltip.Trigger>

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
      <Tooltip.Content side="bottom">{columnsLabel}</Tooltip.Content>
    </Tooltip.Root>

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
  <div class="relative w-[400px] max-w-full min-w-0">
    <ListFilter
      size={16}
      class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground"
    />
    <Input
      bind:ref={keywordInput}
      type="text"
      placeholder="Filter by keyword"
      value={resolvedFilter.search ?? ""}
      oninput={handleKeyword}
      data-testid="filter-keyword"
      class="pl-8 {hasKeyword ? 'pr-8' : ''}"
    />
    {#if hasKeyword}
      <!-- Clear button. Plain button, so Tooltip.Trigger renders it directly (its
           onclick chains with the tooltip's via mergeProps). Styled via the shared
           buttonVariants primitive so it inherits the focus-visible ring, active
           press, and radius; the positioning utilities are layered on with cn. -->
      <Tooltip.Root>
        <Tooltip.Trigger
          type="button"
          aria-label={clearKeywordLabel}
          data-testid="filter-keyword-clear"
          class={cn(
            buttonVariants({ variant: "ghost", size: "icon-xs" }),
            "absolute right-1 inset-y-0 my-auto text-muted-foreground"
          )}
          onclick={clearKeyword}
        >
          <X size={14} />
        </Tooltip.Trigger>
        <Tooltip.Content side="bottom">{clearKeywordLabel}</Tooltip.Content>
      </Tooltip.Root>
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
          <!-- Quick presets that OVERWRITE the status include-list (nibs-ni1v).
               Replaces the retired standalone hide-completed toggle: "Open" shows
               active work, "Open + deferred" mirrors the old hide-completed
               (everything except completed + scrapped). The per-status checkboxes
               below remain for precise tweaking. -->
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
