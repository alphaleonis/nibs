<script lang="ts">
  import { Ban } from "@lucide/svelte";
  import { Input } from "$lib/components/ui/input/index.js";
  import { AREA_PALETTE, areaColor, paletteName } from "../areas";

  interface Props {
    /** A hex code or a bare color name; "" for none. */
    value: string;
    /** Full path of the area the color belongs to, for the accessible name. */
    path: string;
    id?: string;
  }

  let { value = $bindable(), path, id }: Props = $props();

  const hintId = $props.id();

  let selected = $derived(paletteName(value));
  let drawn = $derived(areaColor(value));
  // `areaColor` passes any word in letters, and the browser draws only the ones it knows.
  let drawable = $derived(drawn !== null && CSS.supports("color", drawn));
  let undrawable = $derived(value !== "" && !drawable);

  const label = (name: string) => name[0].toUpperCase() + name.slice(1);

  // The pressed ring sits outside an offset gap, so it reads on every swatch color.
  const SWATCH =
    "size-6 shrink-0 rounded-full border border-border outline-none focus-visible:ring-3 focus-visible:ring-ring/50 aria-pressed:ring-2 aria-pressed:ring-foreground aria-pressed:ring-offset-2 aria-pressed:ring-offset-popover";
</script>

<div class="flex flex-col gap-2">
  <div role="group" aria-label={`Swatches for ${path}`} class="grid grid-cols-7 gap-2 justify-items-center">
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
  </div>
  <div class="flex items-center gap-2">
    <!-- `areaColor` is what makes config text safe in this style. -->
    <span
      data-testid="area-color-preview"
      aria-hidden="true"
      class={`size-4 shrink-0 rounded-full border border-border ${drawable ? "" : "border-dashed"}`}
      style:background-color={drawable ? drawn : undefined}
    ></span>
    <Input
      {id}
      data-testid="area-color-input"
      bind:value
      placeholder="blue or #4488ff"
      aria-label={`Color for ${path}`}
      aria-describedby={undrawable ? hintId : undefined}
      class="h-7"
    />
  </div>
  {#if undrawable}
    <p id={hintId} data-testid="area-color-hint" class="text-caption text-muted-foreground">
      Can't draw this color.
    </p>
  {/if}
</div>
