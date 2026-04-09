<script lang="ts">
  import { PRIORITIES } from "../constants";
  import * as Select from "$lib/components/ui/select/index.js";

  interface Props {
    value: string;
    onchange: (priority: string) => void;
    testId?: string;
    disabled?: boolean;
  }

  let { value, onchange, testId = "priority-select", disabled = false }: Props = $props();
</script>

<Select.Root type="single" value={value || "__none__"} {disabled} onValueChange={(v) => { if (v) onchange(v === "__none__" ? "" : v); }}>
  <Select.Trigger data-testid={testId} size="sm" class="flex-1">
    {value || "None"}
  </Select.Trigger>
  <Select.Content>
    <Select.Item value="__none__">None</Select.Item>
    {#each PRIORITIES as p}
      <Select.Item value={p}>{p}</Select.Item>
    {/each}
  </Select.Content>
</Select.Root>
