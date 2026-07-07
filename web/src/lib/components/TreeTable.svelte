<script lang="ts">
  import { queryStore, subscriptionStore, getContextClient } from "@urql/svelte";
  import { TREE_TABLE_QUERY, NIB_CHANGED_SUBSCRIPTION } from "../queries";
  import { ALL_COLUMN_KEYS } from "../types";
  import type { NibFilter, ViewLevel, ColumnKey, RowDensity } from "../types";
  import type { Preferences } from "../preferences.svelte";
  import { buildTableData } from "../tableData";
  import { isBucketId, bucketIdForItem } from "../tree";
  import { prepareFilter, isDragAllowed } from "../filter";
  import { resolveFilter, resolveViewLevel, resolveVisibleColumns, resolveColumnWidths } from "../resolvePrefs";
  import TreeTableRow from "./TreeTableRow.svelte";
  import { Plus, Minus } from "@lucide/svelte";
  import type { DropZone } from "../drag.svelte";
  import { useSelection, useDrag, useHistoryNav } from "../contexts";
  import { useColumnResize } from "../composables/useColumnResize.svelte";
  import { useTreeDrag } from "../composables/useTreeDrag.svelte";
  import { useKeyboardNav } from "../composables/useKeyboardNav.svelte";
  import { NibChangeTracker } from "../changeTracker.svelte";
  import { onDestroy, untrack } from "svelte";

  interface Props {
    prefs?: Preferences;
    filter?: NibFilter;
    viewLevel?: ViewLevel;
    visibleColumns?: ColumnKey[];
    columnWidths?: Record<ColumnKey, number>;
    oncolumnwidthschange?: (widths: Record<ColumnKey, number>) => void;
    oncolumnresizeend?: () => void;
    ontagschange?: (tags: string[]) => void;
    onrowcontextmenu?: (nibId: string, event: MouseEvent, nib: import("../types").TreeTableNib) => void;
    onaddchild?: (nibId: string, nibType: string) => void;
    rowDensity?: RowDensity;
    ondrop?: (targetNibId: string, zone: DropZone, targetParentId: string | null) => void;
  }

  let {
    prefs = undefined as Preferences | undefined,
    filter = undefined as NibFilter | undefined,
    viewLevel = undefined as ViewLevel | undefined,
    visibleColumns = undefined as ColumnKey[] | undefined,
    columnWidths = undefined as Record<ColumnKey, number> | undefined,
    oncolumnwidthschange,
    oncolumnresizeend,
    ontagschange,
    onrowcontextmenu,
    onaddchild,
    rowDensity = "compact" as RowDensity,
    ondrop,
  }: Props = $props();

  const selection = useSelection();
  const drag = useDrag();
  const nav = useHistoryNav();

  // Resolve values: prefs takes precedence over individual props
  let resolvedFilter = $derived(resolveFilter(prefs, filter));
  let resolvedViewLevel = $derived(resolveViewLevel(prefs, viewLevel));
  let resolvedVisibleColumns = $derived(resolveVisibleColumns(prefs, visibleColumns));
  let resolvedColumnWidths = $derived(resolveColumnWidths(prefs, columnWidths));

  let dragAllowed = $derived(isDragAllowed(resolvedFilter));
  let showColumn = $derived((key: ColumnKey) => resolvedVisibleColumns.includes(key));

  // Explicit table width = actions column (32px) + sum of visible column widths.
  // Required for table-layout: fixed to enforce column widths regardless of content.
  let tableWidth = $derived(
    32 + ALL_COLUMN_KEYS.reduce((sum, key) => showColumn(key) ? sum + resolvedColumnWidths[key] : sum, 0)
  );

  // Split filter into server-side (sent to GraphQL) and client-side (applied locally).
  // This ensures we fetch ancestor nibs from the server so the tree stays intact,
  // while still filtering by type/priority/estimate/tags/status on the client.
  let prepared = $derived(prepareFilter(resolvedFilter));

  const client = getContextClient();

  const result = $derived(
    queryStore({
      client,
      query: TREE_TABLE_QUERY,
      variables: { filter: prepared.serverFilter },
    })
  );

  // --- Real-time subscription for nib changes ---
  const changeTracker = new NibChangeTracker();
  onDestroy(() => changeTracker.destroy());

  const subscription = subscriptionStore({
    client,
    query: NIB_CHANGED_SUBSCRIPTION,
  });

  // Track the last-seen event via a stable content key. urql's
  // subscription store emits a fresh wrapper object on every reactive
  // cycle, so reference comparison is unreliable — compare by content
  // instead. Plain let (not $state) so writes do not re-trigger the effect.
  let lastEventKey = "";

  $effect(() => {
    if ($subscription.error) {
      console.warn("Nib subscription error:", $subscription.error);
    }
  });

  $effect(() => {
    const data = $subscription.data;
    if (!data?.nibChanged) return;
    const event = data.nibChanged as { type: string; nibId: string };
    const key = `${event.type}:${event.nibId}`;

    // All side-effects (changeTracker writes, query reexecute) must run
    // untracked so they do not feed back into this effect and cause a
    // reactive loop.
    untrack(() => {
      if (key === lastEventKey) return;
      lastEventKey = key;

      changeTracker.handleEvent(event);

      if (event.type === "deleted") {
        setTimeout(() => {
          result.reexecute({ requestPolicy: "network-only" });
        }, changeTracker.fadeDurationMs);
      } else {
        result.reexecute({ requestPolicy: "network-only" });
      }
    });
  });

  let collapsedIds: Set<string> = $state(new Set());

  let allNibs = $derived($result.data?.nibs ?? []);

  // The client-side filter is the original filter (not the server-stripped version).
  // buildTableData uses hasClientFilters/matchesFilter from filter.ts directly.
  let tableData = $derived(buildTableData(allNibs, resolvedFilter, resolvedViewLevel, collapsedIds));
  let rows = $derived(tableData.rows);
  let parentIds = $derived(tableData.parentIds);
  let visibleRowIds = $derived(rows.map(r => r.nib.id));

  // Structural equality for two string sets (size + membership).
  function sameSet(a: Set<string>, b: Set<string>): boolean {
    if (a.size !== b.size) return false;
    for (const x of a) {
      if (!b.has(x)) return false;
    }
    return true;
  }

  // --- Ensure-visible: expand collapsed ancestors and scroll into view ---
  $effect(() => {
    const nibId = selection.pendingEnsureVisibleId;
    if (!nibId) return;

    // Build a lookup for all nibs (needed to walk parent chain)
    const nibMap = new Map<string, (typeof allNibs)[number]>();
    for (const nib of allNibs) {
      nibMap.set(nib.id, nib);
    }

    // The nib isn't in the dataset. Two distinct cases must NOT be conflated:
    //   - Query still loading (cold deep-link fires syncFromUrl before the
    //     GraphQL result lands, so allNibs is []): keep the pending request so
    //     the expand/scroll runs once data arrives (nibs-58c3 AC3). Reading
    //     $result.fetching also subscribes the effect to re-run on settle.
    //   - Query settled and the nib is genuinely absent (archived/bad URL):
    //     clear and bail — there is nothing to scroll to.
    if (!nibMap.has(nibId)) {
      if (!$result.fetching) {
        selection.clearEnsureVisible();
      }
      return;
    }

    // The nib is in the dataset but not currently visible. Try to expand its
    // collapsed ancestors so it becomes reachable.
    if (!visibleRowIds.includes(nibId)) {
      const next = new Set(collapsedIds);
      let current = nibMap.get(nibId);
      // Guard against a parentId cycle (A->B->A) — only possible via corrupt
      // .nibs data, but an unguarded walk here would spin forever inside this
      // reactive $effect and hang the tab. Mirrors the visited guard in
      // tableData.ts's ancestor walk.
      const visited = new Set<string>();
      while (current?.parentId && !visited.has(current.parentId)) {
        visited.add(current.parentId);
        next.delete(current.parentId);
        current = nibMap.get(current.parentId);
      }
      // The target may sit inside a synthetic "No X" bucket, which is never any
      // real nib's parentId — so the chain walk above cannot un-collapse it.
      // Un-collapse the enclosing bucket for the current lens too.
      const bucketId = bucketIdForItem(nibMap, nibId, resolvedViewLevel);
      if (bucketId) next.delete(bucketId);
      // If expansion changes nothing yet the nib is still not visible, it is in
      // the dataset but excluded from the visible rows by an active client
      // filter, regardless of collapse state. (Grouping lenses are lossless —
      // buildViewTree never drops a work item — so the lens alone cannot hide it.)
      // Ancestor-expansion can never reveal it, so clear and bail — otherwise
      // reassigning `collapsedIds` to a new Set every pass would loop forever
      // (effect_update_depth_exceeded).
      if (sameSet(next, collapsedIds)) {
        selection.clearEnsureVisible();
        return;
      }
      // Ancestors were collapsed — expand them and let the effect re-run once
      // visibleRowIds updates (either the nib appears, or the next pass hits
      // the filtered-out guard above and clears).
      collapsedIds = next;
      return;
    }

    // Nib is visible — scroll into view and clear
    const scrollContainer = scrollContainerEl;
    if (scrollContainer) {
      const tr = scrollContainer.querySelector(`tr[data-nib-id="${nibId}"]`);
      if (tr) {
        tr.scrollIntoView({ block: "nearest" });
      }
    }
    selection.clearEnsureVisible();
  });

  // Emit unique tags from all nibs to parent component (memoized to avoid re-render cascades)
  let prevTagsKey = "";
  $effect(() => {
    if (ontagschange) {
      const key = tableData.allTags.join(",");
      if (key !== prevTagsKey) {
        prevTagsKey = key;
        ontagschange(tableData.allTags);
      }
    }
  });

  function toggleNode(id: string) {
    const next = new Set(collapsedIds);
    if (next.has(id)) {
      next.delete(id);
    } else {
      next.add(id);
    }
    collapsedIds = next;
  }

  function expandAll() {
    collapsedIds = new Set();
  }

  function collapseAll() {
    collapsedIds = new Set(parentIds);
  }

  // --- Column resize (composable) ---
  let tableEl: HTMLTableElement | undefined = $state(undefined);

  const columnResize = useColumnResize({
    getTableEl: () => tableEl ?? null,
    getColumnWidths: () => resolvedColumnWidths,
    setColumnWidth: (key: ColumnKey, width: number) => {
      if (prefs) {
        prefs.setColumnWidth(key, width);
      } else if (oncolumnwidthschange) {
        oncolumnwidthschange({ ...resolvedColumnWidths, [key]: width });
      }
    },
    onResizeEnd: () => {
      if (prefs) {
        prefs.flushColumnWidths();
      } else {
        oncolumnresizeend?.();
      }
    },
  });

  // --- Scroll container ---
  let scrollContainerEl: HTMLDivElement | undefined = $state(undefined);

  // --- Drag-and-drop (composable) ---
  const treeDrag = useTreeDrag({
    selection,
    drag,
    getRows: () => rows,
    getScrollContainer: () => scrollContainerEl ?? null,
    ondrop: (targetNibId, zone, targetParentId) => ondrop?.(targetNibId, zone, targetParentId),
  });

  // --- Keyboard navigation (composable) ---
  const keyboardNav = useKeyboardNav({
    selection,
    getRows: () => rows,
    getVisibleRowIds: () => visibleRowIds,
    getCollapsedIds: () => collapsedIds,
    toggleNode,
    getScrollContainer: () => scrollContainerEl ?? null,
    onDragKeyDown: treeDrag.onDragKeyDown,
    navigateToNib: nav.navigateToNib,
  });

  // --- Event delegation helpers ---
  // Extract the nib ID from the closest <tr data-nib-id="..."> ancestor
  function getNibIdFromEvent(e: Event): string | null {
    const target = e.target as HTMLElement;
    const tr = target.closest("tr[data-nib-id]") as HTMLElement | null;
    return tr?.dataset.nibId ?? null;
  }

  // Get the data-action attribute and element from the event target or its ancestors (up to the <tr>)
  function getActionFromEvent(e: Event): { action: string; el: HTMLElement } | null {
    const target = e.target as HTMLElement;
    const actionEl = target.closest("[data-action]") as HTMLElement | null;
    if (!actionEl) return null;
    // Make sure the action element is inside a table row (not from header or outside)
    const tr = actionEl.closest("tr[data-nib-id]");
    if (!tr) return null;
    const action = actionEl.dataset.action;
    if (!action) return null;
    return { action, el: actionEl };
  }

  function handleDelegatedClick(e: MouseEvent) {
    if (drag.isDragging) return;

    const nibId = getNibIdFromEvent(e);
    if (!nibId) return;

    const actionResult = getActionFromEvent(e);
    const action = actionResult?.action ?? null;

    if (action === "toggle") {
      toggleNode(nibId);
      return; // Don't fire row click for toggle
    }

    if (action === "title") {
      nav.navigateToNib(nibId);
      return; // Title click selects, not row click
    }

    if (action === "add-child") {
      const nibType = actionResult!.el.dataset.childType ?? "";
      onaddchild?.(nibId, nibType);
      return; // Don't fire row click for add-child
    }

    // Default: row click with modifier handling.
    // Shift/Ctrl-Cmd are bulk-selection gestures — intentionally NOT routed
    // through nav, so they record no Back/Forward history entry. Note that a
    // collapse-to-exactly-one still opens the single-nib panel (and collapse-to-
    // zero closes it) without a history push, so URL/history can lag selection
    // after these gestures; that's accepted because multi-select is a bulk
    // gesture, not detail-panel navigation (nibs-58c3).
    // Only a plain click is treated as navigation.
    if (e.shiftKey) {
      selection.rangeSelect(nibId, visibleRowIds);
    } else if (e.ctrlKey || e.metaKey) {
      selection.toggleSelect(nibId);
    } else {
      nav.navigateToNib(nibId);
    }
  }

  function handleDelegatedDblClick(e: MouseEvent) {
    if (drag.isDragging) return;

    const nibId = getNibIdFromEvent(e);
    if (!nibId) return;

    nav.navigateToNib(nibId);
  }

  function handleDelegatedContextMenu(e: MouseEvent) {
    const nibId = getNibIdFromEvent(e);
    if (!nibId) return;

    const row = rows.find(r => r.nib.id === nibId);
    if (!row) return;

    e.preventDefault();
    onrowcontextmenu?.(nibId, e, row.nib);
  }

  function handleDelegatedPointerDown(e: PointerEvent) {
    // Only left-click initiates drag
    if (e.button !== 0) return;

    const nibId = getNibIdFromEvent(e);
    if (!nibId) return;

    // Only draggable rows can initiate drag
    if (isBucketId(nibId) || !dragAllowed) return;

    treeDrag.onRowPointerDown(nibId, e);
  }
</script>

<div data-testid="tree-table" class="h-full">
{#if $result.fetching}
  <div class="flex items-center justify-center py-12 text-body text-muted-foreground">
    <span>Loading...</span>
  </div>
{:else if $result.error}
  <div class="rounded-lg bg-destructive/10 px-4 py-3 text-body text-destructive">
    Error: {$result.error.message}
  </div>
{:else if rows.length === 0}
  <div class="flex items-center justify-center py-12 text-body text-muted-foreground">
    <span>No nibs found</span>
  </div>
{:else}
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div bind:this={scrollContainerEl} class="overflow-auto h-full scroll-container" role="grid" tabindex="0" onkeydown={keyboardNav.handleKeydown} onclick={handleDelegatedClick} ondblclick={handleDelegatedDblClick} oncontextmenu={handleDelegatedContextMenu} onpointerdown={handleDelegatedPointerDown} style="--row-pad-y: {rowDensity === 'comfortable' ? '0.625rem' : '0.25rem'}">
  <table bind:this={tableEl} class="border-collapse" style="table-layout: fixed; width: {tableWidth}px;">
    <thead class="sticky top-0" style="z-index: var(--z-sticky);">
      <tr>
        <th class="w-8 bg-background" style="width: 32px;">
          <!-- Raw buttons: two 12px icon controls must fit inside the 32px-wide
               actions column; smaller than the Button primitive's minimum size. -->
          <div class="flex items-center">
            <button data-testid="expand-all" class="rounded-sm p-0.5 text-muted-foreground hover:text-foreground" onclick={expandAll} title="Expand all">
              <Plus size={12} />
            </button>
            <button data-testid="collapse-all" class="rounded-sm p-0.5 text-muted-foreground hover:text-foreground" onclick={collapseAll} title="Collapse all">
              <Minus size={12} />
            </button>
          </div>
        </th>
        {#if showColumn("id")}
          <th class="text-left text-label text-muted-foreground px-3 py-2 relative bg-background" style="width: {resolvedColumnWidths.id}px;">
            ID
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="resize-handle" onpointerdown={(e) => columnResize.onPointerDown(e, "id")} onpointermove={columnResize.onPointerMove} onpointerup={columnResize.onPointerUp} ondblclick={() => columnResize.onDblClick("id", showColumn)}></div>
          </th>
        {/if}
        {#if showColumn("parent")}
          <th class="text-left text-label text-muted-foreground px-3 py-2 relative bg-background" style="width: {resolvedColumnWidths.parent}px;">
            Parent
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="resize-handle" onpointerdown={(e) => columnResize.onPointerDown(e, "parent")} onpointermove={columnResize.onPointerMove} onpointerup={columnResize.onPointerUp} ondblclick={() => columnResize.onDblClick("parent", showColumn)}></div>
          </th>
        {/if}
        {#if showColumn("type")}
          <th class="text-left text-label text-muted-foreground px-3 py-2 relative bg-background" style="width: {resolvedColumnWidths.type}px;">
            Type
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="resize-handle" onpointerdown={(e) => columnResize.onPointerDown(e, "type")} onpointermove={columnResize.onPointerMove} onpointerup={columnResize.onPointerUp} ondblclick={() => columnResize.onDblClick("type", showColumn)}></div>
          </th>
        {/if}
        {#if showColumn("title")}
          <th class="text-left text-label text-muted-foreground px-3 py-2 relative bg-background" style="width: {resolvedColumnWidths.title}px;">
            Title
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="resize-handle" onpointerdown={(e) => columnResize.onPointerDown(e, "title")} onpointermove={columnResize.onPointerMove} onpointerup={columnResize.onPointerUp} ondblclick={() => columnResize.onDblClick("title", showColumn)}></div>
          </th>
        {/if}
        {#if showColumn("state")}
          <th class="text-left text-label text-muted-foreground px-3 py-2 relative bg-background" style="width: {resolvedColumnWidths.state}px;">
            State
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="resize-handle" onpointerdown={(e) => columnResize.onPointerDown(e, "state")} onpointermove={columnResize.onPointerMove} onpointerup={columnResize.onPointerUp} ondblclick={() => columnResize.onDblClick("state", showColumn)}></div>
          </th>
        {/if}
        {#if showColumn("effort")}
          <th class="text-left text-label text-muted-foreground px-3 py-2 relative bg-background" style="width: {resolvedColumnWidths.effort}px;">
            Effort
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="resize-handle" onpointerdown={(e) => columnResize.onPointerDown(e, "effort")} onpointermove={columnResize.onPointerMove} onpointerup={columnResize.onPointerUp} ondblclick={() => columnResize.onDblClick("effort", showColumn)}></div>
          </th>
        {/if}
        {#if showColumn("tags")}
          <th class="text-left text-label text-muted-foreground px-3 py-2 relative bg-background" style="width: {resolvedColumnWidths.tags}px;">
            Tags
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="resize-handle" onpointerdown={(e) => columnResize.onPointerDown(e, "tags")} onpointermove={columnResize.onPointerMove} onpointerup={columnResize.onPointerUp} ondblclick={() => columnResize.onDblClick("tags", showColumn)}></div>
          </th>
        {/if}
      </tr>
    </thead>
    <tbody>
      {#each rows as row (row.nib.id)}
        <TreeTableRow
          nib={row.nib}
          depth={row.depth}
          hasChildren={row.hasChildren}
          dimmed={row.dimmed}
          collapsed={collapsedIds.has(row.nib.id)}
          parentNib={row.parentNib}
          visibleColumns={resolvedVisibleColumns}
          draggable={!isBucketId(row.nib.id) && dragAllowed}
          highlighted={changeTracker.isHighlighted(row.nib.id)}
          fading={changeTracker.isFading(row.nib.id)}
        />
      {/each}
    </tbody>
  </table>
  </div>
{/if}
</div>
