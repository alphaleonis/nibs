<script lang="ts">
  import type { NibFilter } from "../types";
  import type { Preferences } from "../preferences.svelte";
  import { STATUSES, TYPES, PRIORITIES, ESTIMATES, ESTIMATE_LABELS } from "../constants";
  import { hasClientFilters, clearClientFilters, resolveStatusConflicts } from "../filter";
  import { priorityIndicators } from "../badges";
  import { resolveFilter, emitFilter as emitFilterHelper } from "../resolvePrefs";
  import { ChevronDown, X } from "@lucide/svelte";
  import StatusDot from "./StatusDot.svelte";
  import TypeIcon from "./TypeIcon.svelte";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu/index.js";

  let {
    prefs = undefined as Preferences | undefined,
    filter = undefined as NibFilter | undefined,
    onchange = undefined as ((filter: NibFilter) => void) | undefined,
    availableTags = [],
  }: {
    prefs?: Preferences;
    filter?: NibFilter;
    onchange?: (filter: NibFilter) => void;
    availableTags?: string[];
  } = $props();

  let resolvedFilter = $derived(resolveFilter(prefs, filter));

  function emitFilter(updated: NibFilter) {
    emitFilterHelper(prefs, onchange, updated);
  }

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

  let openStates = $state<Record<DropdownId, boolean>>({
    type: false,
    priority: false,
    state: false,
    effort: false,
    tags: false,
  });

  function handleOpenChange(id: DropdownId, open: boolean) {
    if (open) {
      // Close all others when opening a new one
      for (const key of Object.keys(openStates) as DropdownId[]) {
        if (key !== id) openStates[key] = false;
      }
    }
    openStates[id] = open;
  }

  function handleKeyword(event: Event) {
    const value = (event.target as HTMLInputElement).value;
    emitFilter({ ...resolvedFilter, search: value || undefined });
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
    let updated: NibFilter = { ...resolvedFilter };
    updated[field] = toggleArrayValue(resolvedFilter[field], value);
    if (updated[field] === undefined) {
      delete updated[field];
    }
    // When changing status, remove any values that conflict with excludeStatus
    if (field === "status") {
      updated = resolveStatusConflicts(updated);
    }
    emitFilter(updated);
  }

  function handleClearField(field: FilterField, id: DropdownId) {
    const updated = { ...resolvedFilter };
    delete updated[field];
    emitFilter(updated);
    openStates[id] = false;
  }

  function isChecked(field: FilterField, value: string): boolean {
    return resolvedFilter[field]?.includes(value) ?? false;
  }

  function getCount(field: FilterField): number {
    return resolvedFilter[field]?.length ?? 0;
  }

  function handleClearAll() {
    emitFilter(clearClientFilters(resolvedFilter));
  }
</script>

<div class="flex items-center gap-2 rounded-lg border border-border bg-card px-3 py-2">
  <input
    type="text"
    placeholder="Filter by keyword"
    value={resolvedFilter.search ?? ""}
    oninput={handleKeyword}
    class="min-w-0 flex-1 rounded-md border border-input bg-popover px-3 py-1 text-sm text-foreground placeholder-muted-foreground focus:border-ring focus:outline-none"
  />

  {#each dropdowns as dd}
    {@const count = getCount(dd.field)}
    <DropdownMenu.Root open={openStates[dd.id]} onOpenChange={(open) => handleOpenChange(dd.id, open)}>
      <DropdownMenu.Trigger
        class="inline-flex items-center gap-1 rounded-md border border-input bg-popover px-2.5 py-1 text-sm text-muted-foreground transition-colors hover:border-border hover:bg-accent hover:text-accent-foreground"
      >
        {dd.label}
        <span class="ml-1 inline-flex h-4.5 min-w-4.5 items-center justify-center rounded-full px-1 text-xs font-medium {count ? 'bg-primary text-primary-foreground' : 'invisible'}">{count || 0}</span>
        <ChevronDown size={14} class="text-muted-foreground" />
      </DropdownMenu.Trigger>
      <DropdownMenu.Content align="start">
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
              <StatusDot status={value} />
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

  <button
    type="button"
    onclick={handleClearAll}
    class="ml-auto rounded p-1 transition-colors {hasClientFilters(resolvedFilter) ? 'text-muted-foreground hover:bg-accent hover:text-accent-foreground cursor-pointer' : 'text-muted-foreground/30 cursor-default'}"
    aria-label="Clear all filters"
    disabled={!hasClientFilters(resolvedFilter)}
  >
    <X size={16} />
  </button>
</div>
