<script lang="ts">
  import { untrack } from "svelte";
  import { getContextClient, queryStore } from "@urql/svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as RadioGroup from "$lib/components/ui/radio-group/index.js";
  import { getMutationStore, removeArea } from "$lib/mutations";
  import type { AreaDisposition } from "$lib/mutations";
  import { runAreaEdit } from "../areaEdit";
  import { graphqlErrorMessage } from "../graphqlError";
  import AreaEditError from "./AreaEditError.svelte";
  import { AREA_MEMBERS_QUERY } from "../queries";
  import { useViewSpine } from "../contexts";

  interface Props {
    /** Full path of the area to retire. Its whole subtree goes with it. */
    path: string;
    onclose?: () => void;
  }

  let { path, onclose }: Props = $props();

  const mutations = getMutationStore();
  const client = getContextClient();
  const spine = useViewSpine();

  // Asked of the SERVER rather than counted from the table's rows: the `area:`
  // filter is downward-closed, so this covers the whole subtree, and the table's
  // own rows are narrowed by whatever filter is active.
  // Read once: one instance of this form serves one area, because TreeTable keys
  // the panel by path and so a different section row remounts it rather than
  // re-pointing this query at a new area underneath an open form.
  // network-only: urql's document cache never invalidates a result that came back
  // EMPTY, because an empty list carries no Nib typename for a later nib mutation
  // to match. A cached zero would arm the no-disposition retirement on an area
  // that has gained members since.
  const members = queryStore({
    client,
    query: AREA_MEMBERS_QUERY,
    variables: { area: untrack(() => path) },
    requestPolicy: "network-only",
  });

  let fetching = $derived($members.fetching);
  // A count is KNOWN only when the query settled with a list. `?? 0` alone turns
  // a failure — or a result that carried no data — into a positive "nothing is
  // assigned", which is the one claim this form must not make without evidence.
  // A partial result counts as unknown too: its list may be short, and short
  // understates exactly the work the retirement would strand.
  let countKnown = $derived(!$members.error && Array.isArray($members.data?.nibs));
  let membersMessage = $derived(graphqlErrorMessage($members.error));
  let memberCount = $derived($members.data?.nibs?.length ?? 0);

  // Everything at or below the node is about to stop being declared, so none of
  // it can receive the members — the server refuses such a moveTo outright.
  let excluded = $derived(new Set(spine().areas.subtreeOf(path).map((n) => n.path)));
  let targets = $derived(spine().areas.sections().filter((n) => !excluded.has(n.path)));

  const UNASSIGN = "unassign";
  const MOVE_PREFIX = "move:";

  let choice: string = $state("");
  let submitting = $state(false);
  let error = $state("");

  function chosenDisposition(): AreaDisposition | undefined {
    if (choice === UNASSIGN) return { unassign: true };
    if (choice.startsWith(MOVE_PREFIX)) return { moveTo: choice.slice(MOVE_PREFIX.length) };
    return undefined;
  }

  async function submit() {
    if (submitting) return;
    // The count is a precondition for the question, not a detail of it: without
    // one there is no basis for either a disposition or its absence.
    if (!countKnown) return;

    const disposition = chosenDisposition();
    // With members assigned, the server refuses the retirement outright unless a
    // disposition says what becomes of them — so an unanswered choice is held
    // here rather than sent and bounced.
    if (memberCount > 0 && disposition === undefined) return;

    submitting = true;
    try {
      const outcome = await runAreaEdit(mutations, removeArea(path, disposition));
      error = outcome.message;
      if (outcome.ok) onclose?.();
    } finally {
      submitting = false;
    }
  }
</script>

<div data-testid="area-remove-form" class="flex flex-col gap-2">
  {#if fetching}
    <span class="px-2 text-caption text-muted-foreground">Checking what is assigned…</span>
  {:else if !countKnown}
    <!-- Ahead of the count: the no-members copy asserts that nothing else is
         affected, which is a claim there is no evidence for here. -->
    <span data-testid="area-remove-unknown" class="px-2 text-caption text-foreground">
      What is assigned to {path} could not be read, so this retirement is not offered.
    </span>
    <Button variant="outline" size="sm" disabled>Retire</Button>
    <AreaEditError message={membersMessage} />
  {:else if memberCount === 0}
    <span class="px-2 text-caption text-muted-foreground">
      Retire {path} and every area declared beneath it.
    </span>
    <Button variant="outline" size="sm" disabled={submitting} onclick={() => void submit()}>
      Retire
    </Button>
  {:else}
    <span data-testid="area-remove-count" class="px-2 text-caption text-foreground">
      {memberCount}
      {memberCount === 1 ? "nib is" : "nibs are"} assigned to {path} or an area beneath it.
      Choose what becomes of them.
    </span>
    <RadioGroup.Root bind:value={choice} class="gap-1 px-2">
      <div class="flex items-center gap-2">
        <RadioGroup.Item value={UNASSIGN} id="area-disposition-unassign" aria-label="Clear their area" />
        <label class="text-caption" for="area-disposition-unassign">Clear their area</label>
      </div>
      {#each targets as target (target.path)}
        <div class="flex items-center gap-2">
          <RadioGroup.Item
            value={`${MOVE_PREFIX}${target.path}`}
            id={`area-disposition-move-${target.path}`}
            aria-label={`Move them to ${target.path}`}
          />
          <label class="text-caption" for={`area-disposition-move-${target.path}`}>
            Move them to {target.path}
          </label>
        </div>
      {/each}
    </RadioGroup.Root>
    <Button
      variant="outline"
      size="sm"
      disabled={submitting || choice === ""}
      onclick={() => void submit()}
    >
      Retire
    </Button>
  {/if}
  <AreaEditError message={error} />
</div>
