<script lang="ts">
  import { untrack } from "svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import { getMutationStore, updateArea } from "$lib/mutations";
  import { runAreaEdit } from "../areaEdit";
  import AreaEditError from "./AreaEditError.svelte";

  interface Props {
    /** Full path of the area being renamed. */
    path: string;
    /** The node's current NAME — its own segment, which is what a rename sets. */
    name?: string;
    onclose?: () => void;
  }

  let { path, name = "", onclose }: Props = $props();

  const mutations = getMutationStore();

  // Seeded once, deliberately: this is a text field someone is typing into, and
  // a live prop would overwrite their edit if the vocabulary changed underneath
  // the open form. `untrack` states that rather than leaving it a warning.
  let newName = $state(untrack(() => name));
  let submitting = $state(false);
  let error = $state("");

  async function submit() {
    const typed = newName.trim();
    // The server refuses a rename to the name the node already has; not sending
    // one keeps that refusal off a user who opened the form and changed nothing.
    if (typed === "" || typed === name || submitting) return;

    submitting = true;
    try {
      // newName ALONE: description and color are left off so the edit cannot
      // clear what the store declares for them.
      const outcome = await runAreaEdit(mutations, updateArea(path, { newName: typed }));
      // A rejected argument is repaired by changing it, so the form stays open
      // with what was typed still in it.
      error = outcome.message;
      if (outcome.ok) onclose?.();
    } finally {
      submitting = false;
    }
  }
</script>

<form
  data-testid="area-rename-form"
  class="flex flex-col gap-2"
  onsubmit={(e) => {
    e.preventDefault();
    void submit();
  }}
>
  <div class="flex items-center gap-2">
    <Input
      data-testid="area-name-input"
      bind:value={newName}
      aria-label={`New name for ${path}`}
      class="h-7 w-56"
    />
    <Button type="submit" variant="outline" size="sm" disabled={submitting}>Rename</Button>
  </div>
  <AreaEditError message={error} />
</form>
