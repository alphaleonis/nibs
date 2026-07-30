<script lang="ts">
	import { DropdownMenu as DropdownMenuPrimitive } from "bits-ui";
	import { cn } from "$lib/utils.js";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: DropdownMenuPrimitive.SubContentProps = $props();
</script>

<!-- `max-w-[min(calc(20rem*var(--font-scale)),calc(100vw-1rem))]` + `overflow-x-hidden`
     mirror dropdown-menu-content's viewport guard so both container primitives share
     the same width contract for their `w-auto` sizing and horizontally clip over-long
     items. The `* var(--font-scale)` factor is part of that contract, not an extra:
     the cap has to grow with the item text or a label that fits at Medium is clipped
     at Large (nibs-grbo). Change the pair together — web/src/lib/fontScaleTokens.test.ts
     asserts both files carry the identical cap. See dropdown-menu-content.svelte. -->
<DropdownMenuPrimitive.SubContent
	bind:ref
	data-slot="dropdown-menu-sub-content"
	class={cn("data-open:animate-in data-closed:animate-out data-closed:fade-out-0 data-open:fade-in-0 data-closed:zoom-out-95 data-open:zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 ring-foreground/10 bg-popover text-popover-foreground min-w-24 max-w-[min(calc(20rem*var(--font-scale)),calc(100vw-1rem))] overflow-x-hidden rounded-lg p-1 shadow-lg ring-1 duration-100 w-auto", className)}
	{...restProps}
/>
