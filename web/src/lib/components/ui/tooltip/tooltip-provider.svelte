<script lang="ts">
	import { Tooltip as TooltipPrimitive } from "bits-ui";

	// DEVIATION from the bits-ui Provider defaults (delayDuration 700->300 for a
	// snappier hover reveal; disableCloseOnTriggerClick false->true and
	// disableHoverableContent false->true because these triggers double as
	// click-to-open dropdown/panel controls — hovering the content must not keep it
	// open and clicking the trigger must not immediately reopen the tooltip). Keep
	// this override on any shadcn re-sync.
	//
	// skipDelayDuration is deliberately NOT overridden here: Tooltip.Root wraps its
	// own per-instance Provider (see tooltip.svelte), so bits-ui's cross-trigger
	// skip-delay grace period never spans two Roots and no skip-delay grouping can
	// fire. Pinning a value would imply behavior that never occurs, so we fall
	// through to the bits-ui default instead.
	let {
		delayDuration = 300,
		disableCloseOnTriggerClick = true,
		disableHoverableContent = true,
		...restProps
	}: TooltipPrimitive.ProviderProps = $props();
</script>

<TooltipPrimitive.Provider
	{delayDuration}
	{disableCloseOnTriggerClick}
	{disableHoverableContent}
	{...restProps}
/>
