<script lang="ts">
  import { STATUSES } from "../constants";
  import * as Select from "$lib/components/ui/select/index.js";

  interface Props {
    value: string;
    onchange: (status: string) => void;
    testId?: string;
    disabled?: boolean;
  }

  let { value, onchange, testId = "status-select", disabled = false }: Props = $props();
</script>

<Select.Root type="single" {value} {disabled} onValueChange={(v) => { if (v) onchange(v); }}>
  <Select.Trigger data-testid={testId} size="sm" class="flex-1">
    {value}
  </Select.Trigger>
  <Select.Content>
    {#each STATUSES as s}
      <Select.Item value={s}>{s}</Select.Item>
    {/each}
  </Select.Content>
</Select.Root>
