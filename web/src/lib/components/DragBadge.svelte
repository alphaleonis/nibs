<script lang="ts">
  import { useDrag } from "../contexts";
  import { dropTreatment, type DropTreatment } from "../ordering/regionBand";

  const drag = useDrag();

  let count = $derived(drag.draggedIds.length);
  // The border matches the row indicator's treatment. A Record, so a new
  // DropTreatment fails to compile until it has a border.
  const BORDER: Record<DropTreatment, string> = {
    parent: "border-border",
    queue: "border-region-queue",
    assign: "border-region-assign",
  };
  let treatment = $derived(dropTreatment(drag.dropAccepted));
  let border = $derived(treatment === null ? "border-border" : BORDER[treatment]);

  // 0 until first measured, so a drag's first frame is unclamped.
  let badgeWidth = $state(0);
  let badgeHeight = $state(0);

  const CURSOR_GAP = 12;
  const EDGE_MARGIN = 8;

  // Clamp inside the viewport: a fixed box with only `left` set overflows to the
  // right of the cursor for a long destination name, and nothing scrolls to reveal
  // it (e2e/drag-affordance.test.ts).
  let left = $derived(
    Math.max(EDGE_MARGIN, Math.min(drag.cursorX + CURSOR_GAP, window.innerWidth - badgeWidth - EDGE_MARGIN)),
  );
  let top = $derived(
    Math.max(EDGE_MARGIN, Math.min(drag.cursorY - CURSOR_GAP, window.innerHeight - badgeHeight - EDGE_MARGIN)),
  );
</script>

<!-- What a release would do, following the cursor. `dropLabel` exists only for
     an accepted drop, so over a refused target the badge shows just the count,
     or nothing on a single-row drag. -->
{#if drag.isDragging && (drag.dropLabel !== null || count > 1)}
  <div
    bind:clientWidth={badgeWidth}
    bind:clientHeight={badgeHeight}
    data-testid="drag-badge"
    class="fixed pointer-events-none flex items-center gap-2 whitespace-nowrap max-w-[min(28rem,60vw)] rounded-full border px-2 py-0.5 text-label bg-accent text-foreground {border}"
    style="left: {left}px; top: {top}px; z-index: var(--z-modal);"
  >
    {#if count > 1}
      <span data-testid="drag-badge-count">{count} items</span>
    {/if}
    {#if drag.dropLabel !== null}
      <!-- The label is what truncates when max-width binds. -->
      <span data-testid="drag-badge-label" class="truncate">{drag.dropLabel}</span>
    {/if}
  </div>
{/if}
