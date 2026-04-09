<script lang="ts">
  import { ESTIMATES, ESTIMATE_LABELS } from "../constants";
  import * as Select from "$lib/components/ui/select/index.js";

  interface Props {
    value: string;
    onchange: (estimate: string) => void;
    testId?: string;
    disabled?: boolean;
  }

  let { value, onchange, testId = "estimate-select", disabled = false }: Props = $props();
</script>

<Select.Root type="single" value={value || "__none__"} {disabled} onValueChange={(v) => { if (v) onchange(v === "__none__" ? "" : v); }}>
  <Select.Trigger data-testid={testId} size="sm" class="flex-1">
    {value ? (ESTIMATE_LABELS[value] ?? value) : "None"}
  </Select.Trigger>
  <Select.Content>
    <Select.Item value="__none__">None</Select.Item>
    {#each ESTIMATES as e}
      <Select.Item value={e}>{ESTIMATE_LABELS[e]}</Select.Item>
    {/each}
  </Select.Content>
</Select.Root>
