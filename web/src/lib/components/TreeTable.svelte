<script lang="ts">
  import { queryStore, subscriptionStore, getContextClient } from "@urql/svelte";
  import { TREE_TABLE_QUERY, NIB_CHANGED_SUBSCRIPTION } from "../queries";
  import { ALL_COLUMN_KEYS, DEFAULT_BLOCKED_EMPHASIS } from "../types";
  import type { NibFilter, ViewLevel, ColumnKey, RowDensity, BlockedEmphasis, RowSubtreeActions, TreeTableNib } from "../types";
  import type { Preferences } from "../preferences.svelte";
  import { buildTableData } from "../tableData";
  import { isBucketId, bucketIdForItem, buildViewTree, collectDescendantIds } from "../tree";
  import { prepareFilter, isDragAllowed, matchesFilter } from "../filter";
  import { resolveFilter, resolveViewLevel, resolveVisibleColumns, resolveColumnWidths } from "../resolvePrefs";
  import TreeTableRow from "./TreeTableRow.svelte";
  import { CopyPlus, CopyMinus } from "@lucide/svelte";
  import type { DropZone } from "../drag.svelte";
  import { useSelection, useDrag, useActiveView, useTreeView } from "../contexts";
  import { useColumnResize } from "../composables/useColumnResize.svelte";
  import { useTreeDrag } from "../composables/useTreeDrag.svelte";
  import { useKeyboardNav } from "../composables/useKeyboardNav.svelte";
  import { useScrollRestore } from "../composables/useScrollRestore.svelte";
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
    onrowcontextmenu?: (nibId: string, event: MouseEvent, nib: TreeTableNib, subtree: RowSubtreeActions) => void;
    onaddchild?: (nibId: string, nibType: string, anchor: DOMRect) => void;
    rowDensity?: RowDensity;
    blockedEmphasis?: BlockedEmphasis;
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
    blockedEmphasis = DEFAULT_BLOCKED_EMPHASIS as BlockedEmphasis,
    ondrop,
  }: Props = $props();

  const selection = useSelection();
  const drag = useDrag();
  // Explicit navigation (title/row click, keyboard Enter) opens the unified view,
  // which routes through the dirty-guard + nav (URL/history). Multi-select stays
  // on SelectionState directly; the view follows via syncTo (documented bypass).
  const view = useActiveView();
  // Collapse state is owned by TreeViewState (provided in App.svelte, outside the
  // {#key position} block) so it survives a TreeTable remount on a dock-position
  // toggle — see treeView.svelte.ts (nibs-a5sb, review #1).
  const treeView = useTreeView();

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

  let allNibs = $derived($result.data?.nibs ?? []);

  // The client-side filter is the original filter (not the server-stripped version).
  // buildTableData uses hasClientFilters/matchesFilter from filter.ts directly.
  let tableData = $derived(buildTableData(allNibs, resolvedFilter, resolvedViewLevel, treeView.collapsedIds));
  let rows = $derived(tableData.rows);
  let parentIds = $derived(tableData.parentIds);
  let visibleRowIds = $derived(rows.map(r => r.nib.id));

  // Structural equality for two string sets (size + membership).
  function sameSet(a: ReadonlySet<string>, b: ReadonlySet<string>): boolean {
    if (a.size !== b.size) return false;
    for (const x of a) {
      if (!b.has(x)) return false;
    }
    return true;
  }

  // --- Prune multi-select of filtered-out nibs ---
  // When a client-side filter narrows the dataset, any previously multi-selected
  // nib that no longer matches must be dropped from the selection — otherwise a
  // bulk mutation or multi-drag silently targets rows the user can no longer see
  // (nibs-mpkm; drag stays enabled under hide-filters, so this is reachable). The
  // "matching set" is strictly `matchesFilter` (dimmed ancestors shown only for
  // tree context are excluded); with no client filters active matchesFilter() is
  // true for everything, so nothing is pruned — collapsing a parent never
  // deselects its (still-in-dataset) children.
  $effect(() => {
    // Don't prune while the first query is still in flight: allNibs is
    // transiently [] before the result lands, and a cold deep-link populates the
    // selection (via syncFromUrl) before data arrives — pruning against an empty
    // dataset would wrongly drop it. Reading $result.fetching also re-subscribes
    // so pruning runs once data settles (mirrors the ensure-visible loading guard).
    if ($result.fetching) return;
    const nibs = allNibs;
    const filter = resolvedFilter;
    const matchingIds = new Set<string>();
    for (const nib of nibs) {
      if (matchesFilter(nib, filter)) matchingIds.add(nib.id);
    }
    // retainOnly reads selection.* — run untracked so those reads don't subscribe
    // this effect (which writes them) and cause effect_update_depth_exceeded. It
    // is a no-op when nothing drops, so re-runs on unrelated data changes are cheap.
    untrack(() => selection.retainOnly(matchingIds));
  });

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
      const next = new Set(treeView.collapsedIds);
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
      if (sameSet(next, treeView.collapsedIds)) {
        selection.clearEnsureVisible();
        return;
      }
      // Ancestors were collapsed — expand them and let the effect re-run once
      // visibleRowIds updates (either the nib appears, or the next pass hits
      // the filtered-out guard above and clears).
      treeView.setCollapsed(next);
      return;
    }

    // Nib is visible — scroll into view and clear
    const scrollContainer = scrollContainerEl;
    if (scrollContainer) {
      const tr = scrollContainer.querySelector(`tr[data-nib-id="${nibId}"]`);
      if (tr) {
        tr.scrollIntoView({ block: "nearest" });
        // Persist the deep-link offset synchronously AND claim the container, so
        // the restore effect can't reset this scroll and a refetch that unmounts
        // the container before the async scroll event can't lose it (nibs-n47p
        // review #1). claim() self-locates the live container via getScrollContainer().
        scrollRestore.claim();
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
    treeView.toggle(id);
  }

  function expandAll() {
    treeView.expandAll();
  }

  function collapseAll() {
    treeView.collapseAll(parentIds);
  }

  // --- Subtree expand/collapse (row context menu, nibs-iyw3) ---
  // Descendants are resolved against the DISPLAYED view tree (buildViewTree), not
  // raw parentId, so the grouping lens (headers, hidden containers, "No X"
  // buckets) is honoured. TreeViewState owns the collapse set; these compute the
  // next set and hand it to setCollapsed.
  function expandSubtree(rootId: string) {
    const viewTree = buildViewTree<TreeTableNib>(allNibs, resolvedViewLevel);
    const descendantIds = collectDescendantIds(viewTree, rootId);
    const next = new Set(treeView.collapsedIds);
    next.delete(rootId);
    for (const id of descendantIds) next.delete(id);
    if (!sameSet(next, treeView.collapsedIds)) treeView.setCollapsed(next);
  }

  function collapseSubtree(rootId: string) {
    const viewTree = buildViewTree<TreeTableNib>(allNibs, resolvedViewLevel);
    const descendantIds = collectDescendantIds(viewTree, rootId);
    const next = new Set(treeView.collapsedIds);
    // Collapse the row itself plus every descendant that actually has children,
    // so re-expanding the row reveals exactly one level at a time. parentIds
    // already folds in synthetic buckets (tableData Stage 5a).
    next.add(rootId);
    for (const id of descendantIds) {
      if (parentIds.has(id)) next.add(id);
    }
    if (!sameSet(next, treeView.collapsedIds)) treeView.setCollapsed(next);
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

  // --- Scroll-position restore (composable) ---
  // Saves/restores the scroll offset across App's {#key position} remount (the
  // detail-panel dock toggle recreates this container at scrollTop=0). The saved
  // value lives in TreeViewState outside the keyed block, the same way
  // collapsedIds survives the remount (nibs-n47p).
  const scrollRestore = useScrollRestore({
    getScrollContainer: () => scrollContainerEl ?? null,
    getSavedScrollTop: () => treeView.scrollTop,
    setSavedScrollTop: (n) => { treeView.scrollTop = n; },
    hasContent: () => rows.length > 0,
  });

  // Re-attempt the restore whenever the container binds or the rows change: after
  // a {#key} remount the fresh container starts at scrollTop=0, and restore() only
  // applies the saved offset once content is present (then it's a no-op). Touch
  // both deps so the effect re-runs across the remount + first render.
  $effect(() => {
    void scrollContainerEl; void rows.length;
    // untrack the restore call so the effect keeps only its two intended deps
    // (container binding + row count) and takes no incidental dependency on
    // treeView.scrollTop read inside restore(), mirroring the file's convention
    // for self-feeding side-effects (nibs-n47p review #3).
    untrack(() => scrollRestore.restore());
  });

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
    getCollapsedIds: () => treeView.collapsedIds,
    toggleNode,
    getScrollContainer: () => scrollContainerEl ?? null,
    onDragKeyDown: treeDrag.onDragKeyDown,
    navigateToNib: (id) => openOrToggleBucket(id),
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

  // Row-open guard for synthetic grouping buckets. A "No X" bucket row
  // (isBucketId) is not a real nib, so routing its synthetic id through
  // view.open resolves an empty detail query and fires the missing-nib
  // ("no longer exists") heal path (nibs-gkwg). Instead, opening a bucket
  // toggles/collapses its group — the same effect as its caret — mirroring the
  // drag guard that already skips buckets (isBucketId, ~lines 428/540).
  function openOrToggleBucket(id: string) {
    if (isBucketId(id)) {
      toggleNode(id);
      return;
    }
    void view.open(id);
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
      openOrToggleBucket(nibId);
      return; // Title click selects, not row click
    }

    if (action === "add-child") {
      const nibType = actionResult!.el.dataset.childType ?? "";
      // Anchor the type picker to the clicked [+] button (its viewport rect).
      onaddchild?.(nibId, nibType, actionResult!.el.getBoundingClientRect());
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
      // Multi-select desync: let the view follow the (possibly collapsed-to-one)
      // selection without a dirty-prompt — the documented guard-bypass path.
      view.syncTo(selection.selectedNibId);
    } else if (e.ctrlKey || e.metaKey) {
      selection.toggleSelect(nibId);
      view.syncTo(selection.selectedNibId);
    } else {
      openOrToggleBucket(nibId);
    }
  }

  function handleDelegatedDblClick(e: MouseEvent) {
    if (drag.isDragging) return;

    const nibId = getNibIdFromEvent(e);
    if (!nibId) return;

    openOrToggleBucket(nibId);
  }

  function handleDelegatedContextMenu(e: MouseEvent) {
    const nibId = getNibIdFromEvent(e);
    if (!nibId) return;

    const row = rows.find(r => r.nib.id === nibId);
    if (!row) return;

    e.preventDefault();
    onrowcontextmenu?.(nibId, e, row.nib, {
      hasChildren: row.hasChildren,
      expandChildren: () => expandSubtree(nibId),
      collapseChildren: () => collapseSubtree(nibId),
    });
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
  <div bind:this={scrollContainerEl} class="overflow-auto h-full scroll-container" role="grid" tabindex="0" onkeydown={keyboardNav.handleKeydown} onscroll={scrollRestore.onScroll} onclick={handleDelegatedClick} ondblclick={handleDelegatedDblClick} oncontextmenu={handleDelegatedContextMenu} onpointerdown={handleDelegatedPointerDown} style="--row-pad-y: calc({rowDensity === 'comfortable' ? '0.625rem' : '0.25rem'} * var(--font-scale))">
  <table bind:this={tableEl} class="border-collapse" style="table-layout: fixed; width: {tableWidth}px;">
    <thead class="sticky top-0" style="z-index: var(--z-sticky);">
      <tr>
        <th class="w-8 bg-background" style="width: 32px;">
          <!-- Raw buttons: two 12px icon controls must fit inside the 32px-wide
               actions column; smaller than the Button primitive's minimum size. -->
          <div class="flex items-center">
            <button data-testid="expand-all" class="rounded-sm p-0.5 text-muted-foreground hover:text-foreground" onclick={expandAll} title="Expand all">
              <CopyPlus size={12} />
            </button>
            <button data-testid="collapse-all" class="rounded-sm p-0.5 text-muted-foreground hover:text-foreground" onclick={collapseAll} title="Collapse all">
              <CopyMinus size={12} />
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
        {#if showColumn("blocking")}
          <th class="text-left text-label text-muted-foreground px-3 py-2 relative bg-background" style="width: {resolvedColumnWidths.blocking}px;">
            Blocking
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="resize-handle" onpointerdown={(e) => columnResize.onPointerDown(e, "blocking")} onpointermove={columnResize.onPointerMove} onpointerup={columnResize.onPointerUp} ondblclick={() => columnResize.onDblClick("blocking", showColumn)}></div>
          </th>
        {/if}
        {#if showColumn("blockedBy")}
          <th class="text-left text-label text-muted-foreground px-3 py-2 relative bg-background" style="width: {resolvedColumnWidths.blockedBy}px;">
            Blocked by
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="resize-handle" onpointerdown={(e) => columnResize.onPointerDown(e, "blockedBy")} onpointermove={columnResize.onPointerMove} onpointerup={columnResize.onPointerUp} ondblclick={() => columnResize.onDblClick("blockedBy", showColumn)}></div>
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
          collapsed={treeView.isCollapsed(row.nib.id)}
          parentNib={row.parentNib}
          visibleColumns={resolvedVisibleColumns}
          draggable={!isBucketId(row.nib.id) && dragAllowed}
          highlighted={changeTracker.isHighlighted(row.nib.id)}
          fading={changeTracker.isFading(row.nib.id)}
          {blockedEmphasis}
        />
      {/each}
    </tbody>
  </table>
  </div>
{/if}
</div>
