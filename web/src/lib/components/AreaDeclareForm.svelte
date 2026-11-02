<script lang="ts">
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Dialog from "$lib/components/ui/dialog/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import { toast } from "svelte-sonner";
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
    /**
     * Bring the Areas view into sight: the success toast's Show action. Absent,
     * the toast has no action, for a host where the new area is already shown.
     */
    onshow?: () => void;
    /** Take the full path of the area a create just made. Called before `onclose`. */
    oncreated?: (path: string) => void;
  }

  let { parentPath = "", onclose, oncancel, onshow, oncreated }: Props = $props();

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
        // The form toasts, so every host confirms a create the same way.
        const show = onshow;
        toast.success(`Created area ${full}`, show ? { action: { label: "Show", onClick: () => show() } } : undefined);
        path = "";
        oncreated?.(full);
        onclose?.();
      }
    } finally {
      submitting = false;
    }
  }
</script>

<form
  data-testid="area-declare-form"
  class="flex flex-col gap-3"
  onsubmit={(e) => {
    e.preventDefault();
    void submit();
  }}
>
  <div class="flex flex-col gap-1">
    <label class="text-caption text-muted-foreground" for="area-declare-path">
      {parentPath === "" ? "Path" : "Name"}
    </label>
    <Input
      id="area-declare-path"
      data-testid="area-path-input"
      bind:value={path}
      placeholder={parentPath === "" ? "web/dashboard" : "name"}
      aria-label={parentPath === "" ? "Area path" : `New area under ${parentPath}`}
      class="h-7"
    />
  </div>
  <AreaEditError message={error} />
  <!-- The dialog's footer band, so the actions sit where ConfirmDialog's do. -->
  <Dialog.Footer class="mt-1">
    <Button type="button" variant="outline" disabled={submitting} onclick={() => (oncancel ?? onclose)?.()}>
      Cancel
    </Button>
    <Button type="submit" disabled={submitting}>Create</Button>
  </Dialog.Footer>
</form>
