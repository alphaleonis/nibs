<script lang="ts" generics="T">
  // Presentational autocomplete dropdown, positioned below its nearest positioned
  // ancestor. The caller owns keyboard navigation and the suggestion source; this
  // renders items and reports selection.
  import type { Snippet } from "svelte";

  interface Props {
    items: T[];
    /** Index of the keyboard-highlighted row (-1 = none). */
    activeIndex?: number;
    onselect: (item: T, index: number) => void;
    testId?: string;
    itemTestId?: string;
    /** Custom row content; defaults to the item rendered as text. */
    item?: Snippet<[T, number]>;
    /** Stable `{#each}` key; defaults to the item itself. */
    itemKey?: (item: T) => string | number;
  }

  let {
    items,
    activeIndex = -1,
    onselect,
    testId = "suggestions",
    itemTestId = "suggestion",
    item,
    itemKey,
  }: Props = $props();
</script>

{#if items.length > 0}
  <ul
    data-testid={testId}
    role="listbox"
    class="absolute left-0 top-full mt-1 max-h-48 min-w-full overflow-y-auto rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-lg"
    style="z-index: var(--z-dropdown);"
  >
    {#each items as it, i (itemKey ? itemKey(it) : it)}
      <li role="presentation">
        <!-- Raw button: an option row. mousedown is prevented so the input does
             not blur (and close) before the click commits. -->
        <button
          type="button"
          role="option"
          aria-selected={i === activeIndex}
          data-testid={itemTestId}
          class="block w-full rounded-sm px-2 py-1 text-left text-body hover:bg-accent hover:text-accent-foreground {i === activeIndex ? 'bg-accent text-accent-foreground' : ''}"
          onmousedown={(e) => e.preventDefault()}
          onclick={() => onselect(it, i)}
        >
          {#if item}{@render item(it, i)}{:else}{it}{/if}
        </button>
      </li>
    {/each}
  </ul>
{/if}
