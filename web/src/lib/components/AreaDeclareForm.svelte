<script lang="ts">
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import { addArea, getMutationStore } from "$lib/mutations";
  import { runAreaEdit } from "../areaEdit";
  import AreaEditError from "./AreaEditError.svelte";

  interface Props {
    /**
     * The already-declared area to nest the new one under. Absent declares a
     * root area, where what the user types is the whole path.
     */
    parentPath?: string;
    /** Dismiss the form: a declaration landed. */
    onclose?: () => void;
    /**
     * Back out without declaring anything. Absent, Cancel falls back to
     * `onclose` — which is what a host with nowhere to go back TO wants.
     */
    oncancel?: () => void;
  }

  let { parentPath = "", onclose, oncancel }: Props = $props();

  const mutations = getMutationStore();

  let path = $state("");
  let submitting = $state(false);
  let error = $state("");

  async function submit() {
    // The server refuses an empty path; not sending one keeps the refusal off a
    // user who has typed nothing yet.
    const typed = path.trim();
    if (typed === "" || submitting) return;

    // The schema takes the FULL path and refuses an undeclared parent, so a name
    // typed under a section row is nested here rather than sent on its own —
    // which would declare a second ROOT area that merely looks nested.
    const full = parentPath === "" ? typed : `${parentPath}/${typed}`;

    submitting = true;
    try {
      const outcome = await runAreaEdit(mutations, addArea(full));
      error = outcome.message;
      if (outcome.ok) {
        path = "";
        onclose?.();
      }
    } finally {
      submitting = false;
    }
  }
</script>

<form
  data-testid="area-declare-form"
  class="flex flex-col gap-2"
  onsubmit={(e) => {
    e.preventDefault();
    void submit();
  }}
>
  <div class="flex items-center gap-2">
  <Input
    data-testid="area-path-input"
    bind:value={path}
    placeholder={parentPath === "" ? "web/dashboard" : "name"}
    aria-label={parentPath === "" ? "Area path" : `New area under ${parentPath}`}
    class="h-7 w-56"
  />
  <Button type="submit" variant="outline" size="sm" disabled={submitting}>Declare</Button>
  <Button
    type="button"
    variant="ghost"
    size="sm"
    disabled={submitting}
    onclick={() => (oncancel ?? onclose)?.()}
  >
    Cancel
  </Button>
  </div>
  <AreaEditError message={error} />
</form>
