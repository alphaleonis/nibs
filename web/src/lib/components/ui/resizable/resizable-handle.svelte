<script lang="ts">
	import { PaneResizer, type PaneResizerProps } from "paneforge";
	import { cn } from "$lib/utils.js";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: PaneResizerProps = $props();
</script>

<PaneResizer
	bind:ref
	class={cn(
		"bg-accent hover:bg-primary relative flex w-1 cursor-col-resize items-center justify-center transition-colors",
		"focus-visible:ring-ring focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-offset-1",
		"data-[direction=vertical]:h-1 data-[direction=vertical]:w-full data-[direction=vertical]:cursor-row-resize",
		className
	)}
	{...restProps}
/>
