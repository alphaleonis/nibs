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

<!-- App-level segmented control skinned over the bits-ui RadioGroup primitive.
     bits-ui sets role="radio" + aria-checked + data-state ("checked"/"unchecked")
     and gives us WAI-ARIA roving tabindex + arrow-key nav for free. The pill skin
     lives here (not in the canonical ui/radio-group scaffold) so a vanilla radio
     list can reuse the primitive without forking. -->
<RadioGroupPrimitive.Root
  {value}
  onValueChange={(v) => v && onchange(v)}
  orientation="horizontal"
  aria-label={ariaLabel}
  class={cn(
    "inline-flex items-center gap-0.5 rounded-md bg-muted p-0.5",
    className
  )}
>
  {#each options as option}
    <RadioGroupPrimitive.Item
      value={option.value}
      class="focus-visible:ring-ring/50 text-muted-foreground hover:text-foreground data-[state=checked]:bg-background data-[state=checked]:text-foreground data-[state=checked]:shadow-sm cursor-pointer rounded-sm px-2.5 py-1 text-sm outline-none transition-colors focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-50"
    >
      {option.label}
    </RadioGroupPrimitive.Item>
  {/each}
</RadioGroupPrimitive.Root>
