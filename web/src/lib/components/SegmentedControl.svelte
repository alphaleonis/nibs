<script lang="ts">
  import { RadioGroup as RadioGroupPrimitive } from "bits-ui";
  import { cn } from "$lib/utils.js";

  let {
    value,
    options,
    ariaLabel,
    onchange,
    class: className = undefined,
  }: {
    value: string;
    options: { value: string; label: string }[];
    ariaLabel: string;
    onchange: (value: string) => void;
    class?: string;
  } = $props();
</script>

<!-- Segmented control skinned over the bits-ui RadioGroup primitive, which
     supplies radio roles, roving tabindex and arrow-key nav. The skin lives here,
     not in ui/radio-group, so a plain radio list can reuse the primitive.

     Unselected items keep full-strength text so they don't read as disabled. -->
<RadioGroupPrimitive.Root
  {value}
  onValueChange={(v) => v && onchange(v)}
  orientation="horizontal"
  aria-label={ariaLabel}
  class={cn(
    "inline-flex items-center gap-0.5 rounded-md border border-border bg-background p-0.5",
    className
  )}
>
  {#each options as option}
    <RadioGroupPrimitive.Item
      value={option.value}
      class="focus-visible:ring-ring/50 text-foreground data-[state=unchecked]:hover:bg-accent/50 data-[state=checked]:bg-accent data-[state=checked]:text-accent-foreground data-[state=checked]:shadow-sm cursor-pointer rounded-sm px-2.5 py-1 text-sm outline-none transition-colors focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-50"
    >
      {option.label}
    </RadioGroupPrimitive.Item>
  {/each}
</RadioGroupPrimitive.Root>
