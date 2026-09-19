<script lang="ts">
  import { getContextClient } from "@urql/svelte";
  import { DEFAULT_BLOCKED_EMPHASIS, DEFAULT_OPEN_DETAIL_ON, DEFAULT_REGION_BAND_MODE, TREE_VIEW_LEVEL } from "../types";
  import type { NibFilter, ViewLevel, RowDensity, BlockedEmphasis, RegionBandMode, OpenDetailGesture, RowSubtreeActions, TreeTableNib, TableSort, SortField } from "../types";
  import type { ColumnKey } from "../columns";
  import type { Preferences } from "../preferences.svelte";
  import { isSyntheticRowId } from "../tree";
  import { applySort, nextTableSort } from "../tableSort";
  import { prepareFilter, matchesFilter } from "../filter";
  import { DRAG_BLOCK_TOAST_ID, FLAT_BLOCK_REMEDY_VIEW } from "../dragBlock";
  import type { DragBlock } from "../dragBlock";
  import { toast } from "svelte-sonner";
  import { resolveFilter, resolveViewLevel, resolveVisibleColumns, resolveColumnWidths, resolveColumnOrder, resolveTableSort, emitFilter, emitTableSort, emitColumnOrder, switchViewLevel } from "../resolvePrefs";
  import { planViewTransition } from "../viewTransition";
  import { hierarchyTokens, clearHierarchyFilters, contradictionTokens } from "../query";
  import { graphqlErrorCode, graphqlErrorMessage } from "../graphqlError";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as Popover from "$lib/components/ui/popover/index.js";
  import TreeTableRow from "./TreeTableRow.svelte";
  import TableHeader from "./TableHeader.svelte";
  import AreaDeclareForm from "./AreaDeclareForm.svelte";
  import AreaRowPanel from "./AreaRowPanel.svelte";
  import type { DropPlan } from "../ordering/dropPlan";
  import { regionBandAt, type BandAxis } from "../ordering/regionBand";
  import type { PanelPolicy } from "../selection.svelte";
  import { useSelection, useDrag, useActiveView, useTreeView, useConnection, useViewSpine, useConfigRetry } from "../contexts";
  import { useColumnResize } from "../composables/useColumnResize.svelte";
  import { useColumnDrag } from "../composables/useColumnDrag.svelte";
  import { useTreeDrag } from "../composables/useTreeDrag.svelte";
  import { useKeyboardNav } from "../composables/useKeyboardNav.svelte";
  import { useScrollRestore } from "../composables/useScrollRestore.svelte";
  import { useTableData } from "../composables/useTableData.svelte";
  import { untrack } from "svelte";

  // Coalesces a burst of filter-box keystrokes into one list re-query.
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
    onrowcontextmenu?: (
      nibId: string,
      event: MouseEvent,
      nib: TreeTableNib,
      subtree: RowSubtreeActions,
      etagOf: (id: string) => string | undefined,
    ) => void;
    onaddchild?: (nibId: string, nibType: string, anchor: DOMRect) => void;
    rowDensity?: RowDensity;
    blockedEmphasis?: BlockedEmphasis;
    regionBands?: RegionBandMode;
    /** Which row gesture opens the detail panel. Unused when `prefs` is supplied. */
    openDetailOn?: OpenDetailGesture;
    /** The drop a finished drag decided on, refusals included. */
    ondrop?: (plan: DropPlan) => void;
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
    regionBands = DEFAULT_REGION_BAND_MODE as RegionBandMode,
    openDetailOn = undefined as OpenDetailGesture | undefined,
    ondrop,
  }: Props = $props();

  const selection = useSelection();
  const drag = useDrag();
  // Explicit navigation opens rows through the view (dirty guard, history);
  // multi-select writes SelectionState and the view follows via syncTo.
  const view = useActiveView();
  // Provided by App outside the {#key position} block, so collapse state survives a remount.
  const treeView = useTreeView();
  // A getter: the spine's identity changes when the areas vocabulary arrives, and
  // the `$derived`s below must see it.
  const viewSpine = useViewSpine();

  // Resolve values: prefs takes precedence over individual props
  let resolvedFilter = $derived(resolveFilter(prefs, filter));
  // An empty result under two or more of these gets its own explanation below.
  let activeHierarchyTokens = $derived(hierarchyTokens(resolvedFilter));
  let resolvedViewLevel = $derived(resolveViewLevel(prefs, viewLevel));
  let resolvedVisibleColumns = $derived(resolveVisibleColumns(prefs, visibleColumns));
  let resolvedColumnWidths = $derived(resolveColumnWidths(prefs, columnWidths));
  let resolvedColumnOrder = $derived(resolveColumnOrder(prefs, columnOrder));
  let resolvedTableSort = $derived(resolveTableSort(prefs, tableSort));
  let resolvedOpenDetailOn = $derived(prefs ? prefs.openDetailOn : (openDetailOn ?? DEFAULT_OPEN_DETAIL_ON));
  // The sort applies only while its column is visible; the preference is kept, so
  // re-showing the column restores it. Drives row order, the header indicator
  // and the drag gate.
  let activeSort = $derived(
    resolvedTableSort && resolvedVisibleColumns.includes(resolvedTableSort.field)
      ? resolvedTableSort
      : null
  );

  // Drag is blocked in Flat, under a search, and under an active sort (the sort
  // never rewrites `order`). The same block supplies the blocked-drag toast.
  let dragBlock = $derived(viewSpine().dragBlockFor(resolvedFilter, resolvedViewLevel, activeSort));
  let dragAllowed = $derived(dragBlock === null);
  let showColumn = $derived((key: ColumnKey) => resolvedVisibleColumns.includes(key));

  // TreeTableRow filters the same order, so cells stay under their headers.
  let orderedVisibleColumns = $derived(resolvedColumnOrder.filter((key) => showColumn(key)));

  // Actions column (32px) plus visible widths; table-layout: fixed needs an explicit width.
  let tableWidth = $derived(
    32 + orderedVisibleColumns.reduce((sum, key) => sum + resolvedColumnWidths[key], 0)
  );

  // Server and client parts of the filter. The vocabulary decides whether an
  // `area` value is sent (withSendableArea in filter.ts); read inside the derived
  // so a withheld area is re-applied when the config lands.
  let prepared = $derived(prepareFilter(resolvedFilter, viewSpine().areas));

  const client = getContextClient();

  // The list query, live refetch on nib changes, and the highlight/fade tracker.
  const dataSource = useTableData({
    client,
    getServerFilter: () => prepared.serverFilter,
    refetchDebounceMs: LIST_REFETCH_DEBOUNCE_MS,
  });

  // The list misses change events while the socket is down; re-read on recovery.
  // Absent outside the app.
  const connection = useConnection();
  $effect(() => connection?.onRecovered(() => dataSource.refetch()));

  // Shown to the user, so without urql's "[GraphQL] " prefix.
  let errorMessage = $derived(graphqlErrorMessage(dataSource.error));

  // A filter naming a nonexistent nib is refused with NOT_FOUND. It arrives on
  // every keystroke of a half-typed id, so it renders as an empty state, not an
  // error. Keyed on the code alone: correct only while unresolvable filter ids
  // are the sole read-path source of NOT_FOUND (etagErrorPresenter in cmd/serve.go).
  let notFoundMessage = $derived(
    dataSource.error && graphqlErrorCode(dataSource.error) === "NOT_FOUND"
      ? graphqlErrorMessage(dataSource.error)
      : ""
  );

  // An id field ANDed with the `no:` token for the same relationship is refused
  // with FILTER_CONTRADICTION; "Children of this" reaches it from a `no:parent`
  // view. Pairs are named from the filter, since the server's message spells
  // `hasParent: false`. Empty when the filter has moved on, which leaves the
  // generic error branch.
  let contradictionPairs = $derived(
    dataSource.error && graphqlErrorCode(dataSource.error) === "FILTER_CONTRADICTION"
      ? contradictionTokens(resolvedFilter)
      : []
  );

  let allNibs = $derived(dataSource.allNibs);

  // Nested views sort siblings. The sort is also passed to buildTableData so
  // grouping lenses re-sort promoted headers and bucket items globally.
  let orderedNibs = $derived(activeSort ? applySort(allNibs, activeSort) : allNibs);

  let tableData = $derived(viewSpine().buildTableData(orderedNibs, resolvedFilter, resolvedViewLevel, treeView.collapsedIds, activeSort));
  let rows = $derived(tableData.rows);
  // Ids the current lens has a row for, ignoring collapse and filter.
  let viewMemberIds = $derived(tableData.viewMemberIds);
  // Collapse-independent, so it answers for a nib inside a collapsed section.
  let containment = $derived(tableData.containment);
  let parentIds = $derived(tableData.parentIds);
  let visibleRowIds = $derived(rows.map(r => r.nib.id));

  // Where one ordering region's run of rows ends, per row. Drawn only during a
  // drag in "on-drag" mode, and only where row adjacency reflects the `order`
  // key. Ask adjacencyReflectsOrdering, not the drag gate: a gate added for
  // another reason must not hide the bands.
  let bandsVisible = $derived(regionBands === "on-drag" && drag.isDragging);
  let regionBandAxes: (BandAxis | null)[] = $derived(
    bandsVisible &&
      viewSpine().adjacencyReflectsOrdering(resolvedFilter, resolvedViewLevel, activeSort)
      ? rows.map((row, i) => regionBandAt(row, i === 0 ? null : rows[i - 1]))
      : [],
  );

  function sameSet(a: ReadonlySet<string>, b: ReadonlySet<string>): boolean {
    if (a.size !== b.size) return false;
    for (const x of a) {
      if (!b.has(x)) return false;
    }
    return true;
  }

  // --- Apply a view transition ---
  // A grouping lens hides containers ranked above its tier, so a selected or
  // focused nib can lose its row. Declared before the ensure-visible and
  // scroll-restore effects so they see reconciled state on their first pass.
  $effect(() => {
    const transition = treeView.pendingTransition;
    if (!transition) return;
    // In flight over an empty dataset, every id would look departed: leave the
    // slot unconsumed, and reading `fetching` re-runs this on settle. Gate on the
    // emptiness too, since a refetch keeps the previous rows and the table
    // refetches on every change event.
    if (dataSource.fetching && allNibs.length === 0) return;
    const memberIds = viewMemberIds;
    // Untracked: the plan reads selection state that the sinks below write.
    untrack(() => {
      const plan = planViewTransition(transition, {
        focusedNibId: selection.focusedNibId,
        selectedNibId: selection.selectedNibId,
        memberIds,
      });
      // The view the live scrollTop was measured in. Not transition.from: two
      // switches in one flush collapse into one slot. Read before
      // clearTransition() advances activeLevel.
      const from = treeView.activeLevel;
      // Consume the slot first so a re-run triggered by these writes returns early.
      treeView.clearTransition();
      if (plan.retainIds) selection.retainOnly(plan.retainIds);
      if (plan.switchScroll) {
        treeView.switchScroll(from, transition.to);
        // Apply the adopted offset now: ensureVisible and claim() measure the DOM,
        // and the scroll-restore effect runs after them.
        scrollRestore.restore();
      }
      // The destination's offset applies first; the anchor scrolls only if it is
      // not already visible there.
      if (plan.anchorId) selection.ensureVisible(plan.anchorId);
    });
  });

  // --- Prune multi-select of filtered-out nibs ---
  // Otherwise a bulk mutation or multi-drag targets rows the user cannot see.
  // Matching is `matchesFilter` only, so dimmed context ancestors are pruned;
  // collapse never prunes.
  $effect(() => {
    // allNibs is [] while the first query is in flight, and a cold deep-link
    // selects before data arrives. Reading `fetching` re-runs this on settle.
    if (dataSource.fetching) return;
    const nibs = allNibs;
    const filter = resolvedFilter;
    const matchingIds = new Set<string>();
    for (const nib of nibs) {
      if (matchesFilter(nib, filter)) matchingIds.add(nib.id);
    }
    // A focusable bucket names no nib, so add the rendered buckets on top of the
    // matches, or every list emission drops its focus. Rendered rows, not
    // `isSyntheticRowId` alone, so another lens's bucket is still pruned.
    for (const id of visibleRowIds) {
      if (isSyntheticRowId(id)) matchingIds.add(id);
    }
    // Untracked: retainOnly reads the selection this effect writes.
    untrack(() => selection.retainOnly(matchingIds));
  });

  // --- Ensure-visible: expand collapsed ancestors and scroll into view ---
  $effect(() => {
    const nibId = selection.pendingEnsureVisibleId;
    if (!nibId) return;

    // In the response, which is not the same as having a row in this lens.
    const inDataset = allNibs.some((nib) => nib.id === nibId);

    // Still loading: keep the request (reading `fetching` re-runs this on
    // settle). Settled and absent: drop it.
    if (!inDataset) {
      if (!dataSource.fetching) {
        selection.clearEnsureVisible();
      }
      return;
    }

    // Hidden: open every container around it, not just the innermost.
    if (!visibleRowIds.includes(nibId)) {
      const next = new Set(treeView.collapsedIds);
      for (const id of containment.chainOf(nibId)) next.delete(id);
      // Nothing to expand yet still hidden: a client filter excludes it, or this
      // lens has no row for it. Clear, or reassigning collapsedIds loops forever.
      if (sameSet(next, treeView.collapsedIds)) {
        selection.clearEnsureVisible();
        return;
      }
      // Re-runs once visibleRowIds updates.
      treeView.setCollapsed(next);
      return;
    }

    const scrollContainer = scrollContainerEl;
    if (scrollContainer) {
      const tr = scrollContainer.querySelector(`tr[data-nib-id="${CSS.escape(nibId)}"]`);
      if (tr) {
        tr.scrollIntoView({ block: "nearest" });
        // Claim synchronously so neither the restore effect nor a refetch that
        // unmounts the container before the scroll event loses this offset.
        scrollRestore.claim();
      }
    }
    selection.clearEnsureVisible();
  });

  // Emitted only when the joined tag list changes, to avoid re-render cascades.
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
  // Descendants come from the displayed tree, not parentId, so grouping lenses are honored.
  function expandSubtree(rootId: string) {
    const descendantIds = containment.descendantsOf(rootId);
    const next = new Set(treeView.collapsedIds);
    next.delete(rootId);
    for (const id of descendantIds) next.delete(id);
    if (!sameSet(next, treeView.collapsedIds)) treeView.setCollapsed(next);
  }

  function collapseSubtree(rootId: string) {
    const descendantIds = containment.descendantsOf(rootId);
    const next = new Set(treeView.collapsedIds);
    // The row plus every descendant with children, so re-expanding reveals one level.
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
  // Separate from the row drag: a flat column list needs none of DragState.
  const columnDrag = useColumnDrag({
    getOrder: () => resolvedColumnOrder,
    onReorder: (next: ColumnKey[]) => emitColumnOrder(prefs, oncolumnorderchange, next),
  });

  // --- Scroll container ---
  let scrollContainerEl: HTMLDivElement | undefined = $state(undefined);

  // --- Scroll-position restore (composable) ---
  // Keeps the scroll offset across App's {#key position} remount; the value lives in TreeViewState.
  const scrollRestore = useScrollRestore({
    getScrollContainer: () => scrollContainerEl ?? null,
    getSavedScrollTop: () => treeView.scrollTop,
    setSavedScrollTop: (n) => { treeView.scrollTop = n; },
    hasContent: () => rows.length > 0,
    getEpoch: () => treeView.scrollEpoch,
  });

  // Re-attempt when the container binds, the rows change, or a view transition
  // bumps the epoch. Keep the epoch as its own dependency; do not rely on `rows`
  // changing on a view switch.
  $effect(() => {
    void scrollContainerEl; void rows.length; void treeView.scrollEpoch;
    // Untracked so restore()'s read of treeView.scrollTop adds no dependency.
    untrack(() => scrollRestore.restore());
  });

  // --- Drag-and-drop (composable) ---
  const treeDrag = useTreeDrag({
    selection,
    drag,
    getRows: () => rows,
    getScrollContainer: () => scrollContainerEl ?? null,
    getContainment: () => containment,
    getDragBlock: () => dragBlock,
    ondrop: (plan) => ondrop?.(plan),
    onblockeddrag: (block) => {
      toast.info(block.message, {
        id: DRAG_BLOCK_TOAST_ID,
        action: { label: block.actionLabel, onClick: () => liftDragBlock(block) },
      });
    },
  });

  // Each branch writes through the same path as the corresponding UI control.
  function liftDragBlock(block: DragBlock) {
    switch (block.reason) {
      case "sort":
        emitTableSort(prefs, ontablesortchange, null);
        break;
      case "search": {
        // Only the free-text term blocks drag; keep the token filters.
        const { search: _search, ...rest } = resolvedFilter;
        emitFilter(prefs, onfilterchange, rest);
        break;
      }
      case "flat":
        switchViewLevel(prefs, onviewlevelchange, treeView, resolvedViewLevel, FLAT_BLOCK_REMEDY_VIEW);
        break;
    }
  }

  // --- Keyboard navigation (composable) ---
  const keyboardNav = useKeyboardNav({
    selection,
    getRows: () => rows,
    getVisibleRowIds: () => visibleRowIds,
    getCollapsedIds: () => treeView.collapsedIds,
    getContainment: () => containment,
    toggleNode,
    getScrollContainer: () => scrollContainerEl ?? null,
    onDragKeyDown: treeDrag.onDragKeyDown,
    navigateToNib: (id) => openOrToggleBucket(id),
    getOpenDetailOn: () => resolvedOpenDetailOn,
  });

  // --- Event delegation helpers ---
  function getNibIdFromEvent(e: Event): string | null {
    const target = e.target as HTMLElement;
    const tr = target.closest("tr[data-nib-id]") as HTMLElement | null;
    return tr?.dataset.nibId ?? null;
  }

  function getActionFromEvent(e: Event): { action: string; el: HTMLElement } | null {
    const target = e.target as HTMLElement;
    const actionEl = target.closest("[data-action]") as HTMLElement | null;
    if (!actionEl) return null;
    // Rows only, not the header.
    const tr = actionEl.closest("tr[data-nib-id]");
    if (!tr) return null;
    const action = actionEl.dataset.action;
    if (!action) return null;
    return { action, el: actionEl };
  }

  // A bucket row names no nib, so opening it toggles its group like its caret.
  // A real nib heading a section opens normally.
  function openOrToggleBucket(id: string) {
    if (isSyntheticRowId(id)) {
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
      // The type picker anchors to the clicked [+] button.
      onaddchild?.(nibId, nibType, actionResult!.el.getBoundingClientRect());
      return; // Don't fire row click for add-child
    }

    if (action === "area-actions") {
      // The area path comes off the section's KEY, which is the declared path.
      // The fabricated row id is an escaped form and is not a path.
      const row = rows.find((r) => r.nib.id === nibId);
      const section = row?.drawsSection;
      if (section) {
        // A second click on the same row's [⋯] closes the panel: that is the
        // gesture a user reaches for to get the menu back, and closing is also
        // what drops the sub-form and draft the panel was left in.
        if (areaPanel?.path === section.key) {
          areaPanel = null;
        } else {
          // The node's own segment comes from the section's LABEL rather than from
          // splitting the path, which is what a rename sets.
          areaPanel = {
            path: section.key,
            name: section.display.label,
            description: section.display.description,
            color: section.display.color,
            anchorEl: actionResult!.el,
          };
        }
      }
      return; // Don't toggle the section: this click was for its editor.
    }

    // Any click on a bucket row, modified or not, toggles its group. SelectionState
    // also refuses bucket ids on its own.
    if (isSyntheticRowId(nibId)) {
      toggleNode(nibId);
      return;
    }

    // Row body and title share one modifier path. Shift/Ctrl-Cmd are bulk gestures
    // and record no history, so URL/history can lag selection in "single" mode.
    // In "double" mode the "detach" policy leaves `selectedNibId` and the panel alone.
    const panelPolicy: PanelPolicy = resolvedOpenDetailOn === "double" ? "detach" : "follow";
    if (e.shiftKey) {
      selection.rangeSelect(nibId, visibleRowIds, panelPolicy);
      // Guard-bypassing sync, so the view follows a selection collapsed to one.
      if (panelPolicy === "follow") view.syncTo(selection.selectedNibId);
    } else if (e.ctrlKey || e.metaKey) {
      selection.toggleSelect(nibId, panelPolicy);
      if (panelPolicy === "follow") view.syncTo(selection.selectedNibId);
    } else if (resolvedOpenDetailOn === "double") {
      // Select without opening; double-click opens. No syncTo, which would
      // retarget the panel.
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
    // During a drag the menu would leave the drop plan armed, and the release
    // that dismisses it would execute the drop. Right-click does not cancel;
    // Escape does.
    if (drag.isDragging) return;

    const nibId = getNibIdFromEvent(e);
    if (!nibId) return;

    // A section row names no nib, so there is nothing for the menu to act on.
    // preventDefault, or the browser's own menu appears.
    if (isSyntheticRowId(nibId)) {
      e.preventDefault();
      return;
    }

    const row = rows.find(r => r.nib.id === nibId);
    if (!row) return;

    e.preventDefault();
    onrowcontextmenu?.(nibId, e, row.nib, {
      hasChildren: row.hasChildren,
      expandChildren: () => expandSubtree(nibId),
      collapseChildren: () => collapseSubtree(nibId),
    }, etagFor);
  }

  // Handed up with the event: the menu renders in App and sees only the clicked row.
  function etagFor(id: string): string | undefined {
    return allNibs.find((n) => n.id === id)?.etag;
  }

  function handleDelegatedPointerDown(e: PointerEvent) {
    // Only left-click initiates drag
    if (e.button !== 0) return;

    const nibId = getNibIdFromEvent(e);
    if (!nibId) return;

    // Bucket rows have nothing to reorder. Gate-blocked rows go through so a drag
    // attempt past the threshold can explain the block.
    if (isSyntheticRowId(nibId)) return;

    treeDrag.onRowPointerDown(nibId, e);
  }

  // --- Header sorting ---
  function handleTableSortClick(field: SortField) {
    emitTableSort(prefs, ontablesortchange, nextTableSort(resolvedTableSort, field));
  }

  function clearHierarchy() {
    emitFilter(prefs, onfilterchange, clearHierarchyFilters(resolvedFilter));
  }

  // Areas groups by the declared vocabulary; `status` tells loading, none
  // declared and unavailable apart.
  let areasStatus = $derived(viewSpine().areas.status);
  const retryConfig = useConfigRetry();

  function leaveAreasView() {
    switchViewLevel(prefs, onviewlevelchange, treeView, resolvedViewLevel, TREE_VIEW_LEVEL);
  }

  // The declare form reaches the mutation store, which throws outside a
  // provider; mounting it only once asked for keeps this table renderable
  // without one.
  let declaringArea = $state(false);

  // The area whose row panel is open, or null. Mounted only once opened, for the
  // same reason the declare form is. `anchorEl` is the [⋯] button itself rather
  // than its rect, so the panel stays on it while the table scrolls.
  let areaPanel:
    | { path: string; name: string; description: string; color: string; anchorEl: HTMLElement }
    | null = $state(null);

  // A push that retires or renames the open area destroys its section row and the
  // [⋯] the panel hangs off. bits-ui answers a vanished anchor by HIDING the
  // floating wrapper rather than closing, so the panel would stay mounted and
  // invisible over a draft the user can no longer reach. Keyed on the vocabulary
  // rather than on `anchorEl`: the push is what changes, and a node going
  // detached is not something an effect can wake on.
  $effect(() => {
    if (areaPanel !== null && viewSpine().areas.validity(areaPanel.path) !== "declared") {
      areaPanel = null;
    }
  });
</script>

<div data-testid="tree-table" class="h-full">
{#if resolvedViewLevel === "areas" && areasStatus !== "ready"}
  <!-- Ahead of the nib query's loading and error states: without a vocabulary
       this view has nothing to group by. -->
  {#if areasStatus === "loading"}
    <div data-testid="empty-areas-loading" class="flex items-center justify-center py-12 text-body text-muted-foreground">
      <span>Loading areas...</span>
    </div>
  {:else if areasStatus === "unavailable"}
    <!-- `useLiveConfig` re-asks on a backoff; Retry covers a backoff that gave up. -->
    <div data-testid="empty-areas-unavailable" class="flex flex-col items-center gap-3 py-12 text-body text-muted-foreground">
      <span class="text-foreground">Areas are unavailable</span>
      <span class="max-w-md text-center">
        The project configuration could not be read, so there is no area vocabulary to
        group by. Trying again automatically.
      </span>
      <div class="flex items-center gap-2">
        {#if retryConfig}
          <Button variant="outline" size="sm" onclick={retryConfig}>Retry now</Button>
        {/if}
        <Button variant="outline" size="sm" onclick={leaveAreasView}>Switch to Tree</Button>
      </div>
    </div>
  {:else}
    <div data-testid="empty-areas-none" class="flex flex-col items-center gap-3 py-12 text-body text-muted-foreground">
      <span class="text-foreground">This project declares no areas</span>
      <span class="max-w-md text-center">
        Areas group work by the part of the project it belongs to. Declare an
        <code class="whitespace-nowrap rounded bg-muted px-1 py-0.5 text-foreground">areas:</code>
        block in the store's
        <code class="whitespace-nowrap rounded bg-muted px-1 py-0.5 text-foreground">areas.yml</code>
        and each nib's area can name one. A running server picks the file up, so
        this view fills in without a reload.
      </span>
      <div class="flex items-center gap-2">
        {#if declaringArea}
          <AreaDeclareForm onclose={() => (declaringArea = false)} />
        {:else}
          <Button variant="outline" size="sm" onclick={() => (declaringArea = true)}>Declare an area</Button>
        {/if}
        <!-- Outside the branch: opening the form must not take away the escape
             the user arrived with, since this panel is the whole view. -->
        <Button variant="outline" size="sm" onclick={leaveAreasView}>Switch to Tree</Button>
      </div>
    </div>
  {/if}
{:else if dataSource.fetching && allNibs.length === 0}
  <!-- Initial load only: a background refetch keeps the table mounted so drags,
       resizes and scroll position survive. -->
  <div class="flex items-center justify-center py-12 text-body text-muted-foreground">
    <span>Loading...</span>
  </div>
{:else if notFoundMessage}
  <!-- Before the error branch: a refused filter id is not a failure. -->
  <div data-testid="empty-unknown-id" class="flex flex-col items-center gap-3 py-12 text-body text-muted-foreground">
    <span class="text-foreground">No nibs match this filter</span>
    <span class="max-w-md text-center">{notFoundMessage}</span>
    {#if activeHierarchyTokens.length > 0}
      <Button variant="outline" size="sm" onclick={clearHierarchy}>Clear hierarchy filters</Button>
    {/if}
  </div>
{:else if contradictionPairs.length > 0}
  <!-- Before the error branch: an unanswerable query is not a failure. Clearing
       hierarchy filters cannot resolve a blocked-by pair, so the button needs a
       hierarchy field set. -->
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
  <!-- Do not word these relationships as the cause: a client facet or free text
       may be what emptied the result. -->
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
      {#each rows as row, i (row.nib.id)}
        <TreeTableRow
          regionBand={regionBandAxes[i] ?? null}
          nib={row.nib}
          depth={row.depth}
          hasChildren={row.hasChildren}
          dimmed={row.dimmed}
          collapsed={treeView.isCollapsed(row.nib.id)}
          parentNib={row.parentNib}
          milestoneNib={row.milestoneNib}
          drawsSection={row.drawsSection}
          visibleColumns={resolvedVisibleColumns}
          columnOrder={resolvedColumnOrder}
          draggable={!isSyntheticRowId(row.nib.id) && dragAllowed}
          highlighted={dataSource.changed.isHighlighted(row.nib.id)}
          fading={dataSource.changed.isFading(row.nib.id)}
          {blockedEmphasis}
          openDetailOn={resolvedOpenDetailOn}
        />
      {/each}
    </tbody>
  </table>
  </div>
  {#if areaPanel}
    <!-- A Popover rather than a hand-placed div: Escape and an outside click
         dismiss it, and `customAnchor` tracks the button instead of freezing the
         viewport coordinates it had when clicked. -->
    <Popover.Root
      open={true}
      onOpenChange={(open) => {
        if (!open) areaPanel = null;
      }}
    >
      <Popover.Content
        customAnchor={areaPanel.anchorEl}
        align="start"
        class="w-auto min-w-56 p-1"
      >
        <!-- Keyed by path so opening the panel on another area remounts it: its
             open entry and each form's draft belong to the area they were opened on. -->
        {#key areaPanel.path}
          <AreaRowPanel
            path={areaPanel.path}
            name={areaPanel.name}
            description={areaPanel.description}
            color={areaPanel.color}
            onclose={() => (areaPanel = null)}
          />
        {/key}
      </Popover.Content>
    </Popover.Root>
  {/if}
{/if}
</div>
