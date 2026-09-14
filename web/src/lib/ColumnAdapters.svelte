<!--
  Column adapters: the Svelte markup for each column's header and cell, keyed by
  ColumnKey. The column model lives in columns.ts.

  The snippets close over nothing from the instance <script>, so Svelte hoists
  them to module scope and `columnAdapters` can be exported; the provider below
  (mounted in App.svelte) and makeTestContext hand the table the same map.

  `satisfies ColumnAdapters` here and `satisfies Record<ColumnKey, ColumnDef>` in
  columns.ts pin both to the ColumnKey union.
-->
<script module lang="ts">
  import { getContext, setContext } from "svelte";
  import type { Snippet } from "svelte";
  import type { RowContext, ColumnKey } from "./columns";
  import { ALL_COLUMN_KEYS } from "./columns";
  import { priorityIndicators, statusDotColors } from "./badges";
  import { ChevronRight, ChevronDown } from "@lucide/svelte";
  import StatusIcon from "./components/StatusIcon.svelte";
  import RelationBadge from "./components/RelationBadge.svelte";
  import TypeIcon from "./components/TypeIcon.svelte";
  import { RELATION_CONFIG } from "./relations";
  import { formatRelative, formatAbsolute } from "./date";
  import { isSyntheticRowId } from "./tree";
  import { cssColor } from "./areas";
  import { blockedVariantFor } from "./types";

  /** The two snippets that render one column: its header content and its cell. */
  export interface ColumnRenderer {
    header: Snippet;
    cell: Snippet<[RowContext]>;
  }
  export type ColumnAdapters = Record<ColumnKey, ColumnRenderer>;

  const COLUMN_ADAPTERS_KEY = "nibs:column-adapters";

  export function provideColumnAdapters(a: ColumnAdapters) {
    setContext(COLUMN_ADAPTERS_KEY, a);
  }
  export function useColumnAdapters(): ColumnAdapters {
    const a = getContext<ColumnAdapters>(COLUMN_ADAPTERS_KEY);
    if (!a) throw new Error("useColumnAdapters() called outside provider — mount <ColumnAdapters> above the table (App.svelte) or use makeTestContext()");
    return a;
  }
  // makeTestContext seeds this key in a raw context Map.
  export { COLUMN_ADAPTERS_KEY };

  export function assertColumnParity(adapters: ColumnAdapters): void {
    for (const key of ALL_COLUMN_KEYS) {
      const r = adapters[key];
      if (!r || typeof r.header !== "function" || typeof r.cell !== "function") {
        throw new Error(`ColumnAdapters is missing a header/cell renderer for column "${key}"`);
      }
    }
  }

  // A function rather than a bare literal, so TypeScript does not flag the
  // below-declared snippets as used before declaration.
  function buildColumnAdapters(): ColumnAdapters {
    return {
      id: { header: headerId, cell: cellId },
      parent: { header: headerParent, cell: cellParent },
      type: { header: headerType, cell: cellType },
      title: { header: headerTitle, cell: cellTitle },
      status: { header: headerStatus, cell: cellStatus },
      estimate: { header: headerEstimate, cell: cellEstimate },
      tags: { header: headerTags, cell: cellTags },
      milestone: { header: headerMilestone, cell: cellMilestone },
      area: { header: headerArea, cell: cellArea },
      blocking: { header: headerBlocking, cell: cellBlocking },
      blockedBy: { header: headerBlockedBy, cell: cellBlockedBy },
      created: { header: headerCreated, cell: cellCreated },
      modified: { header: headerModified, cell: cellModified },
    } satisfies ColumnAdapters;
  }

  export const columnAdapters: ColumnAdapters = buildColumnAdapters();
</script>

<script lang="ts">
  let { children }: { children?: Snippet } = $props();

  provideColumnAdapters(columnAdapters);
  if (import.meta.env.DEV) assertColumnParity(columnAdapters);
</script>

{@render children?.()}

<!-- ===================== Header snippets ===================== -->
<!-- TableHeader renders a sortable column's label itself, so these are used only
     for a column marked non-sortable. -->
{#snippet headerId()}ID{/snippet}
{#snippet headerParent()}Parent{/snippet}
{#snippet headerType()}Type{/snippet}
{#snippet headerTitle()}Title{/snippet}
{#snippet headerStatus()}Status{/snippet}
{#snippet headerEstimate()}Estimate{/snippet}
{#snippet headerTags()}Tags{/snippet}
{#snippet headerMilestone()}Milestone{/snippet}
{#snippet headerArea()}Area{/snippet}
{#snippet headerBlocking()}Blocking{/snippet}
{#snippet headerBlockedBy()}Blocked by{/snippet}
{#snippet headerCreated()}Created{/snippet}
{#snippet headerModified()}Modified{/snippet}

<!-- ===================== Cell snippets ===================== -->
<!-- Each cell is a pure function of RowContext. -->

{#snippet cellId(ctx: RowContext)}
  {@const nib = ctx.nib}
  {@const shortId = nib.id.substring(nib.id.lastIndexOf("-") + 1)}
  <td data-testid="nib-id" class="text-body px-3 cell-truncate row-cell" style="color: var(--muted-foreground);">{isSyntheticRowId(nib.id) ? "" : shortId}</td>
{/snippet}

{#snippet cellParent(ctx: RowContext)}
  {@const parentNib = ctx.parentNib}
  <td data-testid="nib-parent" class="text-body px-3 cell-truncate row-cell" style="color: var(--text-secondary);" title={parentNib ? parentNib.id : undefined}>
    {#if parentNib}
      <TypeIcon type={parentNib.type} size={14} />
      {parentNib.title}
    {/if}
  </td>
{/snippet}

<!-- A milestone the table does not hold shows the raw id, as MilestoneSelect
     does: blank would read as unassigned. -->
{#snippet cellMilestone(ctx: RowContext)}
  {@const milestoneNib = ctx.milestoneNib}
  {@const assigned = ctx.nib.milestone}
  <td data-testid="nib-milestone" class="text-body px-3 cell-truncate row-cell" style="color: var(--text-secondary);" title={milestoneNib ? milestoneNib.id : (assigned || undefined)}>
    {#if milestoneNib}
      <TypeIcon type={milestoneNib.type} size={14} />
      {milestoneNib.title}
    {:else if assigned}
      {assigned}
    {/if}
  </td>
{/snippet}

<!-- The full stored path, not the leaf: a leaf is ambiguous across parents. -->
{#snippet cellArea(ctx: RowContext)}
  {@const area = ctx.nib.area}
  <td data-testid="nib-area" class="text-body px-3 cell-truncate row-cell" style="color: var(--text-secondary);" title={area || undefined}>{area}</td>
{/snippet}

{#snippet cellType(ctx: RowContext)}
  <td data-testid="nib-type" class="text-body px-3 cell-truncate row-cell" style="color: var(--text-secondary);">{ctx.nib.type}</td>
{/snippet}

{#snippet cellTitle(ctx: RowContext)}
  {@const { nib, depth, hasChildren, collapsed, blockedEmphasis } = ctx}
  <!-- Section facts are drawn only on a synthetic row; a section headed by a real
       nib renders that nib's own columns. -->
  {@const section = ctx.drawsSection !== null && isSyntheticRowId(nib.id) ? ctx.drawsSection : null}
  {@const sectionColor = section === null ? null : cssColor(section.display.color)}
  {@const priorityIndicator = priorityIndicators[nib.priority] ?? null}
  {@const isBlocked = nib.blockedByIds.length > 0}
  {@const blockedVariant = blockedVariantFor(blockedEmphasis)}
  <td data-testid="nib-title" class="cell-truncate row-cell" style="padding-left: {depth * 24}px;">
    <div class="title-content">
      {#if hasChildren}
        <!-- Raw button: handled by TreeTable's delegated data-action click. -->
        <button
          data-testid="toggle"
          data-action="toggle"
          class="shrink-0 w-5 h-5 inline-flex items-center justify-center rounded-sm text-muted-foreground hover:text-foreground"
        >
          {#if collapsed}<ChevronRight size={14} />{:else}<ChevronDown size={14} />{/if}
        </button>
      {:else}
        <span class="inline-block w-5 h-5 shrink-0"></span>
      {/if}
      <!-- TypeIcon takes no class, so the gap lives on a wrapper. -->
      <span class="type-icon-gap"><TypeIcon type={nib.type} /></span>
      {#if sectionColor}
        <!-- `cssColor` is what makes config text safe in this style. -->
        <span
          data-testid="section-color"
          class="shrink-0 size-2.5 rounded-full border border-border"
          style:background-color={sectionColor}
        ></span>
      {/if}
      {#if priorityIndicator}
        <span
          data-testid="priority-icon"
          class="shrink-0 text-sm font-bold"
          style="color: {priorityIndicator.color};"
        >{priorityIndicator.symbol}</span>
      {/if}
      <!-- Raw button: truncating inline text, handled by TreeTable's delegated
           data-action click. -->
      <button
        data-testid="title-text"
        data-action="title"
        class="title-text-btn"
      >{nib.title}</button>
      {#if section}
        <span data-testid="section-count" class="shrink-0 text-muted-foreground">({section.count})</span>
        {#if section.display.description}
          <span data-testid="section-description" class="section-description">{section.display.description}</span>
        {/if}
      {/if}
      {#if isBlocked}
        <RelationBadge kind="blocked" count={nib.blockedByIds.length} variant={blockedVariant} />
      {/if}
      <!-- Row dimming stays blocked-only (blockedDim in TreeTableRow). -->
      {#if nib.blockingIds.length > 0}
        <RelationBadge kind="blocking" count={nib.blockingIds.length} variant={blockedVariant} />
      {/if}
    </div>
  </td>
{/snippet}

{#snippet cellStatus(ctx: RowContext)}
  {@const nib = ctx.nib}
  {@const statusDotColor = statusDotColors[nib.status] ?? "var(--muted-foreground)"}
  <td data-testid="nib-status" class="text-body px-3 cell-truncate row-cell">
    <StatusIcon status={nib.status} class="mr-1.5" />
    <span style="color: {statusDotColor};">{nib.status}</span>
  </td>
{/snippet}

{#snippet cellEstimate(ctx: RowContext)}
  {@const nib = ctx.nib}
  <td data-testid="nib-estimate" class="text-body px-3 cell-truncate row-cell">
    {#if nib.estimate?.trim()}
      {nib.estimate.toUpperCase()}
    {/if}
  </td>
{/snippet}

{#snippet cellTags(ctx: RowContext)}
  <td data-testid="nib-tags" class="px-3 cell-truncate row-cell">
    {#each ctx.nib.tags as tag}
      <span
        data-testid="tag"
        class="inline-flex rounded-sm px-1.5 py-0.5 text-caption"
        style="background-color: var(--popover); color: var(--muted-foreground);"
      >{tag}</span>
    {/each}
  </td>
{/snippet}

{#snippet cellBlocking(ctx: RowContext)}
  {@const nib = ctx.nib}
  <td data-testid="nib-blocking" class="text-body px-3 cell-truncate row-cell">
    {#if nib.blockingIds.length > 0}
      {@const BlockingIcon = RELATION_CONFIG.blocking.icon}
      <span
        class="inline-flex items-center gap-1"
        style="color: {RELATION_CONFIG.blocking.iconColor};"
        title={nib.blockingIds.join(", ")}
      ><BlockingIcon size={12} />{nib.blockingIds.length}</span>
    {/if}
  </td>
{/snippet}

{#snippet cellBlockedBy(ctx: RowContext)}
  {@const nib = ctx.nib}
  <td data-testid="nib-blocked-by" class="text-body px-3 cell-truncate row-cell">
    {#if nib.blockedByIds.length > 0}
      {@const BlockedIcon = RELATION_CONFIG.blocked.icon}
      <span
        class="inline-flex items-center gap-1"
        style="color: {RELATION_CONFIG.blocked.iconColor};"
        title={nib.blockedByIds.join(", ")}
      ><BlockedIcon size={12} />{nib.blockedByIds.length}</span>
    {/if}
  </td>
{/snippet}

<!-- Relative age, ISO timestamp on hover. A synthetic row's empty timestamp
     formats blank. -->
{#snippet cellCreated(ctx: RowContext)}
  {@const nib = ctx.nib}
  <td data-testid="nib-created" class="text-body px-3 cell-truncate row-cell" style="color: var(--muted-foreground);" title={formatAbsolute(nib.createdAt)}>{formatRelative(nib.createdAt)}</td>
{/snippet}

{#snippet cellModified(ctx: RowContext)}
  {@const nib = ctx.nib}
  <td data-testid="nib-modified" class="text-body px-3 cell-truncate row-cell" style="color: var(--muted-foreground);" title={formatAbsolute(nib.updatedAt)}>{formatRelative(nib.updatedAt)}</td>
{/snippet}

<style>
  /* Scoped styles for the snippets' elements. TreeTableRow defines `.row-cell`
     too, for its actions cell. */
  .row-cell {
    padding-block: var(--row-pad-y, 0.25rem);
  }

  .cell-truncate {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .title-content {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    overflow: hidden;
    white-space: nowrap;
  }

  /* em-based so it scales with the row font size, like the .prose-nib checkbox
     margin (app.css). */
  .type-icon-gap {
    display: inline-flex;
    flex-shrink: 0;
    margin-inline-end: 0.35em;
  }

  /* Shrinks far ahead of the title, which is what the column is for. */
  .section-description {
    color: var(--muted-foreground);
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
    flex-shrink: 1000;
  }

  .title-text-btn {
    color: var(--foreground);
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
    cursor: pointer;
    background: none;
    border: none;
    padding: 0;
    font: inherit;
    /* The body type scale, not the 16px root. The title stands out by color,
       not weight. */
    font-size: var(--text-body-size);
    font-weight: 400;
    line-height: var(--text-body-leading);
    text-align: left;
  }
</style>
