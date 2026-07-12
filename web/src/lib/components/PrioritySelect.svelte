<script lang="ts">
  import { PRIORITIES } from "../constants";
  import * as Select from "$lib/components/ui/select/index.js";
  import { priorityIndicators } from "../badges";

  interface Props {
    value: string;
    onchange: (priority: string) => void;
    testId?: string;
    disabled?: boolean;
  }

  let { value, onchange, testId = "priority-select", disabled = false }: Props = $props();

  // A nib always has an effective priority; an unset value shows as "normal",
  // matching the TUI (which builds its list solely from config.DefaultPriorities).
  let effective = $derived(value || "normal");
  let displayIndicator = $derived(priorityIndicators[effective]);
</script>

<Select.Root type="single" value={effective} {disabled} onValueChange={(v) => { if (v) onchange(v); }}>
  <Select.Trigger data-testid={testId} size="sm" class="flex-1">
    {#if displayIndicator}
      <span class="inline-block w-3.5 text-center text-xs font-bold" style="color: {displayIndicator.color};">{displayIndicator.symbol}</span>
    {/if}
    {effective}
  </Select.Trigger>
  <Select.Content>
    {#each PRIORITIES as p}
      {@const ind = priorityIndicators[p]}
      <Select.Item value={p}>
        {#if ind}
          <span class="inline-block w-3.5 text-center text-xs font-bold" style="color: {ind.color};">{ind.symbol}</span>
        {:else}
          <span class="inline-block w-3.5"></span>
        {/if}
        {p}
      </Select.Item>
    {/each}
  </Select.Content>
</Select.Root>
