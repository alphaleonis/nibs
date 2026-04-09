<script lang="ts">
  import { TYPES } from "../constants";
  import * as Select from "$lib/components/ui/select/index.js";

  interface Props {
    value: string;
    onchange: (type: string) => void;
    testId?: string;
    disabled?: boolean;
  }

  let { value, onchange, testId = "type-select", disabled = false }: Props = $props();
</script>

<Select.Root type="single" {value} {disabled} onValueChange={(v) => { if (v) onchange(v); }}>
  <Select.Trigger data-testid={testId} size="sm" class="flex-1">
    {value}
  </Select.Trigger>
  <Select.Content>
    {#each TYPES as t}
      <Select.Item value={t}>{t}</Select.Item>
    {/each}
  </Select.Content>
</Select.Root>
