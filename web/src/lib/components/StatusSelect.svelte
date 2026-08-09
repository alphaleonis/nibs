<script lang="ts">
  import { STATUS_WORKFLOW } from "../constants";
  import * as Select from "$lib/components/ui/select/index.js";
  import StatusIcon from "./StatusIcon.svelte";

  interface Props {
    value: string;
    onchange: (status: string) => void;
    testId?: string;
    disabled?: boolean;
  }

  let { value, onchange, testId = "status-select", disabled = false }: Props = $props();
</script>

<Select.Root type="single" {value} {disabled} onValueChange={(v) => { if (v) onchange(v); }}>
  <Select.Trigger data-testid={testId} size="default" class="flex-1">
    <StatusIcon status={value} />
    {value}
  </Select.Trigger>
  <Select.Content>
    {#each STATUS_WORKFLOW as s}
      <Select.Item value={s}>
        <StatusIcon status={s} />
        {s}
      </Select.Item>
    {/each}
  </Select.Content>
</Select.Root>
