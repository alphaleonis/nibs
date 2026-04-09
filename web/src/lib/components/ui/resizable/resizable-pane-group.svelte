<script lang="ts">
	import { PaneGroup, type PaneGroupProps } from "paneforge";
	import { cn } from "$lib/utils.js";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: PaneGroupProps = $props();
</script>

<PaneGroup bind:ref class={cn("flex h-full w-full", className)} {...restProps} />
