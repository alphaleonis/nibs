<script lang="ts">
  // A presentational autocomplete dropdown, generalized from the suggestion list
  // in TagEditor: an absolutely-positioned popover anchored below its positioned
  // ancestor, with an active-row highlight and mousedown-preserved focus so a
  // click commits before the anchoring input blurs. Keyboard navigation
  // (arrow/enter/esc) and the suggestion source live with the caller; this
  // component only renders items and reports selection.
  interface Props {
    items: string[];
    /** Index of the keyboard-highlighted row (-1 = none). */
    activeIndex?: number;
    onselect: (item: string, index: number) => void;
    testId?: string;
    itemTestId?: string;
  }

  let {
    items,
    activeIndex = -1,
    onselect,
    testId = "suggestions",
    itemTestId = "suggestion",
  }: Props = $props();
</script>

{#if items.length > 0}
  <ul
    data-testid={testId}
    role="listbox"
    class="absolute left-0 top-full mt-1 max-h-48 min-w-full overflow-y-auto rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-lg"
    style="z-index: var(--z-dropdown);"
  >
    {#each items as item, i (item)}
      <li role="presentation">
        <!-- Raw button: an autocomplete option row. mousedown is prevented so
             clicking it doesn't blur (and close) the input before the click
             commits — the same trick TagEditor uses. -->
        <button
          type="button"
          role="option"
          aria-selected={i === activeIndex}
          data-testid={itemTestId}
          class="block w-full rounded-sm px-2 py-1 text-left text-body hover:bg-accent hover:text-accent-foreground {i === activeIndex ? 'bg-accent text-accent-foreground' : ''}"
          onmousedown={(e) => e.preventDefault()}
          onclick={() => onselect(item, i)}
        >
          {item}
        </button>
      </li>
    {/each}
  </ul>
{/if}
