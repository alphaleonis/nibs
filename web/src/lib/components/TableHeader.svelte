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

  // The whole sortable <th> is the sort control, so a below-threshold click
  // anywhere in the header — label, padding, or arrow — cycles that column's
  // sort. Only the resize edge-handle is exempt.
  function handleHeaderSortClick(field: SortField, e: MouseEvent) {
    // The resize edge-handle is a child of the <th>; a click on it resizes, never
    // sorts.
    if ((e.target as HTMLElement).closest(".resize-handle")) return;
    // Keep the header click from reaching the table's delegated row-click handler.
    e.stopPropagation();
    // A past-threshold reorder-drag swallows its trailing click so it can't sort.
    if (columnDrag.consumeClickSuppression()) return;
    onSort(field);
  }

  // Keyboard activation of the focusable sortable columnheader: Enter/Space cycle
  // the sort. preventDefault stops Space from paging the scroll container, and
  // stopPropagation keeps the key event from bubbling to the grid's keyboard-nav
  // handler on the scroll container — which would otherwise act on a background
  // focused row for a key-repeat or a modifier chord. The header therefore
  // consumes its own Enter/Space unconditionally, but only a clean, non-repeat,
  // unmodified press actually sorts. A keydown never trails a pointer drag, so
  // this path does not consult columnDrag.consumeClickSuppression (unlike the
  // mouse handleHeaderSortClick).
  function handleHeaderSortKeydown(field: SortField, e: KeyboardEvent) {
    if (e.key !== "Enter" && e.key !== " ") return;
    e.preventDefault();
    e.stopPropagation();
    if (e.repeat || e.ctrlKey || e.metaKey || e.altKey) return;
    onSort(field);
  }
</script>

<!-- Sortable-column header content: the label + a direction arrow for the active
     field, in EVERY view. The <th> itself is the sort control (see its onclick /
     onkeydown); this snippet only renders, carrying no interaction of its own. -->
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
    </th>
    {#each columns as key (key)}
      {@const def = COLUMNS[key]}
      {@const sortField = def.sortable ? def.sortKey : null}
      <!--
        Keep the sortable <th> a native columnheader (implicit role) — do NOT add
        role="button". aria-sort is only honored on columnheader/rowheader roles,
        so a button role would silence sort-state announcement; keyboard
        operability comes from tabindex=0 + the explicit onkeydown instead. The
        <th> itself carries the click/keydown sort handlers, hence the
        svelte-ignore for a static element with interaction handlers. The
        columnheader's accessible name stays its visible label (no aria-label
        override) so cell/column association announces the plain column name;
        aria-sort conveys the sortable state.
      -->
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
          // The resize edge-handle owns its own pointerdown; never start a
          // reorder-drag from it.
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
  /* Column reorder affordances. The whole header is grabbable (a movement
     threshold in useColumnDrag distinguishes a reorder-drag from the nibs-6grg
     sort-click); the resize edge-handle keeps its own col-resize cursor via its
     higher-specificity rule + stacking. */
  .col-header {
    cursor: grab;
    /* The header is a click/drag sort control, not selectable text — a header
       pointer-drag must not start a text selection of the label. The native
       <button> this replaced provided user-select:none implicitly. */
    user-select: none;
    -webkit-user-select: none;
  }

  /* The whole sortable header is the click target; shift its label toward the
     foreground on hover so the full-width sort affordance is discoverable.
     Transition lives on the base rule so the color eases both in and out. */
  .sort-label {
    transition: color 0.1s ease;
  }
  .col-header:hover .sort-label {
    color: var(--foreground);
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
