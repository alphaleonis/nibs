<script lang="ts">
	import { Pane, type PaneProps } from "paneforge";
	import { cn } from "$lib/utils.js";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: PaneProps = $props();

	let pane: ReturnType<typeof Pane>;

	export const collapse = () => pane.collapse();
	export const expand = () => pane.expand();
	export const getSize = () => pane.getSize();
	export const isCollapsed = () => pane.isCollapsed();
	export const isExpanded = () => pane.isExpanded();
	export const resize = (size: number) => pane.resize(size);
	export const getId = () => pane.getId();
</script>

<Pane bind:this={pane} bind:ref class={cn(className)} {...restProps} />
