<script lang="ts" module>
	import type { HTMLInputAttributes } from "svelte/elements";
	import type { WithElementRef } from "$lib/utils.js";

	export type InputProps = WithElementRef<HTMLInputAttributes>;
</script>

<script lang="ts">
	import { cn } from "$lib/utils.js";

	let {
		ref = $bindable(null),
		value = $bindable(),
		class: className,
		...restProps
	}: InputProps = $props();
</script>

<!-- `rounded-lg` is intentional: this shared Input primitive matches the
     button/select primitive family radius. (Hand-styled panel inputs — TagEditor,
     ActiveNibView — use the smaller `var(--radius-md)` instead.) -->
<input
	bind:this={ref}
	bind:value
	data-slot="input"
	class={cn(
		"border-input bg-popover text-foreground placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-ring/50 h-8 w-full rounded-lg border px-2.5 text-sm outline-none transition-colors focus-visible:ring-3 disabled:pointer-events-none disabled:opacity-50",
		className
	)}
	{...restProps}
/>
