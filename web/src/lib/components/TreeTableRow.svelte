<script lang="ts">
  import { ALL_COLUMN_KEYS } from "../types";
  import type { TreeTableNib, ColumnKey } from "../types";
  import { priorityIndicators, statusDotColors } from "../badges";
  import { Lock, Link, ChevronRight, ChevronDown, Plus } from "@lucide/svelte";
  import StatusDot from "./StatusDot.svelte";
  import TypeIcon from "./TypeIcon.svelte";
  import { canHaveChildren } from "../typeHierarchy";
  import { isBucketId } from "../tree";
  import { useSelection, useDrag } from "../contexts";

  import type { DropZone } from "../drag.svelte";

  interface Props {
    nib: TreeTableNib;
    depth?: number;
    hasChildren?: boolean;
    dimmed?: boolean;
    collapsed?: boolean;
    parentNib?: TreeTableNib | null;
    visibleColumns?: ColumnKey[];
    draggable?: boolean;
    highlighted?: boolean;
    fading?: boolean;
  }

  let {
    nib,
    depth = 0,
    hasChildren = false,
    dimmed = false,
    collapsed = false,
    parentNib = null,
    visibleColumns = [...ALL_COLUMN_KEYS],
    draggable = false,
    highlighted = false,
    fading = false,
  }: Props = $props();

  const selection = useSelection();
  const drag = useDrag();

  // Computed from context + nib.id
  let selected = $derived(selection.selectedNibId === nib.id || selection.selectedIds.has(nib.id));
  let focused = $derived(selection.focusedNibId === nib.id);
  let isDragged = $derived(drag.isDraggedItem(nib.id));
  let anyDragging = $derived(drag.isDragging);
  let isDropTarget = $derived(drag.dropTargetId === nib.id);
  let dropZone: DropZone | null = $derived(isDropTarget ? drag.dropZone : null);
  let dropValid = $derived(isDropTarget ? drag.dropValid : false);

  function getPriorityIndicator(priority: string) {
    return priorityIndicators[priority] ?? null;
  }

  const priorityIndicator = $derived(getPriorityIndicator(nib.priority));
  const shortId = $derived(nib.id.substring(nib.id.lastIndexOf("-") + 1));
  const statusDotColor = $derived(statusDotColors[nib.status] ?? "var(--muted-foreground)");
</script>

<tr
  data-testid="tree-row"
  class="tree-row"
  class:active={selected}
  class:focused={focused}
  class:draggable={draggable}
  class:any-dragging={anyDragging}
  class:dragged={isDragged}
  class:drop-before={isDropTarget && dropZone === "before" && dropValid}
  class:drop-after={isDropTarget && dropZone === "after" && dropValid}
  class:drop-reparent={isDropTarget && dropZone === "reparent" && dropValid}
  class:drop-invalid={isDropTarget && !dropValid}
  class:nib-highlighted={highlighted}
  class:nib-fading={fading}
  data-nib-id={nib.id}
  style={dimmed && !isDragged ? "opacity: 0.4;" : ""}
>
  <!-- Actions column -->
  <td class="actions-cell row-cell">
    <div class="actions-cell-inner">
      {#if canHaveChildren(nib.type)}
        <!-- Raw button: pure-render delegated control (data-action) whose
             reveal-on-row-hover is driven by scoped CSS; routing it through the
             Button component would break event delegation (see CLAUDE.md). -->
        <button
          data-testid="row-add-child"
          data-action="add-child"
          data-child-type={nib.type}
          class="row-add-child-btn"
          title="Add child"
        >
          <Plus size={14} />
        </button>
      {/if}
    </div>
  </td>

  <!-- ID column -->
  {#if visibleColumns.includes("id")}
    <td data-testid="nib-id" class="text-body px-3 cell-truncate row-cell" style="color: var(--muted-foreground);">{isBucketId(nib.id) ? "" : shortId}</td>
  {/if}

  <!-- Parent column -->
  {#if visibleColumns.includes("parent")}
    <td data-testid="nib-parent" class="text-body px-3 cell-truncate row-cell" style="color: var(--text-secondary);" title={parentNib ? parentNib.id : undefined}>
      {#if parentNib}
        <TypeIcon type={parentNib.type} size={14} />
        {parentNib.title}
      {/if}
    </td>
  {/if}

  <!-- Type column -->
  {#if visibleColumns.includes("type")}
    <td data-testid="nib-type" class="text-body px-3 cell-truncate row-cell" style="color: var(--text-secondary);">{nib.type}</td>
  {/if}

  <!-- Title column -->
  {#if visibleColumns.includes("title")}
    <td data-testid="nib-title" class="cell-truncate row-cell" style="padding-left: {depth * 24}px;">
      <div class="title-content">
        {#if hasChildren}
          <!-- Raw button: delegated expand/collapse control (data-action);
               kept raw to preserve TreeTable's event delegation. -->
          <button
            data-testid="toggle"
            data-action="toggle"
            class="shrink-0 w-5 h-5 inline-flex items-center justify-center rounded-sm text-muted-foreground hover:text-foreground"
          >
            {#if collapsed}<ChevronRight size={14} />{:else}<ChevronDown size={14} />{/if}
          </button>
        {:else}
          <span class="inline-block w-5 h-5 shrink-0"></span>
        {/if}
        <TypeIcon type={nib.type} />
        {#if priorityIndicator}
          <span
            data-testid="priority-icon"
            class="shrink-0 text-sm font-bold"
            style="color: {priorityIndicator.color};"
          >{priorityIndicator.symbol}</span>
        {/if}
        <!-- Raw button: delegated title control (data-action) rendered as inline
             ellipsis-truncating text; the Button primitive's flex/height layout
             doesn't fit, and delegation must be preserved. -->
        <button
          data-testid="title-text"
          data-action="title"
          class="title-text-btn"
        >{nib.title}</button>
        {#if nib.blockedByIds.length > 0}
          <span
            data-testid="blocked-icon"
            class="inline-flex items-center shrink-0"
            style="color: var(--blocked);"
            title="Blocked by {nib.blockedByIds.length} nib(s)"
          ><Lock size={12} /></span>
        {/if}
        {#if nib.blockingIds.length > 0}
          <span
            data-testid="blocking-icon"
            class="inline-flex items-center shrink-0"
            style="color: var(--blocking);"
            title="Blocking {nib.blockingIds.length} nib(s)"
          ><Link size={12} /></span>
        {/if}
      </div>
    </td>
  {/if}

  <!-- State column -->
  {#if visibleColumns.includes("state")}
    <td data-testid="nib-state" class="text-body px-3 cell-truncate row-cell">
      <StatusDot status={nib.status} class="mr-1.5" />
      <span style="color: {statusDotColor};">{nib.status}</span>
    </td>
  {/if}

  <!-- Effort column -->
  {#if visibleColumns.includes("effort")}
    <td data-testid="nib-effort" class="text-body px-3 cell-truncate row-cell">
      {#if nib.estimate?.trim()}
        {nib.estimate.toUpperCase()}
      {/if}
    </td>
  {/if}

  <!-- Tags column -->
  {#if visibleColumns.includes("tags")}
    <td data-testid="nib-tags" class="px-3 cell-truncate row-cell">
      {#each nib.tags as tag}
        <span
          data-testid="tag"
          class="inline-flex rounded-sm px-1.5 py-0.5 text-caption"
          style="background-color: var(--popover); color: var(--muted-foreground);"
        >{tag}</span>
      {/each}
    </td>
  {/if}
</tr>

<style>
  .tree-row {
    user-select: none;
    position: relative;
  }

  .tree-row.draggable {
    cursor: grab;
  }

  .tree-row:hover:not(.any-dragging) {
    background-color: var(--accent);
  }

  .tree-row.active {
    background-color: oklch(0.488 0.243 264 / 0.15);
  }

  /* Drag state: dimmed dragged row */
  .tree-row.dragged {
    opacity: 0.3;
  }

  /* Drop zone indicators */
  .tree-row.drop-before {
    box-shadow: inset 0 2px 0 0 var(--ring);
  }

  .tree-row.drop-after {
    box-shadow: inset 0 -2px 0 0 var(--ring);
  }

  .tree-row.drop-reparent {
    background-color: oklch(0.488 0.243 264 / 0.12);
    box-shadow: inset 0 0 0 1px var(--ring);
  }

  /* .tree-row.drop-invalid intentionally has no styling — invalid drop targets
     get no highlight. The class exists for drop-zone logic, not for CSS. */

  /* Real-time change highlight — brief accent background pulse */
  .tree-row.nib-highlighted {
    animation: nib-highlight-pulse 1s ease-out;
  }

  @keyframes nib-highlight-pulse {
    0% { background-color: oklch(0.488 0.243 264 / 0.25); }
    100% { background-color: transparent; }
  }

  /* Fade-out for deleted rows */
  .tree-row.nib-fading {
    opacity: 0;
    transition: opacity 0.5s ease-out;
  }

  .row-cell {
    padding-block: var(--row-pad-y, 0.25rem);
  }

  .cell-truncate {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .title-content {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    overflow: hidden;
    white-space: nowrap;
  }

  .actions-cell {
    position: relative;
    vertical-align: middle;
  }

  .actions-cell-inner {
    display: flex;
    align-items: center;
    height: 100%;
  }

  .title-text-btn {
    color: var(--foreground);
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
    cursor: pointer;
    background: none;
    border: none;
    padding: 0;
    font: inherit;
    text-align: left;
  }

  .row-add-child-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0.125rem;
    color: var(--muted-foreground);
    background: none;
    border: none;
    border-radius: var(--radius-sm);
    cursor: pointer;
    opacity: 0;
    transition: opacity 0.1s;
  }

  .tree-row:hover:not(.any-dragging) .row-add-child-btn {
    opacity: 1;
  }

  .row-add-child-btn:hover {
    color: var(--foreground);
    background-color: var(--accent);
  }
</style>
