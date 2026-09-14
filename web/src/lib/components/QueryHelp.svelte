<script lang="ts">
  import { CircleHelp } from "@lucide/svelte";
  import * as Popover from "$lib/components/ui/popover/index.js";
  import WithTooltip from "./WithTooltip.svelte";
  import { queryHelpSections } from "../query";

  // Reference for the filter query language. The token rows are built from the
  // parser's vocabulary (query/help.ts).
  const sections = queryHelpSections();

  const triggerLabel = "Query syntax help";
</script>

<Popover.Root>
  <!-- WithTooltip CHAIN mode: hover hints, click opens the panel. -->
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
  <!-- Height-capped with internal scroll: the reference is taller than a popover
       should grow. -->
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
