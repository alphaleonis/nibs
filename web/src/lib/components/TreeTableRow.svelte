<script lang="ts">
  import { DEFAULT_BLOCKED_EMPHASIS, DEFAULT_OPEN_DETAIL_ON } from "../types";
  import type { TreeTableNib, BlockedEmphasis, OpenDetailGesture } from "../types";
  import { ALL_COLUMN_KEYS } from "../columns";
  import type { ColumnKey, RowContext } from "../columns";
  import type { RowSection } from "../tableData";
  import { Plus } from "@lucide/svelte";
  import { canHaveChildren } from "../typeHierarchy";
  import { useSelection, useDrag } from "../contexts";
  import { useColumnAdapters } from "../ColumnAdapters.svelte";

  import type { DropZone } from "../drag.svelte";
  import { dropTreatment, isQueueAxis, type BandAxis } from "../ordering/regionBand";

  interface Props {
    nib: TreeTableNib;
    depth?: number;
    hasChildren?: boolean;
    dimmed?: boolean;
    collapsed?: boolean;
    parentNib?: TreeTableNib | null;
    milestoneNib?: TreeTableNib | null;
    visibleColumns?: ColumnKey[];
    columnOrder?: ColumnKey[];
    draggable?: boolean;
    highlighted?: boolean;
    fading?: boolean;
    blockedEmphasis?: BlockedEmphasis;
    openDetailOn?: OpenDetailGesture;
    /** The axis of the region boundary running above this row, or null for none. */
    regionBand?: BandAxis | null;
    /** The section this row draws, or null — `RowData.drawsSection`. */
    drawsSection?: RowSection | null;
  }

  let {
    nib,
    depth = 0,
    hasChildren = false,
    dimmed = false,
    collapsed = false,
    parentNib = null,
    milestoneNib = null,
    visibleColumns = [...ALL_COLUMN_KEYS],
    columnOrder = [...ALL_COLUMN_KEYS],
    draggable = false,
    highlighted = false,
    fading = false,
    blockedEmphasis = DEFAULT_BLOCKED_EMPHASIS,
    openDetailOn = DEFAULT_OPEN_DETAIL_ON,
    regionBand = null,
    drawsSection = null,
  }: Props = $props();

  const selection = useSelection();
  const drag = useDrag();
  // Provided by <ColumnAdapters> in the app and by makeTestContext in tests.
  const adapters = useColumnAdapters();

  // The same sequence as TreeTable's header loop, so cells stay under their headers.
  let orderedVisibleColumns = $derived(columnOrder.filter((k) => visibleColumns.includes(k)));

  // Cells are pure functions of this bag; ambient row state stays on the <tr>.
  let rowCtx: RowContext = $derived({ nib, depth, parentNib, milestoneNib, hasChildren, collapsed, blockedEmphasis, drawsSection });

  // Two independent channels. inSelection is the bulk-action set (`.active`,
  // aria-selected); do not fold `selectedNibId` into it. opened means the detail
  // panel shows this row (aria-current, and the `.opened` accent).
  let inSelection = $derived(selection.selectedIds.has(nib.id));
  let opened = $derived(selection.selectedNibId === nib.id);
  // The visual accent is "double"-only: in "single" the open row is normally the
  // selected row, which the fill already marks. aria-current is not gated. In
  // "single", an open row outside the selection (after clearAfterMutation or
  // retainOnly) gets no visual marker; the panel names it.
  let showOpenAccent = $derived(openDetailOn === "double" && opened);
  let focused = $derived(selection.focusedNibId === nib.id);
  let isDragged = $derived(drag.isDraggedItem(nib.id));
  let anyDragging = $derived(drag.isDragging);
  let isDropTarget = $derived(drag.dropTargetId === nib.id);
  let dropZone: DropZone | null = $derived(isDropTarget ? drag.dropZone : null);
  let dropValid = $derived(isDropTarget ? drag.dropValid : false);
  // Decided from the plan's kind: `isQueueAxis` answers false for a missing axis,
  // which would color an assignment like a parent-axis drop.
  let treatment = $derived(dropValid ? dropTreatment(drag.dropAccepted) : null);

  const isBlocked = $derived(nib.blockedByIds.length > 0);
  // Suppressed while dragged, a drop target, or pulsing, so the dim does not mute those.
  const blockedDim = $derived(
    blockedEmphasis === "pill-dim" && isBlocked && !isDragged && !isDropTarget && !highlighted,
  );

  // The row's opacity, applied inline. Precedence in JS, not CSS order:
  // fading 0, dragged 0.3, dimmed 0.4, blocked-dim 0.6, else no inline opacity.
  const rowOpacity = $derived(
    fading ? 0 :
    isDragged ? 0.3 :
    dimmed ? 0.4 :
    blockedDim ? 0.6 :
    1,
  );
</script>

<tr
  data-testid="tree-row"
  class="tree-row"
  class:active={inSelection}
  class:opened={showOpenAccent}
  class:focused={focused}
  class:draggable={draggable}
  class:any-dragging={anyDragging}
  class:dragged={isDragged}
  class:drop-before={isDropTarget && dropZone === "before" && dropValid}
  class:drop-after={isDropTarget && dropZone === "after" && dropValid}
  class:drop-reparent={isDropTarget && dropZone === "reparent" && dropValid}
  class:drop-invalid={isDropTarget && !dropValid}
  class:drop-queue={treatment === "queue"}
  class:drop-assign={treatment === "assign"}
  class:region-band={regionBand !== null}
  class:region-band-queue={isQueueAxis(regionBand)}
  class:nib-highlighted={highlighted}
  class:nib-fading={fading}
  class:blocked-dim={blockedDim}
  data-nib-id={nib.id}
  aria-selected={inSelection}
  aria-current={opened ? "true" : undefined}
  style={rowOpacity < 1 ? `opacity: ${rowOpacity};` : ""}
>
  <!-- Actions column -->
  <td class="actions-cell row-cell">
    <div class="actions-cell-inner">
      {#if canHaveChildren(nib.type)}
        <!-- Raw button so this component's scoped CSS styles it; clicks are delegated via data-action. -->
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

  <!-- Each {@render} emits the column's <td> (see ColumnAdapters.svelte). -->
  {#each orderedVisibleColumns as key (key)}
    {@render adapters[key].cell(rowCtx)}
  {/each}
</tr>

<style>
  .tree-row {
    user-select: none;
    position: relative;
    /* Gutter for the open-row accent, reserved on every row so opening one does
       not shift the first column. */
    border-inline-start: 5px solid transparent;
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

  /* The row open in the detail panel: leading-edge accent only. Fill is `.active`,
     the bulk-action set. Colored `--row-open`, off the `--ring` focus hue.

     Not a box-shadow: the focus ring (`.tree-row.focused` in app.css) is one,
     and a scoped box-shadow here would override it on an open, focused row. */
  .tree-row.opened {
    border-inline-start-color: var(--row-open);
  }

  /* `.dragged` and `.blocked-dim` are unstyled: opacity comes from `rowOpacity`.
     useTreeDrag strips `.dragged` from the drag-image clone; tests assert
     `.blocked-dim`. */

  /* The boundary between ordering regions, above the row that opens the new one.
     A border, not a box-shadow: the row can also be a drop target, and two
     box-shadows do not compose. */
  .tree-row.region-band {
    border-block-start: 1px solid var(--border);
  }

  /* A seam beside a milestone queue: heavier, in the queue color. The row always
     carries both classes; the compound outranks the rule above. */
  .tree-row.region-band.region-band-queue {
    border-block-start: 2px solid var(--region-queue);
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

  /* Milestone-axis drops. One extra class outranks each parent-axis form above. */
  .tree-row.drop-before.drop-queue {
    box-shadow: inset 0 2px 0 0 var(--region-queue);
  }

  .tree-row.drop-after.drop-queue {
    box-shadow: inset 0 -2px 0 0 var(--region-queue);
  }

  /* A queue band and a queue drop-before line overlap on the top edge, reading as
     one heavier rule, so the band yields to a neutral hairline while the drop is
     aimed at it. drop-after paints the bottom edge and never meets the band. */
  .tree-row.region-band.region-band-queue.drop-before.drop-queue {
    border-block-start: 1px solid var(--border);
  }

  /* color-mix: an alpha cannot be written onto a var() color directly. */
  .tree-row.drop-reparent.drop-queue {
    background-color: color-mix(in oklab, var(--region-queue), transparent 88%);
    box-shadow: inset 0 0 0 1px var(--region-queue);
  }

  /* An assignment writes no position, so it has only the "into" indicator. */
  .tree-row.drop-reparent.drop-assign {
    background-color: color-mix(in oklab, var(--region-assign), transparent 88%);
    box-shadow: inset 0 0 0 1px var(--region-assign);
  }

  /* `.drop-invalid` is unstyled: a refused target gets no highlight, and
     DragBadge names no destination for it. */

  /* Real-time change highlight — brief accent background pulse */
  .tree-row.nib-highlighted {
    animation: nib-highlight-pulse 1s ease-out;
  }

  @keyframes nib-highlight-pulse {
    0% { background-color: oklch(0.488 0.243 264 / 0.25); }
    100% { background-color: transparent; }
  }

  /* Deleted rows: `rowOpacity` sets 0 inline; this adds only the transition. */
  .tree-row.nib-fading {
    transition: opacity 0.5s ease-out;
  }

  /* Shared with the data cells, whose other styles live in ColumnAdapters.svelte. */
  .row-cell {
    padding-block: var(--row-pad-y, 0.25rem);
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

  .row-add-child-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    /* Inset by the focused row's 2px ring so the hover fill does not paint over it. */
    margin-inline: 2px;
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

  /* Inset focus ring, so it stays inside the row; shown without row hover. */
  .row-add-child-btn:focus-visible {
    opacity: 1;
    outline: 2px solid var(--ring);
    outline-offset: -2px;
  }
</style>
