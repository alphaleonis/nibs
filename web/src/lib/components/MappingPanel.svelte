<script lang="ts">
  import { getContextClient, queryStore, subscriptionStore } from "@urql/svelte";
  import { untrack } from "svelte";
  import { Inbox, X } from "@lucide/svelte";
  import { MAPPING_PLAN_QUERY, NIB_CHANGED_SUBSCRIPTION } from "../queries";
  import { useConnection, useDrag, useMappingMode, useMilestones } from "../contexts";
  import type { MembershipLookup, MembershipNib } from "../membership";
  import type { MappingPlanQuery } from "../gql/graphql";
  import { mappingTargets, milestoneCounts, planMappingDrop } from "../ordering/mappingDrop";
  import { refuse } from "../ordering/dropPlan";
  import { Progress } from "$lib/components/ui/progress/index.js";
  import StatusIcon from "./StatusIcon.svelte";
  import TooltipButton from "./TooltipButton.svelte";

  let { onclose }: { /** Turns mapping mode off. */ onclose: () => void } = $props();

  const REFETCH_DEBOUNCE_MS = 250;

  const client = getContextClient();
  const drag = useDrag();
  const mapping = useMappingMode();
  const milestones = useMilestones();

  // Unfiltered, because a milestone's plan spans nibs the table's filter hides.
  const plan = queryStore({ client, query: MAPPING_PLAN_QUERY });
  // The last answer, kept across a failed refetch, whose result carries no data.
  let nibs = $state<MappingPlanQuery["nibs"] | null>(null);
  let queryFailed = $state(false);
  $effect(() =>
    plan.subscribe((v) => {
      if (v.data?.nibs) nibs = v.data.nibs;
      queryFailed = v.error !== undefined;
    }),
  );
  const failed = $derived(nibs === null && queryFailed);
  const stale = $derived(nibs !== null && queryFailed);
  const byId = $derived(new Map((nibs ?? []).map((n) => [n.id, n])));
  const childrenById = $derived.by(() => {
    const children = new Map<string, MembershipNib[]>();
    for (const n of nibs ?? []) {
      if (n.parentId === null) continue;
      const list = children.get(n.parentId);
      if (list) list.push(n);
      else children.set(n.parentId, [n]);
    }
    return children;
  });
  const counts = $derived(milestoneCounts(nibs ?? []));
  const targets = $derived(mappingTargets(milestones() ?? []));

  // urql re-runs the query after this client's own `Nib` mutations; another
  // client's edits arrive only through the change feed.
  const changes = subscriptionStore({ client, query: NIB_CHANGED_SUBSCRIPTION });
  let refetchTimer: ReturnType<typeof setTimeout> | null = null;
  function scheduleRefetch() {
    if (refetchTimer !== null) clearTimeout(refetchTimer);
    refetchTimer = setTimeout(() => {
      refetchTimer = null;
      plan.reexecute({ requestPolicy: "network-only" });
    }, REFETCH_DEBOUNCE_MS);
  }
  $effect(() => {
    let last: unknown = null;
    const unsub = changes.subscribe((v) => {
      const event = v.data?.nibChanged;
      if (event && event !== last) {
        last = event;
        untrack(scheduleRefetch);
      }
    });
    return () => {
      unsub();
      if (refetchTimer !== null) clearTimeout(refetchTimer);
    };
  });

  const connection = useConnection();
  $effect(() => connection?.onRecovered(() => plan.reexecute({ requestPolicy: "network-only" })));

  // Until the store has loaded, a card refuses: an empty lookup would read every
  // inherited membership as none.
  const loaded = $derived(nibs !== null);
  $effect(() => {
    if (loaded) {
      const lookup: MembershipLookup = (id) => byId.get(id);
      const nameOf = (id: string) => byId.get(id)?.title;
      const childrenOf = (id: string) => childrenById.get(id) ?? [];
      const statusOf = (id: string) => byId.get(id)?.status;
      mapping.planner = (target, draggedIds, draggedNib) =>
        planMappingDrop({ draggedIds, draggedNib, target, lookup, childrenOf, statusOf, stale, nameOf });
    } else {
      const message = failed
        ? "Milestone counts could not be loaded, so nothing can be planned here yet."
        : "Milestone counts are still loading.";
      mapping.planner = () => refuse("panel-unavailable", message);
    }
    return () => {
      mapping.planner = null;
    };
  });

  function highlight(value: string): string {
    if (!drag.isDragging || drag.panelTargetKey !== value) return "border-border";
    return drag.dropValid ? "border-region-assign bg-region-assign/12" : "border-destructive/60 border-dashed";
  }
</script>

<section class="flex h-full min-w-0 flex-col bg-background text-foreground" data-testid="mapping-panel" data-mapping-panel aria-label="Mapping">
  <header class="flex flex-none items-start gap-2 border-b border-border px-4 py-3">
    <div class="min-w-0 flex-1">
      <h2 class="text-sm font-semibold">Mapping</h2>
      <p class="text-xs text-muted-foreground">Drag rows onto a milestone to plan them.</p>
      {#if stale}
        <p class="text-xs text-destructive" data-testid="mapping-counts-stale">Could not refresh; counts may be out of date.</p>
      {/if}
    </div>
    <TooltipButton label="Exit mapping mode" size="icon-xs" class="text-muted-foreground" data-testid="mapping-panel-close" onclick={onclose}>
      <X size={14} />
    </TooltipButton>
  </header>

  <div class="mapping-targets min-h-0 flex-1 overflow-y-auto p-3">
    {#each targets as target (target.value)}
      {@const milestone = target.milestone}
      {@const count = milestone === null ? null : (counts.get(milestone.id) ?? { open: 0, closed: 0 })}
      <div
        class="flex min-h-24 flex-col justify-between gap-3 rounded-lg border p-3 transition-colors {highlight(target.value)}"
        data-mapping-target={target.value}
        data-testid="mapping-target"
        data-target-title={milestone?.title ?? "No milestone"}
      >
        <div class="flex min-w-0 items-center gap-2 text-sm font-medium">
          {#if milestone === null}
            <Inbox size={14} class="shrink-0 text-muted-foreground" />
            <span class="truncate">No milestone</span>
          {:else}
            <StatusIcon status={milestone.status} />
            <span class="truncate" title={milestone.title}>{milestone.title}</span>
          {/if}
        </div>
        {#if count === null}
          <p class="text-xs text-muted-foreground">Drop here to clear the milestone.</p>
        {:else if failed}
          <p class="text-xs text-destructive" data-testid="mapping-counts-error">Counts could not be loaded.</p>
        {:else if !loaded}
          <p class="text-xs text-muted-foreground" data-testid="mapping-counts-loading">Loading counts…</p>
        {:else}
          {@const total = count.open + count.closed}
          <div class="flex flex-col gap-1.5">
            <p class="text-xs text-muted-foreground">
              <span data-testid="mapping-open-count">{count.open}</span> open ·
              <span data-testid="mapping-closed-count">{count.closed}</span> closed
            </p>
            <Progress value={count.closed} max={Math.max(total, 1)} aria-label="{count.closed} of {total} closed" />
          </div>
        {/if}
      </div>
    {/each}
  </div>
</section>

<style>
  .mapping-targets {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(14rem, 1fr));
    align-content: start;
    gap: 0.75rem;
  }
</style>
