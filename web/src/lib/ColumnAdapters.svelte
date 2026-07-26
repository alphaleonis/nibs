<!--
  Column ADAPTERS — the Svelte "adapter" half of the table's ports-&-adapters
  column registry (the pure "port" model lives in columns.ts). This is the ONLY
  file that pairs a column key with the bespoke Svelte markup that renders its
  header and cell.

  Each column's header + cell is a {#snippet} below; the snippets are assembled
  into `columnAdapters` (a plain Record<ColumnKey, ColumnRenderer>) in the module
  script and exported. Because the snippets close over nothing from the instance
  <script> (every input arrives via the RowContext argument or a module-level
  import), Svelte hoists them to module scope and they can be exported as a value
  — which lets both the real provider AND makeTestContext hand the same map to
  the table without duplicating markup.

  The map is delivered to TreeTable / TreeTableRow through a Svelte context
  (provideColumnAdapters / useColumnAdapters), mirroring provideSelection /
  useSelection. Mounting <ColumnAdapters> around the table region (App.svelte)
  provides it; tests get it via makeTestContext.

  The sync guard: `columnAdapters satisfies ColumnAdapters` and, in columns.ts,
  `COLUMNS satisfies Record<ColumnKey, ColumnDef>` both pin to the ColumnKey
  union — a column with a def but no renderer (or vice versa) is a compile error
  naming the file to edit. assertColumnParity is the DEV-only runtime backstop.
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
  import { isBucketId } from "./tree";
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
  // Export the key so makeTestContext (which builds a raw context Map rather than
  // calling setContext) can seed the same slot useColumnAdapters reads.
  export { COLUMN_ADAPTERS_KEY };

  // DEV-only runtime backstop for the compile-time `satisfies` pin: every
  // ColumnKey must resolve to a renderer carrying both a header and a cell
  // snippet. Belt-and-suspenders for any future dynamically-built adapter map.
  export function assertColumnParity(adapters: ColumnAdapters): void {
    for (const key of ALL_COLUMN_KEYS) {
      const r = adapters[key];
      if (!r || typeof r.header !== "function" || typeof r.cell !== "function") {
        throw new Error(`ColumnAdapters is missing a header/cell renderer for column "${key}"`);
      }
    }
  }

  // The assembled adapter map. `satisfies ColumnAdapters` pins the key-set to the
  // ColumnKey union, so a missing or extra column fails to compile here.
  //
  // Assembled inside a function (not a bare object literal) so TypeScript does
  // not read the references to the below-declared {#snippet} bindings as a
  // temporal-dead-zone violation — Svelte hoists them to module scope, so they
  // are defined by the time this runs at module init.
  function buildColumnAdapters(): ColumnAdapters {
    return {
      id: { header: headerId, cell: cellId },
      parent: { header: headerParent, cell: cellParent },
      type: { header: headerType, cell: cellType },
      title: { header: headerTitle, cell: cellTitle },
      status: { header: headerStatus, cell: cellStatus },
      effort: { header: headerEffort, cell: cellEffort },
      tags: { header: headerTags, cell: cellTags },
      blocking: { header: headerBlocking, cell: cellBlocking },
      blockedBy: { header: headerBlockedBy, cell: cellBlockedBy },
      created: { header: headerCreated, cell: cellCreated },
      modified: { header: headerModified, cell: cellModified },
    } satisfies ColumnAdapters;
  }

  export const columnAdapters: ColumnAdapters = buildColumnAdapters();
</script>

<script lang="ts">
  // The provider component. Wrap the table region with it (App.svelte) so
  // TreeTable / TreeTableRow can read the adapter map via useColumnAdapters().
  let { children }: { children?: Snippet } = $props();

  provideColumnAdapters(columnAdapters);
  if (import.meta.env.DEV) assertColumnParity(columnAdapters);
</script>

{@render children?.()}

<!-- ===================== Header snippets ===================== -->
<!-- Header content is just the column label. The <th> shell (width, resize
     handle, and the click-to-sort UI) stays in TreeTable. Every column is
     sortable, so the shell renders its own sort-aware header (built from the
     column label) — a click-to-sort button in the flat view, a plain label
     elsewhere. These plain-label snippets back every column for parity and are
     the shell's fallback for any column later marked non-sortable. -->
{#snippet headerId()}ID{/snippet}
{#snippet headerParent()}Parent{/snippet}
{#snippet headerType()}Type{/snippet}
{#snippet headerTitle()}Title{/snippet}
{#snippet headerStatus()}Status{/snippet}
{#snippet headerEffort()}Effort{/snippet}
{#snippet headerTags()}Tags{/snippet}
{#snippet headerBlocking()}Blocking{/snippet}
{#snippet headerBlockedBy()}Blocked by{/snippet}
{#snippet headerCreated()}Created{/snippet}
{#snippet headerModified()}Modified{/snippet}

<!-- ===================== Cell snippets ===================== -->
<!-- Each cell is a pure function of RowContext, moved verbatim from
     TreeTableRow so the rendered <td> (testid / classes / inline style) is
     identical. -->

<!-- ID column -->
{#snippet cellId(ctx: RowContext)}
  {@const nib = ctx.nib}
  {@const shortId = nib.id.substring(nib.id.lastIndexOf("-") + 1)}
  <td data-testid="nib-id" class="text-body px-3 cell-truncate row-cell" style="color: var(--muted-foreground);">{isBucketId(nib.id) ? "" : shortId}</td>
{/snippet}

<!-- Parent column -->
{#snippet cellParent(ctx: RowContext)}
  {@const parentNib = ctx.parentNib}
  <td data-testid="nib-parent" class="text-body px-3 cell-truncate row-cell" style="color: var(--text-secondary);" title={parentNib ? parentNib.id : undefined}>
    {#if parentNib}
      <TypeIcon type={parentNib.type} size={14} />
      {parentNib.title}
    {/if}
  </td>
{/snippet}

<!-- Type column -->
{#snippet cellType(ctx: RowContext)}
  <td data-testid="nib-type" class="text-body px-3 cell-truncate row-cell" style="color: var(--text-secondary);">{ctx.nib.type}</td>
{/snippet}

<!-- Title column -->
{#snippet cellTitle(ctx: RowContext)}
  {@const { nib, depth, hasChildren, collapsed, blockedEmphasis } = ctx}
  {@const priorityIndicator = priorityIndicators[nib.priority] ?? null}
  {@const isBlocked = nib.blockedByIds.length > 0}
  {@const blockedVariant = blockedVariantFor(blockedEmphasis)}
  <td data-testid="nib-title" class="cell-truncate row-cell" style="padding-left: {depth * 24}px;">
    <div class="title-content">
      {#if hasChildren}
        <!-- Raw button: delegated expand/collapse control (data-action);
             kept raw to preserve TreeTable's event delegation. -->
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
      <!-- Wrapper adds breathing room after the type icon; TypeIcon forwards
           no class/style, so the em-based margin lives here (scales with the
           row font-size). Title column only — the Parent column is untouched. -->
      <span class="type-icon-gap"><TypeIcon type={nib.type} /></span>
      {#if priorityIndicator}
        <span
          data-testid="priority-icon"
          class="shrink-0 text-sm font-bold"
          style="color: {priorityIndicator.color};"
        >{priorityIndicator.symbol}</span>
      {/if}
      <!-- Raw button: delegated title control (data-action) rendered as inline
           ellipsis-truncating text; the Button primitive's flex/height layout
           doesn't fit, and delegation must be preserved. -->
      <button
        data-testid="title-text"
        data-action="title"
        class="title-text-btn"
      >{nib.title}</button>
      {#if isBlocked}
        <RelationBadge kind="blocked" count={nib.blockedByIds.length} variant={blockedVariant} />
      {/if}
      <!-- 'blocking' mirrors 'blocked': same emphasis-driven variant (subtle→icon,
           pill/pill-dim→pill). Row DIMMING stays blocked-only — a nib is not
           dimmed for blocking others (see blockedDim in TreeTableRow). -->
      {#if nib.blockingIds.length > 0}
        <RelationBadge kind="blocking" count={nib.blockingIds.length} variant={blockedVariant} />
      {/if}
    </div>
  </td>
{/snippet}

<!-- Status column -->
{#snippet cellStatus(ctx: RowContext)}
  {@const nib = ctx.nib}
  {@const statusDotColor = statusDotColors[nib.status] ?? "var(--muted-foreground)"}
  <td data-testid="nib-status" class="text-body px-3 cell-truncate row-cell">
    <StatusIcon status={nib.status} class="mr-1.5" />
    <span style="color: {statusDotColor};">{nib.status}</span>
  </td>
{/snippet}

<!-- Effort column -->
{#snippet cellEffort(ctx: RowContext)}
  {@const nib = ctx.nib}
  <td data-testid="nib-effort" class="text-body px-3 cell-truncate row-cell">
    {#if nib.estimate?.trim()}
      {nib.estimate.toUpperCase()}
    {/if}
  </td>
{/snippet}

<!-- Tags column -->
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

<!-- Blocking column (opt-in) -->
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

<!-- Blocked by column (opt-in) -->
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

<!-- Created column (opt-in). Relative age with the full ISO timestamp on hover.
     Bucket rows have an empty createdAt, so the formatter returns "" (blank). -->
{#snippet cellCreated(ctx: RowContext)}
  {@const nib = ctx.nib}
  <td data-testid="nib-created" class="text-body px-3 cell-truncate row-cell" style="color: var(--muted-foreground);" title={formatAbsolute(nib.createdAt)}>{formatRelative(nib.createdAt)}</td>
{/snippet}

<!-- Modified column. Relative age with the full ISO timestamp on hover. -->
{#snippet cellModified(ctx: RowContext)}
  {@const nib = ctx.nib}
  <td data-testid="nib-modified" class="text-body px-3 cell-truncate row-cell" style="color: var(--muted-foreground);" title={formatAbsolute(nib.updatedAt)}>{formatRelative(nib.updatedAt)}</td>
{/snippet}

<style>
  /* Cell styles moved here alongside the cell markup they target: Svelte scopes
     CSS to the component that owns the elements, and these <td>/content elements
     now live in this file's snippets. `.row-cell` is intentionally duplicated in
     TreeTableRow, which still owns the actions cell. */
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

  /* Extra horizontal space between the type icon and the title (Title column
     only). em-based so it scales with the row font-size, matching the inline
     checkbox convention in .prose-nib (app.css). */
  .type-icon-gap {
    display: inline-flex;
    flex-shrink: 0;
    margin-inline-end: 0.35em;
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
    /* Join the row's type scale (14px) instead of inheriting the 16px root;
       the title stays the primary column via weight, not size. */
    font-size: var(--text-body-size);
    font-weight: 500;
    line-height: var(--text-body-leading);
    text-align: left;
  }
</style>
