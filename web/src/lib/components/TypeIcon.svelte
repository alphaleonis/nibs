<script lang="ts">
  import { typeIcons } from "../icons";

  interface Props {
    type: string;
    size?: number;
  }

  let { type, size = 16 }: Props = $props();

  let iconInfo = $derived(typeIcons[type]);
</script>

{#if iconInfo}
  {@const Icon = iconInfo.icon}
  <Icon {size} style="color: {iconInfo.color}; display: inline;" class="inline shrink-0" />
{/if}
