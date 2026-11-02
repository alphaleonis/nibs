<script lang="ts">
  import { untrack } from "svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Dialog from "$lib/components/ui/dialog/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import { getMutationStore, updateArea } from "$lib/mutations";
  import { runAreaEdit } from "../areaEdit";
  import AreaColorField from "./AreaColorField.svelte";
  import AreaEditError from "./AreaEditError.svelte";

  interface Props {
    /** Full path of the area being edited. */
    path: string;
    /** The node's own segment, which is what a rename sets. */
    name?: string;
    /** What the store declares today; "" when it declares none. */
    description?: string;
    color?: string;
    /** Dismiss the form: an edit landed, or there was nothing to send. */
    onclose?: () => void;
    /** Back out without saving. */
    oncancel?: () => void;
  }

  let { path, name = "", description = "", color = "", onclose, oncancel }: Props = $props();

  const mutations = getMutationStore();

  // The baseline for "what changed". Seeded once: a live prop would overwrite
  // what is being typed when the vocabulary changes under the open form.
  const seed = untrack(() => ({ name, description, color }));

  let draftName = $state(seed.name);
  let draftDescription = $state(seed.description);
  let draftColor = $state(seed.color);
  let submitting = $state(false);
  let error = $state("");

  async function submit() {
    if (submitting) return;

    const typedName = draftName.trim();
    if (typedName === "") {
      error = "An area needs a name.";
      return;
    }

    // Only what changed: an echoed field would overwrite a concurrent edit to it,
    // and the server refuses a rename to the current name. "" clears a field.
    const edit: { newName?: string; description?: string; color?: string } = {};
    if (typedName !== seed.name) edit.newName = typedName;
    if (draftDescription !== seed.description) edit.description = draftDescription;
    if (draftColor !== seed.color) edit.color = draftColor;

    // The server refuses an input that sets nothing.
    if (Object.keys(edit).length === 0) {
      onclose?.();
      return;
    }

    submitting = true;
    try {
      const outcome = await runAreaEdit(mutations, updateArea(path, edit));
      // A refusal stays open with the drafts intact, since the fix is to change them.
      error = outcome.message;
      if (outcome.ok) onclose?.();
    } finally {
      submitting = false;
    }
  }
</script>

<form
  data-testid="area-edit-form"
  class="flex flex-col gap-3"
  onsubmit={(e) => {
    e.preventDefault();
    void submit();
  }}
>
  <div class="flex flex-col gap-1">
    <label class="text-caption text-muted-foreground" for="area-edit-name">Name</label>
    <Input
      id="area-edit-name"
      data-testid="area-name-input"
      bind:value={draftName}
      aria-label={`Name for ${path}`}
      class="h-7"
    />
  </div>
  <div class="flex flex-col gap-1">
    <label class="text-caption text-muted-foreground" for="area-edit-description">Description</label>
    <Input
      id="area-edit-description"
      data-testid="area-description-input"
      bind:value={draftDescription}
      placeholder="What belongs in this area"
      aria-label={`Description for ${path}`}
      class="h-7"
    />
  </div>
  <div class="flex flex-col gap-1">
    <span class="text-caption text-muted-foreground">Color</span>
    <AreaColorField bind:value={draftColor} {path} />
  </div>
  <AreaEditError message={error} />
  <!-- The dialog's footer band, so the actions sit where ConfirmDialog's do. -->
  <Dialog.Footer class="mt-1">
    <Button type="button" variant="outline" disabled={submitting} onclick={() => (oncancel ?? onclose)?.()}>
      Cancel
    </Button>
    <Button type="submit" disabled={submitting}>Save</Button>
  </Dialog.Footer>
</form>
