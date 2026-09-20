<script lang="ts">
  import { untrack } from "svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import { getMutationStore, updateArea } from "$lib/mutations";
  import { runAreaEdit } from "../areaEdit";
  import AreaEditError from "./AreaEditError.svelte";

  interface Props {
    /** Full path of the area being edited. */
    path: string;
    /** What the store declares today; "" when it declares none. */
    description?: string;
    color?: string;
    onclose?: () => void;
  }

  let { path, description = "", color = "", onclose }: Props = $props();

  const mutations = getMutationStore();

  // The values as they stood when the form opened, kept to decide what actually
  // changed. Seeded once: a live prop would both overwrite what is being typed
  // and move the baseline these drafts are compared against.
  const seed = untrack(() => ({ description, color }));

  let draftDescription = $state(seed.description);
  let draftColor = $state(seed.color);
  let submitting = $state(false);
  let error = $state("");

  async function submit() {
    if (submitting) return;

    // Only what CHANGED. An untouched field is left out rather than echoed
    // back, because on the wire an echo is indistinguishable from an edit and
    // would overwrite a change made elsewhere to a field nobody here touched.
    // An emptied field is a real edit, so "" is sent and clears the key.
    const edit: { description?: string; color?: string } = {};
    if (draftDescription !== seed.description) edit.description = draftDescription;
    if (draftColor !== seed.color) edit.color = draftColor;

    // The server refuses an input that sets none of its fields, so a save with
    // nothing edited must not become a refusal for the user to read.
    if (Object.keys(edit).length === 0) {
      onclose?.();
      return;
    }

    submitting = true;
    try {
      const outcome = await runAreaEdit(mutations, updateArea(path, edit));
      error = outcome.message;
      if (outcome.ok) onclose?.();
    } finally {
      submitting = false;
    }
  }
</script>

<form
  data-testid="area-details-form"
  class="flex flex-col gap-2"
  onsubmit={(e) => {
    e.preventDefault();
    void submit();
  }}
>
  <Input
    data-testid="area-description-input"
    bind:value={draftDescription}
    placeholder="What belongs in this area"
    aria-label={`Description for ${path}`}
    class="h-7 w-64"
  />
  <Input
    data-testid="area-color-input"
    bind:value={draftColor}
    placeholder="blue or #4488ff"
    aria-label={`Color for ${path}`}
    class="h-7 w-64"
  />
  <Button type="submit" variant="outline" size="sm" disabled={submitting}>Save</Button>
  <AreaEditError message={error} />
</form>
