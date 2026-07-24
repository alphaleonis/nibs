<script lang="ts">
  import { COLUMNS } from "../columns";
  import type { ColumnKey } from "../columns";
  import type { TableSort, SortField } from "../types";
  import { useColumnAdapters } from "../ColumnAdapters.svelte";
  import type { ColumnDrag } from "../composables/useColumnDrag.svelte";
  import type { ColumnResize } from "../composables/useColumnResize.svelte";
  import { CopyPlus, CopyMinus, ArrowUp, ArrowDown } from "@lucide/svelte";

  // The whole table header. TreeTable owns the data/state (resolved column list,
  // widths, sort, the resize/drag composables) and passes them down; this
  // component hosts the header <tr> and the three-way click-vs-drag arbitration
  // (edge-handle → resize; header-body pointerdown + threshold → below-threshold
  // pointerup = sort-click, past-threshold = reorder). Header gestures stay wired
  // on the header elements — the scroll-container delegation that handles rows
  // never sees them (no tr[data-nib-id] ancestor), and the sort control stops
  // propagation so a header click can't reach the delegated row-click handler.
  interface Props {
    // Visible columns in the per-view order (drives the <th> sequence, matching
    // TreeTableRow's cell loop so cells stay aligned under their headers).
    columns: ColumnKey[];
    // Resolved width (px) per column, keyed by ColumnKey.
    columnWidths: Record<ColumnKey, number>;
    // Active table sort — the single source for both the header arrow and
    // aria-sort — or null when no sort is applied/visible.
    activeSort: TableSort | null;
    // Visibility predicate; the resize double-click auto-fit needs it to index
    // the resized column past any hidden ones.
    showColumn: (key: ColumnKey) => boolean;
    // Gesture composables, owned by TreeTable and consumed here as props.
    columnResize: ColumnResize;
    columnDrag: ColumnDrag;
    // Cycle the sort for a field (asc → desc → off).
    onSort: (field: SortField) => void;
    // Actions-column controls.
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

  // Per-column header renderers. Header content for each <th> comes from the
  // adapter (read from context, which works in a child); the <th> shell (width,
  // resize handle, sort UI) stays here.
  const adapters = useColumnAdapters();

  // aria-sort for a sortable <th>: the active direction when this field is the
  // table sort, else "none". Every sortable header reports it in every view;
  // non-sortable headers omit the attribute (handled at the call site).
  function ariaSortFor(field: SortField): "ascending" | "descending" | "none" {
    if (activeSort?.field !== field) return "none";
    return activeSort.direction === "asc" ? "ascending" : "descending";
  }
</script>

<!-- Sortable-column header content: a click-to-sort button (asc → desc → off)
     showing an arrow for the active field, in EVERY view. The click is on the
     button, not the sibling resize handle, and stops propagation so it never
     reaches the table's delegated row-click handler. -->
{#snippet sortableHeader(field: SortField, label: string)}
  <button
    type="button"
    data-testid="table-sort-{field}"
    class="inline-flex items-center gap-1 text-label text-muted-foreground hover:text-foreground"
    aria-label={`Sort by ${label}`}
    onclick={(e) => { e.stopPropagation(); if (columnDrag.consumeClickSuppression()) return; onSort(field); }}
  >
    {label}
    {#if activeSort?.field === field}
      {#if activeSort.direction === "asc"}
        <ArrowUp size={12} data-testid="table-sort-arrow-{field}" aria-hidden="true" />
      {:else}
        <ArrowDown size={12} data-testid="table-sort-arrow-{field}" aria-hidden="true" />
      {/if}
    {/if}
  </button>
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
    </th>
    {#each columns as key (key)}
      {@const def = COLUMNS[key]}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <th
        data-col-key={key}
        class="text-left text-label text-muted-foreground px-3 py-2 relative bg-background col-header"
        class:col-dragging={columnDrag.draggedKey === key}
        class:col-drop-before={columnDrag.targetKey === key && columnDrag.targetSide === "before"}
        class:col-drop-after={columnDrag.targetKey === key && columnDrag.targetSide === "after"}
        style="width: {columnWidths[key]}px;"
        aria-sort={def.sortable && def.sortKey ? ariaSortFor(def.sortKey) : undefined}
        onpointerdown={(e) => {
          // The resize edge-handle owns its own pointerdown; never start a
          // reorder-drag from it.
          if ((e.target as HTMLElement).closest(".resize-handle")) return;
          columnDrag.onHeaderPointerDown(key, e);
        }}
      >
        {#if def.sortable && def.sortKey}
          {@render sortableHeader(def.sortKey, def.label)}
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
  /* Column reorder affordances. The whole header is grabbable (a movement
     threshold in useColumnDrag distinguishes a reorder-drag from the nibs-6grg
     sort-click); the resize edge-handle keeps its own col-resize cursor via its
     higher-specificity rule + stacking. */
  .col-header {
    cursor: grab;
  }

  /* The header being dragged recedes; its drop target shows an insertion edge on
     the side the cursor is over — mirroring the row drop-before/after indicators. */
  .col-dragging {
    opacity: 0.4;
  }

  .col-drop-before {
    box-shadow: inset 2px 0 0 0 var(--ring);
  }

  .col-drop-after {
    box-shadow: inset -2px 0 0 0 var(--ring);
  }
</style>
