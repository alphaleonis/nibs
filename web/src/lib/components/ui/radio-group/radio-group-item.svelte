<script lang="ts">
	import { RadioGroup as RadioGroupPrimitive } from "bits-ui";
	import { cn } from "$lib/utils.js";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: RadioGroupPrimitive.ItemProps = $props();
</script>

<!-- Segmented-control item: bits-ui sets role="radio" + aria-checked + data-state
     ("checked"/"unchecked"); we style the selected state via data-[state=checked].
     Label text is provided as children (its accessible name). -->
<RadioGroupPrimitive.Item
	bind:ref
	data-slot="radio-group-item"
	class={cn(
		"focus-visible:ring-ring/50 text-muted-foreground hover:text-foreground data-[state=checked]:bg-background data-[state=checked]:text-foreground data-[state=checked]:shadow-sm cursor-pointer rounded-sm px-2.5 py-1 text-sm outline-none transition-colors focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-50",
		className
	)}
	{...restProps}
/>
