<script lang="ts">
  import { CircleHelp } from "@lucide/svelte";
  import * as Popover from "$lib/components/ui/popover/index.js";
  import WithTooltip from "./WithTooltip.svelte";
  import { queryHelpSections } from "../query";

  // In-UI reference for the filter query language, opened from a `?` button at the
  // end of the filter band. The grammar is powerful but was undiscoverable: the
  // placeholder hints one token and the autocomplete only helps once you are
  // already typing something it recognizes.
  //
  // The token rows are GENERATED from the vocabulary the parser reads (see
  // `query/help.ts`), so this panel cannot list a token the box would reject.
  //
  // Content is computed once — the vocabulary is module-level constant data, not
  // reactive state, so there is nothing here to re-derive per render.
  const sections = queryHelpSections();

  // Shared by the trigger's accessible name and its tooltip text — the two are
  // separate mechanisms (aria-label names the control, the tooltip is the visible
  // hint) and both should read the same.
  const triggerLabel = "Query syntax help";
</script>

<Popover.Root>
  <!-- CHAIN mode: Popover.Trigger runs its own mergeProps over the spread, so the
       tooltip's hover/focus handlers chain with the popover's open handler — hover
       hints, click still opens the panel. Every other control in this band hints
       through the same styled tooltip. -->
  <WithTooltip tooltip={triggerLabel}>
    {#snippet trigger({ props })}
      <Popover.Trigger
        {...props}
        aria-label={triggerLabel}
        data-testid="query-help-trigger"
        class="inline-flex size-8 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <CircleHelp size={16} />
      </Popover.Trigger>
    {/snippet}
  </WithTooltip>
  <!-- Constrained height with internal scroll: the reference is longer than a
       popover should ever grow, and the band it anchors to sits near the top of
       the viewport. `align="end"` keeps it inside the window when the trigger is
       at the right edge of the row. -->
  <Popover.Content
    align="end"
    data-testid="query-help-panel"
    class="max-h-[70vh] w-[min(30rem,calc(100vw-2rem))] overflow-y-auto p-0"
  >
    <div class="border-b border-border px-4 py-3">
      <h2 class="text-body font-medium text-foreground">Filter query syntax</h2>
      <p class="mt-0.5 text-caption text-muted-foreground">
        Combine conditions with spaces, or type plain words to search.
      </p>
    </div>
    {#each sections as s (s.title)}
      <section class="border-b border-border px-4 py-3 last:border-b-0">
        <h3 class="text-label font-medium text-foreground">{s.title}</h3>
        {#if s.note}
          <p class="mt-0.5 text-caption text-muted-foreground">{s.note}</p>
        {/if}
        <dl class="mt-2 space-y-1.5">
          {#each s.rows as row (row.token)}
            <div class="flex flex-col gap-0.5 sm:flex-row sm:gap-3">
              <dt class="shrink-0 sm:w-44">
                <code class="rounded bg-muted px-1 py-0.5 text-caption text-foreground">{row.token}</code>
              </dt>
              <dd class="min-w-0 text-caption text-muted-foreground">{row.description}</dd>
            </div>
          {/each}
        </dl>
      </section>
    {/each}
  </Popover.Content>
</Popover.Root>
