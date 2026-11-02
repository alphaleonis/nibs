<script lang="ts" module>
  /**
   * The "+ New area…" item's value. Its leading "/" keeps it distinct from every
   * declared path, as `NO_AREA`'s does.
   */
  const NEW_AREA = "/__new_area__";
</script>

<script lang="ts">
  import { Plus } from "@lucide/svelte";
  import * as Select from "$lib/components/ui/select/index.js";
  import { NO_AREA, areaColor, fromSelectValue, toSelectValue } from "../areas";
  import { useViewSpine } from "../contexts";

  interface Props {
    /** The nib's DIRECT area assignment: "" for one in no area. */
    value: string;
    onchange: (area: string) => void;
    /** Asks the host to create an area. Absent, the picker offers no "+ New area…" item. */
    oncreatearea?: () => void;
    testId?: string;
    disabled?: boolean;
  }

  let { value, onchange, oncreatearea, testId = "area-select", disabled = false }: Props = $props();

  const spine = useViewSpine();
  // Parents included: a non-leaf is a legal assignment.
  let nodes = $derived(spine().areas.sections());
  // The trigger shows the stored value verbatim (the full path, or raw text for
  // an undeclared area); rows show their segment under their parent.
  let label = $derived(value === "" ? "None" : value);
  // Bound, so a "+ New area…" pick can be reset: `value` does not change then, and
  // the Select would otherwise keep the sentinel as its selection.
  let selected = $derived(toSelectValue(value));

  function pick(v: string) {
    if (v === NEW_AREA) {
      selected = toSelectValue(value);
      oncreatearea?.();
      return;
    }
    if (v) onchange(fromSelectValue(v));
  }
</script>

<Select.Root type="single" bind:value={selected} {disabled} onValueChange={pick}>
  <Select.Trigger data-testid={testId} size="default" class="flex-1">
    {label}
  </Select.Trigger>
  <Select.Content>
    <Select.Item value={NO_AREA}>None</Select.Item>
    {#each nodes as node (node.path)}
      {@const swatch = areaColor(node.color)}
      <Select.Item value={node.path}>
        <span style:padding-left={`${node.depth * 0.75}rem`}>
          {#if swatch}
            <!-- `areaColor` is what makes config text safe in this style. -->
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
    {#if oncreatearea}
      <Select.Separator />
      <Select.Item value={NEW_AREA} label="New area…">
        <Plus class="text-muted-foreground" />
        New area…
      </Select.Item>
    {/if}
  </Select.Content>
</Select.Root>
