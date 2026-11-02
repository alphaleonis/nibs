<script lang="ts">
  import { Input } from "$lib/components/ui/input/index.js";

  interface Props {
    /** A hex code or a bare color name; "" for none. */
    value: string;
    /** Full path of the area the color belongs to, for the accessible name. */
    path: string;
    id?: string;
  }

  let { value = $bindable(), path, id }: Props = $props();
</script>

<Input
  {id}
  data-testid="area-color-input"
  bind:value
  placeholder="blue or #4488ff"
  aria-label={`Color for ${path}`}
  class="h-7"
/>
