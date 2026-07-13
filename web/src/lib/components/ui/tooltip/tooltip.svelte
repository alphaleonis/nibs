<script lang="ts">
	import { Tooltip as TooltipPrimitive } from "bits-ui";
	import TooltipProvider from "./tooltip-provider.svelte";

	// Self-contained Root: it wraps its own Provider so every `<Tooltip.Root>`
	// works standalone, without an app-level `<Tooltip.Provider>`. bits-ui's
	// Tooltip.Root throws "Context Tooltip.Provider not found" without a Provider
	// ancestor, and Toolbar/SettingsSheet render standalone in many tests — a
	// self-contained Root keeps those render trees valid with zero extra plumbing.
	let {
		open = $bindable(false),
		delayDuration = 300,
		...restProps
	}: TooltipPrimitive.RootProps = $props();
</script>

<TooltipProvider {delayDuration}>
	<TooltipPrimitive.Root bind:open {...restProps} />
</TooltipProvider>
