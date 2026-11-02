<script lang="ts">
  import { untrack } from "svelte";
  import { Ban } from "@lucide/svelte";
  import { AREA_PALETTE, areaColor, paletteName } from "../areas";

  interface Props {
    /** A hex code or a bare color name; "" for none. */
    value: string;
    /** Full path of the area the color belongs to, for the accessible name. */
    path: string;
  }

  let { value = $bindable(), path }: Props = $props();

  // A color set outside the palette gets its own swatch, so picking another stays reversible.
  const custom = untrack(() => (value !== "" && paletteName(value) === null ? value : ""));
  const customDrawn = areaColor(custom);
  // `areaColor` passes any word in letters, and the browser draws only the ones it knows.
  const customDrawable = customDrawn !== null && CSS.supports("color", customDrawn);

  let selected = $derived(paletteName(value));

  const label = (name: string) => name[0].toUpperCase() + name.slice(1);

  // The pressed ring sits outside an offset gap, so it reads on every swatch color.
  const SWATCH =
    "size-6 shrink-0 rounded-full border border-border outline-none focus-visible:ring-3 focus-visible:ring-ring/50 aria-pressed:ring-2 aria-pressed:ring-foreground aria-pressed:ring-offset-2 aria-pressed:ring-offset-popover";
</script>

<div role="group" aria-label={`Color for ${path}`} class="grid grid-cols-7 gap-2 justify-items-center">
  <button
    type="button"
    aria-label="No color"
    aria-pressed={value === ""}
    class={`${SWATCH} inline-flex items-center justify-center bg-popover text-muted-foreground`}
    onclick={() => (value = "")}
  >
    <Ban size={14} />
  </button>
  {#each AREA_PALETTE as name (name)}
    <!-- A palette name always resolves, so this style never carries config text. -->
    <button
      type="button"
      data-testid={`area-swatch-${name}`}
      aria-label={label(name)}
      aria-pressed={selected === name}
      class={SWATCH}
      style:background-color={areaColor(name)}
      onclick={() => (value = name)}
    ></button>
  {/each}
  {#if custom}
    <!-- `areaColor` is what makes config text safe in this style. -->
    <button
      type="button"
      data-testid="area-swatch-custom"
      aria-label={`Custom color ${custom}`}
      title={custom}
      aria-pressed={value === custom}
      class={`${SWATCH} ${customDrawable ? "" : "border-dashed"}`}
      style:background-color={customDrawn}
      onclick={() => (value = custom)}
    ></button>
  {/if}
</div>
