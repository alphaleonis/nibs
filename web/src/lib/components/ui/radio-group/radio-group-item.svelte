<!-- Canonical shadcn radio, generalized in nibs-qj7m out of the SettingsSheet
     segmented-pill skin (now SegmentedControl.svelte). It was originally reserved
     for nibs-vmaq's theme selector, but that shipped as a Select dropdown
     (ThemeSelect.svelte, consistent with StatusSelect/TypeSelect) rather than a
     radio group — so this remains a canonical shadcn scaffold with no current
     production consumer; do not delete as "unused". Label each item via
     aria-label / an external <Label>, not children — the indicator dot owns the
     children snippet here. -->
<script lang="ts">
	import { RadioGroup as RadioGroupPrimitive } from "bits-ui";
	import { cn, type WithoutChildrenOrChild } from "$lib/utils.js";
	import CircleIcon from "@lucide/svelte/icons/circle";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: WithoutChildrenOrChild<RadioGroupPrimitive.ItemProps> = $props();
</script>

<RadioGroupPrimitive.Item
	bind:ref
	data-slot="radio-group-item"
	class={cn(
		"border-input text-primary focus-visible:border-ring focus-visible:ring-ring/50 aspect-square size-4 shrink-0 rounded-full border shadow-xs transition-[color,box-shadow] outline-none focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50",
		className
	)}
	{...restProps}
>
	{#snippet children({ checked })}
		<span
			data-slot="radio-group-item-indicator"
			class="relative flex items-center justify-center"
		>
			{#if checked}
				<CircleIcon
					class="fill-primary absolute top-1/2 left-1/2 size-2 -translate-x-1/2 -translate-y-1/2"
				/>
			{/if}
		</span>
	{/snippet}
</RadioGroupPrimitive.Item>
