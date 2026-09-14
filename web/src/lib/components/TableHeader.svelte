<script lang="ts">
  import { COLUMNS } from "../columns";
  import type { ColumnKey } from "../columns";
  import type { TableSort, SortField } from "../types";
  import { useColumnAdapters } from "../ColumnAdapters.svelte";
  import { GHOST_OFFSET_X, GHOST_OFFSET_Y } from "../composables/useColumnDrag.svelte";
  import type { ColumnDrag } from "../composables/useColumnDrag.svelte";
  import type { ColumnResize } from "../composables/useColumnResize.svelte";
  import { CopyPlus, CopyMinus, ArrowUp, ArrowDown } from "@lucide/svelte";

  // The table header row. TreeTable owns the columns, widths, sort and gesture
  // composables; this component arbitrates header gestures: the edge handle
  // resizes, a below-threshold click sorts, a past-threshold drag reorders. Sort
  // clicks stop propagation so the table's delegated row-click handler never sees
  // them.
  interface Props {
    // Visible columns, in the order TreeTableRow renders cells.
    columns: ColumnKey[];
    columnWidths: Record<ColumnKey, number>;
    // Null when no sort is applied.
    activeSort: TableSort | null;
    // Lets the resize double-click auto-fit index past hidden columns.
    showColumn: (key: ColumnKey) => boolean;
    columnResize: ColumnResize;
    columnDrag: ColumnDrag;
    // Cycles asc → desc → off.
    onSort: (field: SortField) => void;
    onExpandAll: () => void;
    onCollapseAll: () => void;
  }

  let {
    columns,
    columnWidths,
    activeSort,
    showColumn,
    columnResize,
    columnDrag,
    onSort,
    onExpandAll,
    onCollapseAll,
  }: Props = $props();

  // Header content for a non-sortable column.
  const adapters = useColumnAdapters();

  function ariaSortFor(field: SortField): "ascending" | "descending" | "none" {
    if (activeSort?.field !== field) return "none";
    return activeSort.direction === "asc" ? "ascending" : "descending";
  }

  function handleHeaderSortClick(field: SortField, e: MouseEvent) {
    if ((e.target as HTMLElement).closest(".resize-handle")) return;
    e.stopPropagation();
    // A reorder drag swallows its trailing click.
    if (columnDrag.consumeClickSuppression()) return;
    onSort(field);
  }

  // Enter/Space are consumed unconditionally so they never reach the grid's
  // keyboard-nav handler; only an unmodified, non-repeat press sorts.
  function handleHeaderSortKeydown(field: SortField, e: KeyboardEvent) {
    if (e.key !== "Enter" && e.key !== " ") return;
    e.preventDefault();
    e.stopPropagation();
    if (e.repeat || e.ctrlKey || e.metaKey || e.altKey) return;
    onSort(field);
  }
</script>

<!-- Label + active-sort arrow. The <th> carries the interaction. -->
{#snippet sortableHeader(field: SortField, label: string)}
  <span
    data-testid="table-sort-{field}"
    class="sort-label inline-flex items-center gap-1 text-label text-muted-foreground"
  >
    {label}
    {#if activeSort?.field === field}
      {#if activeSort.direction === "asc"}
        <ArrowUp size={12} data-testid="table-sort-arrow-{field}" aria-hidden="true" />
      {:else}
        <ArrowDown size={12} data-testid="table-sort-arrow-{field}" aria-hidden="true" />
      {/if}
    {/if}
  </span>
{/snippet}

<thead class="sticky top-0" style="z-index: var(--z-sticky);">
  <tr>
    <th class="w-8 bg-background" style="width: 32px;">
      <!-- Raw buttons: two 12px icon controls must fit inside the 32px-wide
           actions column; smaller than the Button primitive's minimum size. -->
      <div class="flex items-center">
        <button data-testid="expand-all" class="rounded-sm p-0.5 text-muted-foreground hover:text-foreground" onclick={onExpandAll} title="Expand all">
          <CopyPlus size={12} />
        </button>
        <button data-testid="collapse-all" class="rounded-sm p-0.5 text-muted-foreground hover:text-foreground" onclick={onCollapseAll} title="Collapse all">
          <CopyMinus size={12} />
        </button>
      </div>
      <!-- Cursor-following clone of the dragged header. Inside the actions <th>
           for valid table markup; `pointer-events: none` keeps it out of the
           drag's elementFromPoint drop-target detection. -->
      {#if columnDrag.ghost}
        {@const ghost = columnDrag.ghost}
        <div
          data-testid="col-drag-ghost"
          aria-hidden="true"
          class="col-drag-ghost text-left text-label text-muted-foreground px-3 py-2 bg-background border border-border rounded-md shadow-lg"
          style="position: fixed; pointer-events: none; left: {ghost.x + GHOST_OFFSET_X}px; top: {ghost.y + GHOST_OFFSET_Y}px; width: {ghost.width}px; z-index: var(--z-drag-ghost);"
        >
          <!-- Not the sortableHeader snippet: its `table-sort-*` testids would
               duplicate the real header's. -->
          <span class="sort-label inline-flex items-center gap-1 text-label text-muted-foreground">
            {ghost.label}
            {#if ghost.sortKey && activeSort?.field === ghost.sortKey}
              {#if activeSort.direction === "asc"}
                <ArrowUp size={12} aria-hidden="true" />
              {:else}
                <ArrowDown size={12} aria-hidden="true" />
              {/if}
            {/if}
          </span>
        </div>
      {/if}
    </th>
    {#each columns as key (key)}
      {@const def = COLUMNS[key]}
      {@const sortField = def.sortable ? def.sortKey : null}
      <!-- Keep the native columnheader role: role="button" would silence
           aria-sort. Keyboard access comes from tabindex + onkeydown. No
           aria-label, so the visible label names the column. -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <th
        data-col-key={key}
        class="text-left text-label text-muted-foreground px-3 py-2 relative bg-background col-header"
        class:col-dragging={columnDrag.draggedKey === key}
        class:col-drop-before={columnDrag.targetKey === key && columnDrag.targetSide === "before"}
        class:col-drop-after={columnDrag.targetKey === key && columnDrag.targetSide === "after"}
        style="width: {columnWidths[key]}px;"
        tabindex={sortField ? 0 : undefined}
        aria-sort={sortField ? ariaSortFor(sortField) : undefined}
        onpointerdown={(e) => {
          if ((e.target as HTMLElement).closest(".resize-handle")) return;
          columnDrag.onHeaderPointerDown(key, e);
        }}
        onclick={sortField ? (e) => handleHeaderSortClick(sortField, e) : undefined}
        onkeydown={sortField ? (e) => handleHeaderSortKeydown(sortField, e) : undefined}
      >
        {#if sortField}
          {@render sortableHeader(sortField, def.label)}
        {:else}
          {@render adapters[key].header()}
        {/if}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="resize-handle" onpointerdown={(e) => columnResize.onPointerDown(e, key)} onpointermove={columnResize.onPointerMove} onpointerup={columnResize.onPointerUp} ondblclick={() => columnResize.onDblClick(key, showColumn)}></div>
      </th>
    {/each}
  </tr>
</thead>

<style>
  .col-header {
    cursor: grab;
    /* A header drag must not select the label text. */
    user-select: none;
    -webkit-user-select: none;
  }

  /* Highlight the label on hover so the full-width sort target is discoverable. */
  .sort-label {
    transition: color 0.1s ease;
  }
  .col-header:hover .sort-label {
    color: var(--foreground);
  }

  /* The dragged header recedes; the drop target shows an insertion edge. */
  .col-dragging {
    opacity: 0.4;
  }

  .col-drop-before {
    box-shadow: inset 2px 0 0 0 var(--ring);
  }

  .col-drop-after {
    box-shadow: inset -2px 0 0 0 var(--ring);
  }

  /* Positioning is inline, updated per pointer move. */
  .col-drag-ghost {
    opacity: 0.6;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
