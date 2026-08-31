<script lang="ts">
  import { DEFAULT_BLOCKED_EMPHASIS, DEFAULT_OPEN_DETAIL_ON } from "../types";
  import type { TreeTableNib, BlockedEmphasis, OpenDetailGesture } from "../types";
  import { ALL_COLUMN_KEYS } from "../columns";
  import type { ColumnKey, RowContext } from "../columns";
  import { Plus } from "@lucide/svelte";
  import { canHaveChildren } from "../typeHierarchy";
  import { useSelection, useDrag } from "../contexts";
  import { useColumnAdapters } from "../ColumnAdapters.svelte";

  import type { DropZone } from "../drag.svelte";
  import { isQueueAxis, type BandAxis } from "../ordering/regionBand";

  interface Props {
    nib: TreeTableNib;
    depth?: number;
    hasChildren?: boolean;
    dimmed?: boolean;
    collapsed?: boolean;
    parentNib?: TreeTableNib | null;
    visibleColumns?: ColumnKey[];
    columnOrder?: ColumnKey[];
    draggable?: boolean;
    highlighted?: boolean;
    fading?: boolean;
    blockedEmphasis?: BlockedEmphasis;
    openDetailOn?: OpenDetailGesture;
    /** The axis of the region boundary running above this row, or null for none. */
    regionBand?: BandAxis | null;
  }

  let {
    nib,
    depth = 0,
    hasChildren = false,
    dimmed = false,
    collapsed = false,
    parentNib = null,
    visibleColumns = [...ALL_COLUMN_KEYS],
    columnOrder = [...ALL_COLUMN_KEYS],
    draggable = false,
    highlighted = false,
    fading = false,
    blockedEmphasis = DEFAULT_BLOCKED_EMPHASIS,
    openDetailOn = DEFAULT_OPEN_DETAIL_ON,
    regionBand = null,
  }: Props = $props();

  const selection = useSelection();
  const drag = useDrag();
  // The per-column cell renderers (pure snippets of RowContext). Provided by
  // <ColumnAdapters> in the app and by makeTestContext in tests.
  const adapters = useColumnAdapters();

  // Render cells in the per-view column order, filtered to the visible set. The
  // order (from TreeTable's resolved columnOrder) carries every ColumnKey, so
  // filtering by visibility yields the ordered visible cells; TreeTable's header
  // loop iterates the identical sequence so cells stay under their headers.
  let orderedVisibleColumns = $derived(columnOrder.filter((k) => visibleColumns.includes(k)));

  // The bag each cell adapter reads. Cells are pure functions of this — they
  // touch no selection/drag context — so ambient row state stays on the <tr>.
  let rowCtx: RowContext = $derived({ nib, depth, parentNib, hasChildren, collapsed, blockedEmphasis });

  // Computed from context + nib.id. `selectedIds` and `selectedNibId` are two
  // independent facts, so they get two independent channels here:
  //
  //   inSelection — the row is in the bulk-action set, i.e. what a delete or a
  //     bulk status change would consume. Drives the `.active` fill and
  //     `aria-selected`, so the destructive target set is legible in both the
  //     visual and the assistive channel. Reading `selectedNibId` here too would
  //     make the two-row and one-row action sets render identically.
  //   opened — the detail panel is showing this row. Drives the `.opened`
  //     leading accent and `aria-current`.
  let inSelection = $derived(selection.selectedIds.has(nib.id));
  let opened = $derived(selection.selectedNibId === nib.id);
  // The VISUAL accent is "double"-only, while `opened` above stays true in both
  // modes. Under "single" the panel follows the selection, so on an ordinary
  // click the open row is also the selected row and the bar would repeat what
  // the fill already says on every click — redundant chrome for the default
  // mode. The two channels are allowed to disagree because they cost different
  // things: `aria-current` is free and still tells assistive tech which row the
  // panel is showing, so it is NOT gated.
  //
  // Accepted gap: "single" can still put an open row outside the action set —
  // `clearAfterMutation` empties the set while leaving a panel open on a nib the
  // mutation did not touch, and `retainOnly` prunes the set on a filter change.
  // Such a row carries no visual marker at all. Tolerated because the detail
  // panel itself names the nib it is showing.
  let showOpenAccent = $derived(openDetailOn === "double" && opened);
  let focused = $derived(selection.focusedNibId === nib.id);
  let isDragged = $derived(drag.isDraggedItem(nib.id));
  let anyDragging = $derived(drag.isDragging);
  let isDropTarget = $derived(drag.dropTargetId === nib.id);
  let dropZone: DropZone | null = $derived(isDropTarget ? drag.dropZone : null);
  let dropValid = $derived(isDropTarget ? drag.dropValid : false);
  // Which list the accepted drop writes in. Only the axis reaches the styling —
  // a queue move is the one that does not touch a parent link, and drawing it in
  // the parent-axis color is the confusion this seam exists to remove.
  let dropQueue = $derived(dropValid && isQueueAxis(drag.dropRegion?.axis));

  const isBlocked = $derived(nib.blockedByIds.length > 0);
  // `pill-dim` additionally dims the whole row. Gated off during drag, while a
  // drop target, and during the change-pulse so its 0.6 opacity doesn't mute
  // those affordances. This is only the *blocked-dim* trigger — the resolved
  // row opacity is single-sourced by `rowOpacity` below.
  const blockedDim = $derived(
    blockedEmphasis === "pill-dim" && isBlocked && !isDragged && !isDropTarget && !highlighted,
  );

  // Single source of truth for the row opacity, applied inline below. Multiple
  // states can be active at once, so precedence is made explicit here rather
  // than left to CSS inline-vs-class specificity + stylesheet declaration order
  // (which cannot express this precedence reliably — leaving it to CSS silently
  // kills `.blocked-dim` and puts the delete fade-out one reorder away from
  // breaking). Precedence, strongest first:
  //   fading (0)      — a deleted row MUST fully fade to 0; it wins over all.
  //   dragged (0.3)   — the dragged source row recedes while in flight.
  //   dimmed (0.4)    — filter-context fade for non-matching rows.
  //   blocked-dim (0.6) — blocked work recedes; weakest dim, yields to the above.
  //   normal (1).
  // Only `fading` carries a transition (see `.nib-fading` in the style block);
  // every other rank is instant. The value is applied inline on the row, and the
  // normal rank (1) is written as *no* inline opacity (the CSS default) so an
  // undimmed row carries no inline opacity attribute at all.
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
  class:drop-queue={dropQueue}
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

  <!-- Data columns: rendered by the per-column cell adapters in canonical order,
       filtered to the visible set. Each {@render} emits the column's <td>
       (testid / classes / inline style live in ColumnAdapters.svelte). -->
  {#each orderedVisibleColumns as key (key)}
    {@render adapters[key].cell(rowCtx)}
  {/each}
</tr>

<style>
  .tree-row {
    user-select: none;
    position: relative;
    /* Gutter for the open-row accent below. Reserved on EVERY row (transparent
       here) so switching a row to "open" only recolors it — turning the border
       on per-row would shift the first column by its width. Sized so the accent
       still reads as a row state at the table's outer edge, where it competes
       with the focus ring for attention rather than sitting beside it. */
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

  /* The row open in the detail panel: the leading-edge accent ONLY, no fill.
     Fill is `.active`'s channel and means "in the bulk-action set", so an open
     row that a delete would not consume must not carry one — a fill here would
     also render it more prominently than the rows the action actually targets.
     Shape rather than alpha keeps the two states independently readable when a
     row is both (see also `aria-current` / `aria-selected` on the row). Every
     row reserves the gutter transparent, so coloring it shifts nothing.

     The color is `--row-open` (amber), NOT `--ring`. `--ring` paints the
     keyboard focus outline, which follows the last click; in the same blue this
     accent read as a weaker copy of that outline, so the row the panel was
     actually showing looked unmarked while the clicked row looked open. Amber is
     the one accent no theme uses for focus. Keep it off the focus hue.

     Deliberately not a box-shadow ring: the keyboard focus ring
     (`.tree-row.focused` in app.css) and all three drop-zone indicators are
     box-shadows, and a component-scoped rule outranks the global one — a ring
     here would silently swallow the focus ring on the row that is both open and
     focused, which is the common case. Do not convert this to a ring.

     Declared before the `.drop-*` rules so a drag target still wins over it, and
     the accent survives `.tree-row:hover` (which repaints only the background)
     — the case where the pointer parks on the row it just opened. */
  .tree-row.opened {
    border-inline-start-color: var(--row-open);
  }

  /* Drag / pill-dim state markers — no styling here; opacity is single-sourced by
     the `rowOpacity` derived in the script block and applied inline on the row.
     `.dragged` is retained because useTreeDrag strips it from the drag-image clone
     (useTreeDrag.svelte.ts) so the ghost isn't faded. `.blocked-dim` has no CSS or
     JS consumer; it stays only as a state marker that tests assert to pin the
     blockedDim gating logic (drag/drop/pulse suppression). */

  /* The boundary between one ordering region and the next, drawn above the row
     that opens the new one (`regionBandAt` decides which rows those are). Not
     reserved-transparent like the accent gutter above: a band is settled by the
     row list rather than by hover or selection, so it can afford the 1px of
     height it adds.

     A border rather than the box-shadow the drop states use, because a row can
     be both at once and box-shadow does not compose — two rules setting it
     cannot both apply, so whichever won would erase the other while the pointer
     is over the row. */
  .tree-row.region-band {
    border-block-start: 1px solid var(--border);
  }

  /* A seam a milestone queue is on either side of, in the queue's own color and
     a touch heavier: where a queue's run ENDS is the one place nothing else
     marks it — a run that opens by descending is left to the indent, which is
     why `regionBandAt` draws only the closing side — so it is worth spotting
     from across the table. Written as the compound of
     both classes — which is how the row always carries it — so it outranks the
     rule above rather than depending on following it.

     One row can carry this AND `.drop-before.drop-queue` below, which paints 2px
     of the same hue on the inside of the same edge — 4px of cyan across two
     boxes, where the parent axis pairs two different tokens (--border band,
     --ring indicator). Unreached until a lens mints a milestone region
     (nibs-iaqd), and left alone until it can be seen rather than reasoned about:
     the two are not distinguishable from the CSS alone. */
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

  /* The same three indicators for a drop that writes on the MILESTONE axis,
     which moves a row inside a queue and changes no parent link. Each selector
     carries one class more than its parent-axis form above, so it outranks it
     whatever the declaration order. */
  .tree-row.drop-before.drop-queue {
    box-shadow: inset 0 2px 0 0 var(--region-queue);
  }

  .tree-row.drop-after.drop-queue {
    box-shadow: inset 0 -2px 0 0 var(--region-queue);
  }

  /* `color-mix` where the parent-axis rule four lines above writes a literal
     alpha channel: that shortcut needs the color spelled out, and this one is
     reached through a `var()`, which relative-color syntax would be needed to
     add an alpha to. `color-mix` is what the codebase already reaches for in
     that position (ActiveNibView.svelte, App.svelte). */
  .tree-row.drop-reparent.drop-queue {
    background-color: color-mix(in oklab, var(--region-queue), transparent 88%);
    box-shadow: inset 0 0 0 1px var(--region-queue);
  }

  /* .tree-row.drop-invalid intentionally has no styling — invalid drop targets
     get no highlight. The class exists for drop-zone logic, not for CSS.

     What a user reads instead is an absence: the drag badge names a destination
     only for an accepted plan (DragBadge.svelte), so it names none over a target
     nothing can happen on. Release hands the refusal to App.svelte's handleDrop,
     which raises its message for every reason but `drop-on-self` — releasing on
     the row you grabbed is a cancel, and a cancel says nothing. */

  /* Real-time change highlight — brief accent background pulse */
  .tree-row.nib-highlighted {
    animation: nib-highlight-pulse 1s ease-out;
  }

  @keyframes nib-highlight-pulse {
    0% { background-color: oklch(0.488 0.243 264 / 0.25); }
    100% { background-color: transparent; }
  }

  /* Fade-out for deleted rows. The target opacity (0) comes from `rowOpacity`
     (inline); this class carries ONLY the transition so that when `fading`
     flips true the inline opacity animates 0.5s to 0. All other opacity ranks
     are instant because no other state adds a transition. */
  .tree-row.nib-fading {
    transition: opacity 0.5s ease-out;
  }

  /* Actions cell shares .row-cell with the data cells; the data-cell styles
     (.cell-truncate, .title-content, .type-icon-gap, .title-text-btn) live in
     ColumnAdapters.svelte, which now owns that markup. */
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
    /* Inset the button from the row's left edge by the width of the
       selected/focused row's inset ring (box-shadow: inset 0 0 0 2px var(--ring)
       on .tree-row.focused). Without this the button sits flush at x=0 and its
       rounded --accent hover fill (and any focus ring) paints OVER the ring,
       reading as bleeding past the row border. 2px == the widest
       ring, so the fill starts exactly at the ring's inner edge — inside it. */
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

  /* Contained inset focus ring — mirrors .scroll-container:focus-visible in
     app.css (outline + negative outline-offset). outline-offset: -2px draws the
     ring INSIDE the button box so it can never bleed past the row border, unlike
     the UA default outline it replaces. Reveal on keyboard focus so the ring is
     visible even when the row isn't hovered. */
  .row-add-child-btn:focus-visible {
    opacity: 1;
    outline: 2px solid var(--ring);
    outline-offset: -2px;
  }
</style>
