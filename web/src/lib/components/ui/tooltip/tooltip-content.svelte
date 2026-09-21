<script lang="ts">
	import { Tooltip as TooltipPrimitive } from "bits-ui";
	import TooltipPortal from "./tooltip-portal.svelte";
	import { cn, type WithoutChildrenOrChild } from "$lib/utils.js";
	import type { ComponentProps, Snippet } from "svelte";

	let {
		ref = $bindable(null),
		class: className,
		sideOffset = 4,
		portalProps,
		children,
		...restProps
	}: TooltipPrimitive.ContentProps & {
		portalProps?: WithoutChildrenOrChild<ComponentProps<typeof TooltipPortal>>;
		children?: Snippet;
	} = $props();
</script>

<TooltipPortal {...portalProps}>
	<!-- DEVIATION from the bits/shadcn default (which sets no max-width): the text is
	     arbitrary nib data — a title, a joined id list, a document path — and nothing
	     bounds it, so an uncapped `w-fit` surface fills the viewport over content the
	     pointer cannot reach until it moves. Same bound and same reason as
	     dropdown-menu-content.svelte, including `* var(--font-scale)` so a rem cap does
	     not clip at the Large font preference; `wrap-break-word` because a path or an
	     id has no space to wrap at. Keep this override on any shadcn re-sync. -->
	<TooltipPrimitive.Content
		bind:ref
		data-slot="tooltip-content"
		{sideOffset}
		class={cn(
			"bg-popover text-popover-foreground ring-foreground/10 data-open:animate-in data-closed:animate-out data-closed:fade-out-0 data-open:fade-in-0 data-closed:zoom-out-95 data-open:zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 z-50 w-fit max-w-[min(calc(20rem*var(--font-scale)),calc(100vw-1rem))] wrap-break-word rounded-md px-2.5 py-1 text-xs shadow-md ring-1 duration-100 outline-none",
			className
		)}
		{...restProps}
	>
		{@render children?.()}
	</TooltipPrimitive.Content>
</TooltipPortal>
