<script lang="ts">
  import { useDrag } from "../contexts";
  import { isQueueAxis } from "../ordering/regionBand";

  const drag = useDrag();

  let count = $derived(drag.draggedIds.length);
  // The queue axis gets the badge's border as well as the row's indicator, so
  // the two halves of one affordance are recognizably the same event.
  let queue = $derived(isQueueAxis(drag.dropRegion?.axis));

  // The badge's own box, for the clamp below. 0 until the first measurement,
  // which only makes the first frame of a drag unclamped.
  let badgeWidth = $state(0);
  let badgeHeight = $state(0);

  const CURSOR_GAP = 12;
  const EDGE_MARGIN = 8;

  // Kept inside the viewport instead of being clamped BY it. The badge is
  // `position: fixed` with no ancestor establishing a containing block, so the
  // viewport is one: with only `left` set, shrink-to-fit gives it the space to
  // the RIGHT of the cursor, and a destination name is as long as a container's
  // title. Measured against the fixture in e2e/drag-affordance.test.ts: without
  // this clamp the pill runs to 1526px in a 1440px viewport, where a fixed box
  // adds no scrollable overflow and nothing scrolls to reveal it. The wrap
  // `whitespace-nowrap` prevents needs the clamp gone TOO — with it in place the
  // badge is never squeezed, so no test exercises that class on its own, and
  // `truncate` on the label is what lets a long name ellipsize rather than
  // widen. Read per pointermove, which is when a stale viewport size would show.
  let left = $derived(
    Math.max(EDGE_MARGIN, Math.min(drag.cursorX + CURSOR_GAP, window.innerWidth - badgeWidth - EDGE_MARGIN)),
  );
  let top = $derived(
    Math.max(EDGE_MARGIN, Math.min(drag.cursorY - CURSOR_GAP, window.innerHeight - badgeHeight - EDGE_MARGIN)),
  );
</script>

<!-- The sentence a release would carry out, following the cursor.
     `dropLabel` is set from an ACCEPTED plan only, so a refused target leaves
     the destination line off and the badge falls back to the count (or
     disappears entirely on a single-row drag). App.svelte's handleDrop explains
     the refusal on release, except for `drop-on-self` — the cancel gesture,
     which is silent by design and so leaves a single-row drag with no signal at
     all. -->
{#if drag.isDragging && (drag.dropLabel !== null || count > 1)}
  <div
    bind:clientWidth={badgeWidth}
    bind:clientHeight={badgeHeight}
    data-testid="drag-badge"
    class="fixed pointer-events-none flex items-center gap-2 whitespace-nowrap max-w-[min(28rem,60vw)] rounded-full border px-2 py-0.5 text-label bg-accent text-foreground {queue
      ? 'border-region-queue'
      : 'border-border'}"
    style="left: {left}px; top: {top}px; z-index: var(--z-modal);"
  >
    {#if count > 1}
      <span data-testid="drag-badge-count">{count} items</span>
    {/if}
    {#if drag.dropLabel !== null}
      <!-- The label is the part that can be arbitrarily long — a container's
           whole title — so it is what gives way when the max-width binds. -->
      <span data-testid="drag-badge-label" class="truncate">{drag.dropLabel}</span>
    {/if}
  </div>
{/if}
