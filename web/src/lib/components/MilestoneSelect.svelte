<script lang="ts">
  import * as Select from "$lib/components/ui/select/index.js";
  import { NO_MILESTONE, fromSelectValue, milestoneChoices, toSelectValue } from "../milestones";
  import { useMilestones } from "../contexts";

  interface Props {
    /** The nib's direct milestone assignment; "" for none. */
    value: string;
    /** The nib's status, which decides which milestones are refused. */
    subjectStatus: string;
    onchange: (milestone: string) => void;
    testId?: string;
    disabled?: boolean;
  }

  let { value, subjectStatus, onchange, testId = "milestone-select", disabled = false }: Props = $props();

  const milestones = useMilestones();
  let choices = $derived(milestoneChoices(milestones(), { status: subjectStatus, milestone: value }));
  // An assignment can name a milestone the list does not hold (deleted, or not yet
  // loaded); show the raw id rather than blank.
  let current = $derived(choices.find((c) => c.id === value));
  let label = $derived(value === "" ? "None" : (current?.title ?? value));
</script>

<Select.Root
  type="single"
  value={toSelectValue(value)}
  {disabled}
  onValueChange={(v) => { if (v) onchange(fromSelectValue(v)); }}
>
  <Select.Trigger data-testid={testId} size="default" class="flex-1">
    {label}
  </Select.Trigger>
  <Select.Content>
    <Select.Item value={NO_MILESTONE}>None</Select.Item>
    {#each choices as c (c.id)}
      <Select.Item value={c.id} disabled={c.refusal !== null} title={c.refusal ?? undefined}>
        {c.title}
        {#if c.refusal !== null}
          <span class="text-caption text-muted-foreground">{c.status}</span>
        {/if}
      </Select.Item>
    {/each}
  </Select.Content>
</Select.Root>
