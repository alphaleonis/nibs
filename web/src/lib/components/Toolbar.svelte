<script lang="ts">
  import { VIEW_LEVELS, DEFAULT_COLUMNS, ALL_COLUMN_KEYS } from "../types";
  import type { NibFilter, ViewLevel, ColumnKey, RowDensity } from "../types";
  import type { Preferences } from "../preferences.svelte";
  import { hasClientFilters, resolveStatusConflicts } from "../filter";
  import { TERMINAL_STATUSES, TYPES } from "../constants";
  import {
    Plus,
    ChevronDown,
    ListFilter,
    Settings2,
    Columns3,
  } from "@lucide/svelte";
  import { typeIcons } from "../icons";
  import type { TypeIconInfo } from "../icons";
  import { resolveFilter, resolveViewLevel, resolveVisibleColumns, emitFilter as emitFilterHelper } from "../resolvePrefs";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu/index.js";

  let {
    prefs = undefined as Preferences | undefined,
    filter = undefined as NibFilter | undefined,
    onchange = undefined as ((filter: NibFilter) => void) | undefined,
    viewLevel = undefined as ViewLevel | undefined,
    onviewlevelchange = undefined as ((level: ViewLevel) => void) | undefined,
    ontogglefilters = (() => {}) as () => void,
    filtersOpen = false,
    visibleColumns = undefined as ColumnKey[] | undefined,
    oncolumnschange = undefined as ((columns: ColumnKey[]) => void) | undefined,
    oncreatenew = undefined as ((type: string) => void) | undefined,
    rowDensity = "compact" as RowDensity,
    ondensitychange = undefined as ((density: RowDensity) => void) | undefined,
  }: {
    prefs?: Preferences;
    filter?: NibFilter;
    onchange?: (filter: NibFilter) => void;
    viewLevel?: ViewLevel;
    onviewlevelchange?: (level: ViewLevel) => void;
    ontogglefilters?: () => void;
    filtersOpen?: boolean;
    visibleColumns?: ColumnKey[];
    oncolumnschange?: (columns: ColumnKey[]) => void;
    oncreatenew?: (type: string) => void;
    rowDensity?: RowDensity;
    ondensitychange?: (density: RowDensity) => void;
  } = $props();

  let resolvedDensity = $derived(prefs ? prefs.rowDensity : rowDensity);

  function handleSetDensity(density: RowDensity) {
    if (prefs) {
      prefs.rowDensity = density;
    } else {
      ondensitychange?.(density);
    }
  }

  // Resolve values: prefs takes precedence over individual props
  let resolvedFilter = $derived(resolveFilter(prefs, filter));
  let resolvedViewLevel = $derived(resolveViewLevel(prefs, viewLevel));
  let resolvedVisibleColumns = $derived(resolveVisibleColumns(prefs, visibleColumns));
  let ViewLevelIcon = $derived(VIEW_LEVEL_ICON_INFO[resolvedViewLevel].icon);

  let includeCompleted = $derived(!resolvedFilter.excludeStatus?.length);
  let addMenuOpen = $state(false);
  let viewLevelOpen = $state(false);
  let optionsOpen = $state(false);
  let columnsOpen = $state(false);
  let filtersActive = $derived(hasClientFilters(resolvedFilter));
  let viewLevelIconInfo = $derived(VIEW_LEVEL_ICON_INFO[resolvedViewLevel]);

  // Filter columns shown in the checklist: hide "parent" for milestones view
  let columnOptions = $derived(
    DEFAULT_COLUMNS.filter(col => !(col.key === "parent" && resolvedViewLevel === "milestones"))
  );

  const VIEW_LEVEL_LABELS: Record<ViewLevel, string> = {
    milestones: "Milestones",
    epics: "Epics",
    backlog: "Backlog Items",
  };

  const VIEW_LEVEL_ICON_INFO: Record<ViewLevel, TypeIconInfo> = {
    milestones: typeIcons.milestone,
    epics: typeIcons.epic,
    backlog: typeIcons.feature,
  };

  function emitFilter(updated: NibFilter) {
    emitFilterHelper(prefs, onchange, updated);
  }

  function handleToggleIncludeCompleted(checked: boolean) {
    let updated = { ...resolvedFilter };
    if (!checked) {
      updated.excludeStatus = [...TERMINAL_STATUSES];
      updated = resolveStatusConflicts(updated);
    } else {
      delete updated.excludeStatus;
    }
    emitFilter(updated);
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

  const iconBtnBase =
    "inline-flex items-center justify-center rounded-md border p-1.5 text-sm h-8 transition-colors";
  const iconBtnDefault =
    "border-border bg-popover text-muted-foreground hover:text-foreground";
  const iconBtnActive =
    "border-primary bg-primary/10 text-primary";
  const iconBtnDisabled =
    "border-border bg-popover text-muted-foreground/50 cursor-not-allowed";
</script>

<div class="flex items-center gap-2 rounded-lg border border-border bg-card px-3 py-2">
  <!-- Left: New button with type picker dropdown -->
  <DropdownMenu.Root bind:open={addMenuOpen}>
    <DropdownMenu.Trigger
      title="New item"
      data-testid="toolbar-add"
      class="{iconBtnBase} {addMenuOpen ? iconBtnActive : iconBtnDefault}"
    >
      <Plus size={16} />
    </DropdownMenu.Trigger>

    <DropdownMenu.Content align="start" class="w-40">
      {#each TYPES as nibType}
        {@const iconInfo = typeIcons[nibType]}
        {@const TypeIcon = iconInfo.icon}
        <DropdownMenu.Item
          data-testid="toolbar-add-{nibType}"
          class="flex items-center gap-2 text-sm"
          onclick={() => { oncreatenew?.(nibType); }}
        >
          <TypeIcon size={14} style="color: {iconInfo.color};" />
          {nibType}
        </DropdownMenu.Item>
      {/each}
    </DropdownMenu.Content>
  </DropdownMenu.Root>

  <!-- Spacer -->
  <div class="flex-1"></div>

  <!-- Right group -->
  <div class="flex items-center gap-1">
    <!-- View selector -->
    <DropdownMenu.Root bind:open={viewLevelOpen}>
      <DropdownMenu.Trigger
        title="Select view"
        class="flex items-center gap-1 {iconBtnBase} {viewLevelOpen ? iconBtnActive : iconBtnDefault} px-2 text-sm"
      >
        <ViewLevelIcon size={14} style="color: {viewLevelIconInfo.color};" />
        {VIEW_LEVEL_LABELS[resolvedViewLevel]}
        <ChevronDown size={14} />
      </DropdownMenu.Trigger>

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

    <!-- Filter button -->
    <button
      type="button"
      title="Filters"
      onclick={ontogglefilters}
      aria-pressed={filtersOpen}
      class="relative {iconBtnBase} {filtersOpen ? iconBtnActive : iconBtnDefault}"
    >
      <ListFilter size={16} />
      {#if filtersActive}
        <span class="absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full bg-primary"></span>
      {/if}
    </button>

    <!-- Options dropdown -->
    <DropdownMenu.Root bind:open={optionsOpen}>
      <DropdownMenu.Trigger
        title="Options"
        aria-expanded={optionsOpen}
        class="{iconBtnBase} {optionsOpen ? iconBtnActive : iconBtnDefault}"
      >
        <Settings2 size={16} />
      </DropdownMenu.Trigger>

      <DropdownMenu.Content align="end" class="w-52 p-3">
        <DropdownMenu.CheckboxItem
          checked={includeCompleted}
          onCheckedChange={handleToggleIncludeCompleted}
          class="flex items-center justify-between text-sm"
        >
          Include completed
        </DropdownMenu.CheckboxItem>
        <DropdownMenu.Separator />
        <DropdownMenu.Label class="text-xs text-muted-foreground px-2 py-1">Row density</DropdownMenu.Label>
        <DropdownMenu.RadioGroup value={resolvedDensity} onValueChange={(v) => { if (v) handleSetDensity(v as RowDensity); }}>
          <DropdownMenu.RadioItem value="compact" class="flex items-center gap-2 text-sm">
            Compact
          </DropdownMenu.RadioItem>
          <DropdownMenu.RadioItem value="comfortable" class="flex items-center gap-2 text-sm">
            Comfortable
          </DropdownMenu.RadioItem>
        </DropdownMenu.RadioGroup>
      </DropdownMenu.Content>
    </DropdownMenu.Root>

    <!-- Columns dropdown -->
    <DropdownMenu.Root bind:open={columnsOpen}>
      <DropdownMenu.Trigger
        title="Columns"
        aria-expanded={columnsOpen}
        class="{iconBtnBase} {columnsOpen ? iconBtnActive : iconBtnDefault}"
      >
        <Columns3 size={16} />
      </DropdownMenu.Trigger>

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
  </div>
</div>
