<script lang="ts">
  import { getContextClient } from "@urql/svelte";
  import { DEFAULT_BLOCKED_EMPHASIS, DEFAULT_OPEN_DETAIL_ON } from "../types";
  import type { NibFilter, ViewLevel, RowDensity, BlockedEmphasis, OpenDetailGesture, RowSubtreeActions, TreeTableNib, TableSort, SortField } from "../types";
  import type { ColumnKey } from "../columns";
  import type { Preferences } from "../preferences.svelte";
  import { buildTableData } from "../tableData";
  import { isBucketId, bucketIdForItem, buildViewTree, collectDescendantIds } from "../tree";
  import { applySort, nextTableSort } from "../tableSort";
  import { prepareFilter, matchesFilter } from "../filter";
  import { dragBlockFor, DRAG_BLOCK_TOAST_ID } from "../dragBlock";
  import type { DragBlock } from "../dragBlock";
  import { toast } from "svelte-sonner";
  import { resolveFilter, resolveViewLevel, resolveVisibleColumns, resolveColumnWidths, resolveColumnOrder, resolveTableSort, emitFilter, emitTableSort, emitColumnOrder, emitViewLevel } from "../resolvePrefs";
  import { hierarchyTokens, clearHierarchyFilters, contradictionTokens } from "../query";
  import { graphqlErrorCode, graphqlErrorMessage } from "../graphqlError";
  import { Button } from "$lib/components/ui/button/index.js";
  import TreeTableRow from "./TreeTableRow.svelte";
  import TableHeader from "./TableHeader.svelte";
  import type { DropZone } from "../drag.svelte";
  import type { PanelPolicy } from "../selection.svelte";
  import { useSelection, useDrag, useActiveView, useTreeView, useConnection } from "../contexts";
  import { useColumnResize } from "../composables/useColumnResize.svelte";
  import { useColumnDrag } from "../composables/useColumnDrag.svelte";
  import { useTreeDrag } from "../composables/useTreeDrag.svelte";
  import { useKeyboardNav } from "../composables/useKeyboardNav.svelte";
  import { useScrollRestore } from "../composables/useScrollRestore.svelte";
  import { useTableData } from "../composables/useTableData.svelte";
  import { untrack } from "svelte";

  // Settle time before a filter-box change re-queries the server. Long enough to
  // coalesce a burst of keystrokes, short enough to feel immediate once typing stops.
  const LIST_REFETCH_DEBOUNCE_MS = 250;

  interface Props {
    prefs?: Preferences;
    filter?: NibFilter;
    viewLevel?: ViewLevel;
    visibleColumns?: ColumnKey[];
    columnWidths?: Record<ColumnKey, number>;
    columnOrder?: ColumnKey[];
    tableSort?: TableSort | null;
    ontablesortchange?: (s: TableSort | null) => void;
    /** Write path for the empty state's "clear hierarchy filters" action. Unused
     *  when `prefs` is supplied — the write goes through the preference instead. */
    onfilterchange?: (f: NibFilter) => void;
    /** Write path for the blocked-drag toast's "Switch to Tree" action. Unused
     *  when `prefs` is supplied — the write goes through the preference instead. */
    onviewlevelchange?: (v: ViewLevel) => void;
    oncolumnwidthschange?: (widths: Record<ColumnKey, number>) => void;
    oncolumnresizeend?: () => void;
    oncolumnorderchange?: (order: ColumnKey[]) => void;
    ontagschange?: (tags: string[]) => void;
    onrowcontextmenu?: (nibId: string, event: MouseEvent, nib: TreeTableNib, subtree: RowSubtreeActions) => void;
    onaddchild?: (nibId: string, nibType: string, anchor: DOMRect) => void;
    rowDensity?: RowDensity;
    blockedEmphasis?: BlockedEmphasis;
    /** Which row gesture opens the detail panel. Unused when `prefs` is supplied. */
    openDetailOn?: OpenDetailGesture;
    ondrop?: (targetNibId: string, zone: DropZone, targetParentId: string | null) => void;
  }

  let {
    prefs = undefined as Preferences | undefined,
    filter = undefined as NibFilter | undefined,
    viewLevel = undefined as ViewLevel | undefined,
    visibleColumns = undefined as ColumnKey[] | undefined,
    columnWidths = undefined as Record<ColumnKey, number> | undefined,
    columnOrder = undefined as ColumnKey[] | undefined,
    tableSort = undefined as TableSort | null | undefined,
    ontablesortchange,
    onfilterchange,
    onviewlevelchange,
    oncolumnwidthschange,
    oncolumnresizeend,
    oncolumnorderchange,
    ontagschange,
    onrowcontextmenu,
    onaddchild,
    rowDensity = "compact" as RowDensity,
    blockedEmphasis = DEFAULT_BLOCKED_EMPHASIS as BlockedEmphasis,
    openDetailOn = undefined as OpenDetailGesture | undefined,
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
  // toggle — see treeView.svelte.ts.
  const treeView = useTreeView();

  // Resolve values: prefs takes precedence over individual props
  let resolvedFilter = $derived(resolveFilter(prefs, filter));
  // Canonical tokens for the tree-position constraints currently in force. Two or
  // more of them ANDed together carve a very narrow slice out of an acyclic forest
  // — `ancestor:<parent> descendant:<child>` can match nothing at all — so an empty
  // result under them gets an explanation instead of the generic message.
  let activeHierarchyTokens = $derived(hierarchyTokens(resolvedFilter));
  let resolvedViewLevel = $derived(resolveViewLevel(prefs, viewLevel));
  let resolvedVisibleColumns = $derived(resolveVisibleColumns(prefs, visibleColumns));
  let resolvedColumnWidths = $derived(resolveColumnWidths(prefs, columnWidths));
  // The full per-view column order (all keys). Drives the header + cell + width
  // loops (filtered to the visible set). Reordering writes it back per view.
  let resolvedColumnOrder = $derived(resolveColumnOrder(prefs, columnOrder));
  // Table column sort. Resolved from prefs/prop; APPLIED in every view — a flat
  // sorted list in Flat, sibling-sort (siblings/roots/bucket items/promoted
  // headers reordered, nesting preserved) in the Tree + grouping lenses. Applied
  // only while the sorted column is visible: hiding it deactivates the sort (rows
  // revert to the manual `order` sequence) instead of leaving rows sorted by an
  // invisible field with no header to clear it; the persisted preference is
  // retained, so re-showing the column reactivates it. `activeSort` is the single
  // source of truth for the row order, the header sort indicator, AND the drag
  // gate, so they can never disagree.
  let resolvedTableSort = $derived(resolveTableSort(prefs, tableSort));
  // Which mouse gesture opens a row in the detail panel. Resolved from prefs with
  // a prop fallback, like the resolvers above.
  let resolvedOpenDetailOn = $derived(prefs ? prefs.openDetailOn : (openDetailOn ?? DEFAULT_OPEN_DETAIL_ON));
  let activeSort = $derived(
    resolvedTableSort && resolvedVisibleColumns.includes(resolvedTableSort.field)
      ? resolvedTableSort
      : null
  );

  // Drag-reorder gating. Flat is browse-only (its rows intermix real parents and
  // the reorder backend needs a real sibling drop target). In the Tree + lenses
  // an active sort DISABLES drag: the client-side sort never rewrites the `order`
  // key, so dropping a row would fight the sorted display. Turning the sort off
  // (`activeSort == null`, which also covers the sorted-column-hidden case)
  // restores the exact manual order and re-enables drag.
  //
  // dragBlockFor is the single source of truth for BOTH the gate and the
  // explanation raised on a blocked drag attempt, so the row's affordance and the
  // toast can never disagree about which gate is shut.
  let dragBlock = $derived(dragBlockFor(resolvedFilter, resolvedViewLevel, activeSort));
  let dragAllowed = $derived(dragBlock === null);
  let showColumn = $derived((key: ColumnKey) => resolvedVisibleColumns.includes(key));

  // Visible columns in the per-view order. Drives the <th> loop so the header
  // sequence follows the persisted columnOrder; TreeTableRow filters the same
  // order so cells stay aligned under their headers.
  let orderedVisibleColumns = $derived(resolvedColumnOrder.filter((key) => showColumn(key)));

  // Explicit table width = actions column (32px) + sum of visible column widths.
  // Required for table-layout: fixed to enforce column widths regardless of content.
  // Iterates the ordered set (the sum is order-independent, but keeping the loop on
  // columnOrder single-sources "the columns this table renders").
  let tableWidth = $derived(
    32 + orderedVisibleColumns.reduce((sum, key) => sum + resolvedColumnWidths[key], 0)
  );

  // Split filter into server-side (sent to GraphQL) and client-side (applied locally).
  // This ensures we fetch ancestor nibs from the server so the tree stays intact,
  // while still filtering by type/priority/estimate/tags/status on the client.
  let prepared = $derived(prepareFilter(resolvedFilter));

  const client = getContextClient();

  // Live data source: owns the tree-table list query, the live-refetch decision
  // driven by the nib-change subscription, and the highlight/fade change tracker.
  // The fragile refetch logic (dedup / defer-delete / single-timer / throw
  // isolation) lives in a framework-free core, unit-tested in plain vitest —
  // see composables/useTableData.svelte.ts + tableDataSource.ts.
  const dataSource = useTableData({
    client,
    getServerFilter: () => prepared.serverFilter,
    // Debounce the server refetch so typing free-text in the filter box re-queries
    // once keystrokes settle, not on every character. The box stays live (dropdowns
    // tick, highlighting updates) — only the network list query waits. See nibs-rv7c.
    refetchDebounceMs: LIST_REFETCH_DEBOUNCE_MS,
  });

  // While the live socket is down the list misses every change event, so its
  // cached result is stale by an unknown amount the moment the socket returns.
  // Re-read it then (nibs-1seo). Optional by design — absent outside the app,
  // e.g. in component tests that never disconnect.
  const connection = useConnection();
  $effect(() => connection?.onRecovered(() => dataSource.refetch()));

  // error is `unknown` from the source; the query surfaces urql's CombinedError,
  // whose aggregate `.message` carries a "[GraphQL] " transport prefix no user
  // should read. graphqlErrorMessage prefers the server's own message, and this
  // text is rendered rather than logged.
  let errorMessage = $derived(graphqlErrorMessage(dataSource.error));

  // A filter naming a nib that does not exist is the user's own half-typed or
  // stale id, not a fault. The server refuses such a query rather than answering
  // it with an empty list — it cannot tell "nothing is under that nib" from "no
  // such nib" otherwise — and tags the refusal NOT_FOUND so this side can tell
  // them apart in turn.
  //
  // It reaches here on every keystroke of an id being typed, so presenting it as
  // a red failure would flash an error through the list for ids that are merely
  // incomplete. Routed to a calm inline state instead, alongside the other empty
  // results, with the escape hatch offered when tree filters are what is set.
  //
  // This keys on the code alone, which is safe only while an UNRESOLVABLE-ID
  // filter refusal is the sole read-path source of NOT_FOUND. Not every filter
  // refusal qualifies: an id-valued field given the EMPTY STRING is refused too
  // and deliberately carries no code, so it lands in the generic error branch
  // below and stays visible as the client bug it is. That constraint lives with
  // the server that mints the code — see etagErrorPresenter in cmd/serve.go —
  // and a read resolver that started carrying it would be muted here.
  let notFoundMessage = $derived(
    dataSource.error && graphqlErrorCode(dataSource.error) === "NOT_FOUND"
      ? graphqlErrorMessage(dataSource.error)
      : ""
  );

  // An id-valued filter field ANDed with the `no:` token for the same
  // relationship is a query no store state can answer, and the server refuses it
  // rather than replying with an empty list. The box offers the two halves as
  // independent tokens, and "Children of this" on the context menu ANDs a
  // `parent:` onto whatever is already set — so a user browsing root nibs reaches
  // the pair without typing it.
  //
  // Named from the FILTER rather than from the server's message, because the two
  // speak different languages: the refusal says `hasParent: false` where the box
  // says `no:parent`, and the user can only edit what the box spells. When the
  // filter in hand holds no pair to name — it moved on since the refused query —
  // this stays empty and the generic error branch shows the server's own sentence
  // instead of an explanation with nothing in it.
  let contradictionPairs = $derived(
    dataSource.error && graphqlErrorCode(dataSource.error) === "FILTER_CONTRADICTION"
      ? contradictionTokens(resolvedFilter)
      : []
  );

  let allNibs = $derived(dataSource.allNibs);

  // Every view applies the client-side column sort when one is active: Flat gets
  // a flat sorted list; the nested views get sibling-sort (buildTableData nests
  // the pre-sorted array, and the tree builders preserve sibling input order
  // WITHIN each subtree). The active sort is ALSO threaded into buildTableData so
  // the epics/features lenses re-sort their promoted headers and "No X" bucket
  // items GLOBALLY — the pre-sort alone leaves them grouped by their hidden
  // higher-tier ancestor. Sort off keeps the server's manual `order` sequence.
  // Only the ROW ORDER changes — other allNibs consumers stay on the raw list.
  let orderedNibs = $derived(activeSort ? applySort(allNibs, activeSort) : allNibs);

  // The client-side filter is the original filter (not the server-stripped version).
  // buildTableData uses hasClientFilters/matchesFilter from filter.ts directly.
  let tableData = $derived(buildTableData(orderedNibs, resolvedFilter, resolvedViewLevel, treeView.collapsedIds, activeSort));
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
  // (drag stays enabled under hide-filters, so this is reachable). The
  // "matching set" is strictly `matchesFilter` (dimmed ancestors shown only for
  // tree context are excluded); with no client filters active matchesFilter() is
  // true for everything, so nothing is pruned — collapsing a parent never
  // deselects its (still-in-dataset) children.
  $effect(() => {
    // Don't prune while the first query is still in flight: allNibs is
    // transiently [] before the result lands, and a cold deep-link populates the
    // selection (via syncFromUrl) before data arrives — pruning against an empty
    // dataset would wrongly drop it. Reading dataSource.fetching also re-subscribes
    // so pruning runs once data settles (mirrors the ensure-visible loading guard).
    if (dataSource.fetching) return;
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
    //     the expand/scroll runs once data arrives. Reading
    //     dataSource.fetching also subscribes the effect to re-run on settle.
    //   - Query settled and the nib is genuinely absent (archived/bad URL):
    //     clear and bail — there is nothing to scroll to.
    if (!nibMap.has(nibId)) {
      if (!dataSource.fetching) {
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
        // the container before the async scroll event can't lose it.
        // claim() self-locates the live container via getScrollContainer().
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

  // --- Subtree expand/collapse (row context menu) ---
  // Descendants are resolved against the DISPLAYED view tree (buildViewTree), not
  // raw parentId, so the grouping lens (headers, hidden containers, "No X"
  // buckets) is honored. TreeViewState owns the collapse set; these compute the
  // next set and hand it to setCollapsed.
  //
  // Both calls intentionally omit the active-sort comparator that buildTableData
  // threads in: collectDescendantIds returns an order-INDEPENDENT Set, so
  // re-sorting the top-level headers / bucket items can't change which ids are a
  // node's descendants. Building the tree unsorted here is cheaper and equivalent.
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

  // --- Column reorder (drag a header to a new position) ---
  // Separate from the row drag (useTreeDrag): a flat column list has no
  // parent/reparent/zone/nesting concerns, so this owns its own small state
  // (draggedKey/target/side) rather than reusing the tree DragState. It reuses the
  // threshold PATTERN only. Writes the full order for the current view.
  const columnDrag = useColumnDrag({
    getOrder: () => resolvedColumnOrder,
    onReorder: (next: ColumnKey[]) => emitColumnOrder(prefs, oncolumnorderchange, next),
  });

  // --- Scroll container ---
  let scrollContainerEl: HTMLDivElement | undefined = $state(undefined);

  // --- Scroll-position restore (composable) ---
  // Saves/restores the scroll offset across App's {#key position} remount (the
  // detail-panel dock toggle recreates this container at scrollTop=0). The saved
  // value lives in TreeViewState outside the keyed block, the same way
  // collapsedIds survives the remount.
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
    // for self-feeding side-effects.
    untrack(() => scrollRestore.restore());
  });

  // --- Drag-and-drop (composable) ---
  const treeDrag = useTreeDrag({
    selection,
    drag,
    getRows: () => rows,
    getScrollContainer: () => scrollContainerEl ?? null,
    getDragBlock: () => dragBlock,
    ondrop: (targetNibId, zone, targetParentId) => ondrop?.(targetNibId, zone, targetParentId),
    onblockeddrag: (block) => {
      toast.info(block.message, {
        id: DRAG_BLOCK_TOAST_ID,
        action: { label: block.actionLabel, onClick: () => liftDragBlock(block) },
      });
    },
  });

  // Lift the gate the toast named. Each branch writes through the same path the
  // corresponding UI control uses, so clearing from the toast and clearing from
  // the header/toolbar/filter box are the same operation.
  function liftDragBlock(block: DragBlock) {
    switch (block.reason) {
      case "sort":
        emitTableSort(prefs, ontablesortchange, null);
        break;
      case "search": {
        // Drop only the free-text term; the token filters the user built up are
        // not what blocks drag and must survive.
        const { search: _search, ...rest } = resolvedFilter;
        emitFilter(prefs, onfilterchange, rest);
        break;
      }
      case "flat":
        emitViewLevel(prefs, onviewlevelchange, "none");
        break;
    }
  }

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
    getOpenDetailOn: () => resolvedOpenDetailOn,
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
  // ("no longer exists") heal path. Instead, opening a bucket
  // toggles/collapses its group — the same effect as its caret — mirroring the
  // drag handlers, which skip buckets via the same isBucketId test.
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

    if (action === "add-child") {
      const nibType = actionResult!.el.dataset.childType ?? "";
      // Anchor the type picker to the clicked [+] button (its viewport rect).
      onaddchild?.(nibId, nibType, actionResult!.el.getBoundingClientRect());
      return; // Don't fire row click for add-child
    }

    // A synthetic grouping bucket is not a nib, so every click ON a bucket row —
    // plain or modified, title or row body — toggles its group, like its caret,
    // rather than routing to selection. This handles the CLICKED id only. The
    // other bucket-selection paths are guarded at the source, in SelectionState:
    // rangeSelect filters bucket ids out of a range that SPANS a bucket, and
    // select/toggleSelect reject a bucket id outright — so an interleaved or
    // keyboard-focused bucket never reaches selectedIds even where this
    // click-level guard does not see it.
    if (isBucketId(nibId)) {
      toggleNode(nibId);
      return;
    }

    // Default: row click and title click share one modifier path, so the same
    // gesture means the same thing on the row body and on the title. The
    // toggle and add-child controls return above without reading modifiers —
    // they are their own affordances, not part of the row's gesture surface.
    // Shift/Ctrl-Cmd are bulk-selection gestures — intentionally NOT routed
    // through nav, so they record no Back/Forward history entry. In "single"
    // mode a collapse-to-exactly-one still opens the single-nib panel (and
    // collapse-to-zero closes it) without a history push, so URL/history can lag
    // selection after these gestures; that's accepted because multi-select is a
    // bulk gesture, not detail-panel navigation.
    //
    // In "double" mode the panel is decoupled from the selection, so a bulk
    // gesture must not touch it at all: the "detach" policy keeps
    // `selectedNibId` where it is (a ctrl+click is a SINGLE click and must not
    // open the panel; a sweep across unrelated rows must not tear down the nib
    // the user is reading), and there is correspondingly nothing to sync.
    // Only a plain click is treated as navigation.
    const panelPolicy: PanelPolicy = resolvedOpenDetailOn === "double" ? "detach" : "follow";
    if (e.shiftKey) {
      selection.rangeSelect(nibId, visibleRowIds, panelPolicy);
      // Multi-select desync: let the view follow the (possibly collapsed-to-one)
      // selection without a dirty-prompt — the documented guard-bypass path.
      if (panelPolicy === "follow") view.syncTo(selection.selectedNibId);
    } else if (e.ctrlKey || e.metaKey) {
      selection.toggleSelect(nibId, panelPolicy);
      if (panelPolicy === "follow") view.syncTo(selection.selectedNibId);
    } else if (resolvedOpenDetailOn === "double") {
      // Select-without-open: the plain single click focuses and selects the row
      // but leaves `selectedNibId` (and therefore the panel) alone, so whatever
      // the user is reading stays on screen. The open moves to
      // handleDelegatedDblClick below. No view.syncTo here — syncing would
      // retarget the panel, which is exactly what this mode exists to prevent.
      selection.selectOnly(nibId);
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

    // Bucket rows are synthetic containers with nothing to reorder. A row blocked
    // by a gate still goes through, so attempting a drag on it can explain why
    // nothing moves — useTreeDrag only reports once the gesture passes the drag
    // threshold, so a plain click stays silent.
    if (isBucketId(nibId)) return;

    treeDrag.onRowPointerDown(nibId, e);
  }

  // --- Header sorting ---
  // Clicking any sortable header cycles that field asc → desc → off. Active in
  // every view (Flat sorts the whole list; the nested views sort siblings).
  function handleTableSortClick(field: SortField) {
    emitTableSort(prefs, ontablesortchange, nextTableSort(resolvedTableSort, field));
  }

  // Escape hatch out of an over-constrained tree query: drop every hierarchy field
  // and keep the rest of the filter, so the metadata facets and free text the user
  // built up survive.
  function clearHierarchy() {
    emitFilter(prefs, onfilterchange, clearHierarchyFilters(resolvedFilter));
  }
</script>

<div data-testid="tree-table" class="h-full">
{#if dataSource.fetching && allNibs.length === 0}
  <!-- Only the INITIAL load (nothing to show yet) swaps in the loading state.
       A background refetch (fetching=true while allNibs is already populated,
       e.g. the NIB_CHANGED_SUBSCRIPTION-driven live refetch) keeps the table
       mounted so in-progress column drags/resizes, inline editors, and scroll
       position survive; rows update in place via the useTableData live path. -->
  <div class="flex items-center justify-center py-12 text-body text-muted-foreground">
    <span>Loading...</span>
  </div>
{:else if notFoundMessage}
  <!-- Ordered before the generic error branch: a refused filter id is an empty
       result the user can act on, not a failure, so it must not be styled or
       worded as one. -->
  <div data-testid="empty-unknown-id" class="flex flex-col items-center gap-3 py-12 text-body text-muted-foreground">
    <span class="text-foreground">No nibs match this filter</span>
    <span class="max-w-md text-center">{notFoundMessage}</span>
    {#if activeHierarchyTokens.length > 0}
      <Button variant="outline" size="sm" onclick={clearHierarchy}>Clear hierarchy filters</Button>
    {/if}
  </div>
{:else if contradictionPairs.length > 0}
  <!-- Ordered before the generic error branch for the same reason the unknown-id
       branch is: the user wrote a query that cannot be answered, which is a thing
       to fix rather than a failure to report. The escape hatch is offered on the
       same condition as there — some hierarchy field is set — so it clears the
       parent pair outright and merely widens the query otherwise. It is not
       offered at all for a lone blocking-dimension pair, which it cannot reach. -->
  <div data-testid="empty-contradiction" class="flex flex-col items-center gap-3 py-12 text-body text-muted-foreground">
    <span class="text-foreground">No nibs match this filter</span>
    <span class="max-w-md text-center">
      {#each contradictionPairs as [idToken, noToken] (idToken)}
        <code class="whitespace-nowrap rounded bg-muted px-1 py-0.5 text-foreground">{idToken}</code>{" "}
        and{" "}
        <code class="whitespace-nowrap rounded bg-muted px-1 py-0.5 text-foreground">{noToken}</code>{" "}
        ask for opposite things — a nib cannot both have that relationship and have none.{" "}
      {/each}
      Dropping either half of a pair makes the query answerable.
    </span>
    {#if activeHierarchyTokens.length > 0}
      <Button variant="outline" size="sm" onclick={clearHierarchy}>Clear hierarchy filters</Button>
    {/if}
  </div>
{:else if dataSource.error}
  <div class="rounded-lg bg-destructive/10 px-4 py-3 text-body text-destructive">
    Error: {errorMessage}
  </div>
{:else if rows.length === 0 && activeHierarchyTokens.length > 1}
  <!-- Several tree constraints at once: the generic message leaves the user staring
       at a dead end they cannot see the shape of. Name the relationships and offer
       the way out.
       The wording asserts only what this branch has established — that the filter
       ANDs these relationships together and matched nothing. It does NOT claim they
       are the cause: `parent:x ancestor:x` is redundant yet perfectly matchable, and
       a client-side facet (status, tags, …) or free text can be what actually
       emptied the result. So the escape hatch is offered as one way to widen the
       query rather than as the fix. -->
  <div data-testid="empty-hierarchy" class="flex flex-col items-center gap-3 py-12 text-body text-muted-foreground">
    <span class="text-foreground">No nibs match this filter</span>
    <span class="max-w-md text-center">
      It combines {activeHierarchyTokens.length} hierarchy relationships —
      {#each activeHierarchyTokens as token, i (token)}{#if i > 0}, {/if}<code class="whitespace-nowrap rounded bg-muted px-1 py-0.5 text-foreground">{token}</code>{/each}{" "}
      — and a nib has to satisfy every one of them. Clearing them is one way to
      widen the result.
    </span>
    <Button variant="outline" size="sm" onclick={clearHierarchy}>Clear hierarchy filters</Button>
  </div>
{:else if rows.length === 0}
  <div class="flex items-center justify-center py-12 text-body text-muted-foreground">
    <span>No nibs found</span>
  </div>
{:else}
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div bind:this={scrollContainerEl} class="overflow-auto h-full scroll-container" role="grid" tabindex="0" onkeydown={keyboardNav.handleKeydown} onscroll={scrollRestore.onScroll} onclick={handleDelegatedClick} ondblclick={handleDelegatedDblClick} oncontextmenu={handleDelegatedContextMenu} onpointerdown={handleDelegatedPointerDown} style="--row-pad-y: calc({rowDensity === 'comfortable' ? '0.625rem' : '0.25rem'} * var(--font-scale))">
  <table bind:this={tableEl} class="border-collapse" style="table-layout: fixed; width: {tableWidth}px;">
    <TableHeader
      columns={orderedVisibleColumns}
      columnWidths={resolvedColumnWidths}
      {activeSort}
      {showColumn}
      {columnResize}
      {columnDrag}
      onSort={handleTableSortClick}
      onExpandAll={expandAll}
      onCollapseAll={collapseAll}
    />
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
          columnOrder={resolvedColumnOrder}
          draggable={!isBucketId(row.nib.id) && dragAllowed}
          highlighted={dataSource.changed.isHighlighted(row.nib.id)}
          fading={dataSource.changed.isFading(row.nib.id)}
          {blockedEmphasis}
          openDetailOn={resolvedOpenDetailOn}
        />
      {/each}
    </tbody>
  </table>
  </div>
{/if}
</div>
