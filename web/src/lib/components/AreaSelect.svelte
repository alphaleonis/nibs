<script lang="ts">
  import * as Select from "$lib/components/ui/select/index.js";
  import { NO_AREA, cssColor, fromSelectValue, toSelectValue } from "../areas";
  import { useViewSpine } from "../contexts";

  interface Props {
    /** The nib's DIRECT area assignment: "" for one in no area. */
    value: string;
    onchange: (area: string) => void;
    testId?: string;
    disabled?: boolean;
  }

  let { value, onchange, testId = "area-select", disabled = false }: Props = $props();

  const spine = useViewSpine();
  // Every declared node FLAT, parents included — a non-leaf is a legal
  // assignment, so the list offers one rather than only the leaves.
  let nodes = $derived(spine().areas.sections());
  // The trigger carries the STORED value verbatim: the full path for a declared
  // nested area (so the collapsed state is unambiguous and agrees with the Area
  // column), and the raw text for one the vocabulary does not declare — retired
  // from `areas:` since, or read before the config query resolved. Rows label by
  // segment instead, because a row is drawn under the parent that supplies the
  // rest of the path.
  let label = $derived(value === "" ? "None" : value);
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
    <Select.Item value={NO_AREA}>None</Select.Item>
    {#each nodes as node (node.path)}
      {@const swatch = cssColor(node.color)}
      <Select.Item value={node.path}>
        <span style:padding-left={`${node.depth * 0.75}rem`}>
          {#if swatch}
            <!-- `cssColor` is the whole of what makes config text safe in a
                 style here — the sink is a declaration list either way. -->
            <span
              data-testid="area-color"
              class="shrink-0 size-2.5 rounded-full border border-border"
              style:background-color={swatch}
            ></span>
          {/if}
          {node.name}
        </span>
      </Select.Item>
    {/each}
  </Select.Content>
</Select.Root>
