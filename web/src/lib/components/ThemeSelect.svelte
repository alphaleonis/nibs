<script lang="ts">
  import { THEMES } from "../types";
  import type { Theme } from "../types";
  import { parseTheme } from "../storage";
  import * as Select from "$lib/components/ui/select/index.js";

  interface Props {
    value: Theme;
    onchange: (theme: Theme) => void;
    testId?: string;
    disabled?: boolean;
  }

  let { value, onchange, testId = "theme-select", disabled = false }: Props = $props();

  let label = $derived(THEMES.find(t => t.value === value)?.label ?? value);
</script>

<Select.Root type="single" {value} {disabled} onValueChange={(v) => { if (v) onchange(parseTheme(v)); }}>
  <Select.Trigger data-testid={testId} size="sm" class="w-40">
    {label}
  </Select.Trigger>
  <Select.Content>
    {#each THEMES as t}
      <Select.Item value={t.value}>{t.label}</Select.Item>
    {/each}
  </Select.Content>
</Select.Root>
