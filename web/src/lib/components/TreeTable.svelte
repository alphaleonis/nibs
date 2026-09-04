<script lang="ts">
  import { getContextClient } from "@urql/svelte";
  import { DEFAULT_BLOCKED_EMPHASIS, DEFAULT_OPEN_DETAIL_ON, TREE_VIEW_LEVEL } from "../types";
  import type { NibFilter, ViewLevel, RowDensity, BlockedEmphasis, OpenDetailGesture, RowSubtreeActions, TreeTableNib, TableSort, SortField } from "../types";
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
  import TreeTableRow from "./TreeTableRow.svelte";
  import TableHeader from "./TableHeader.svelte";
  import type { DropPlan } from "../ordering/dropPlan";
  import { regionBandAt, type BandAxis } from "../ordering/regionBand";
  import type { PanelPolicy } from "../selection.svelte";
  import { useSelection, useDrag, useActiveView, useTreeView, useConnection, useViewSpine } from "../contexts";
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
  // A getter, never the spine itself: its identity changes once, when the areas
  // vocabulary arrives, and the `$derived`s below have to see that. Capturing the
  // value here would pin them to the pre-load spine.
  const viewSpine = useViewSpine();

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
  let dragBlock = $derived(viewSpine().dragBlockFor(resolvedFilter, resolvedViewLevel, activeSort));
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
  //
  // The spine's vocabulary decides one more thing: whether an `area` value may be
  // sent at all (see withSendableArea in filter.ts). Read INSIDE the derived, so
  // an area withheld before the config answered is re-applied when it lands.
  let prepared = $derived(prepareFilter(resolvedFilter, viewSpine().areas));

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
  let tableData = $derived(viewSpine().buildTableData(orderedNibs, resolvedFilter, resolvedViewLevel, treeView.collapsedIds, activeSort));
  let rows = $derived(tableData.rows);
  // Which ids the CURRENT lens has a row for — collapse- and filter-independent
  // (see TableData.viewMemberIds). The view-transition applier reconciles against
  // this; nothing else reads it.
  let viewMemberIds = $derived(tableData.viewMemberIds);
  // What this view draws inside what. Collapse-independent, so it answers for a
  // nib whose section is shut — which is exactly reveal's subject.
  let containment = $derived(tableData.containment);
  let parentIds = $derived(tableData.parentIds);
  let visibleRowIds = $derived(rows.map(r => r.nib.id));

  // Where one ordering region's run of rows ends and the next begins, per row.
  //
  // Gated on ADJACENCY, not on whether a reorder is permitted, because that is
  // what the band claims: the list changes between these two rows. A flat view
  // intermixes real parents, and a search or a client sort reorders rows away
  // from the `order` key entirely — in all three, two neighbors say nothing
  // about where a region's run starts or stops, so a rule drawn between them
  // would be decoration at best and a lie at worst. Those three are also every
  // gate `dragBlockFor` shuts today, so the two predicates agree; they are asked
  // separately so a gate added for another reason cannot delete the bands.
  let regionBands: (BandAxis | null)[] = $derived(
    viewSpine().adjacencyReflectsOrdering(resolvedFilter, resolvedViewLevel, activeSort)
      ? rows.map((row, i) => regionBandAt(row, i === 0 ? null : rows[i - 1]))
      : [],
  );

  // Structural equality for two string sets (size + membership).
  function sameSet(a: ReadonlySet<string>, b: ReadonlySet<string>): boolean {
    if (a.size !== b.size) return false;
    for (const x of a) {
      if (!b.has(x)) return false;
    }
    return true;
  }

  // --- Apply a view transition ---
  // A grouping lens is lossless in WORK ITEMS but not in ROWS: it hides a
  // container ranked above its tier while descending into it, so a milestone
  // selected in the Tree view has no row under the Epics lens. Left alone it
  // stays selected, focused and a legal bulk-action target while off screen.
  //
  // Declared ahead of the two effects that consume its writes — ensure-visible
  // (which claims the scroll container for the anchor) and the scroll-restore
  // re-attempt — so they see reconciled state on their first pass instead of
  // acting on the outgoing view's and being re-run. Correctness does NOT rest on
  // that: Svelte re-runs dependents until effects settle, and moving this block
  // after both leaves every assertion in the suite passing. It is declared here
  // to keep the intermediate writes down, not to make the outcome right.
  $effect(() => {
    const transition = treeView.pendingTransition;
    if (!transition) return;
    // Nothing to reconcile against while the result is in flight AND the dataset
    // is empty — a cold load, or a filter change that re-keys the query — since
    // viewMemberIds is then empty and every id would look departed. The switch is
    // still owed a reconcile, so the slot is left UNCONSUMED: reading
    // dataSource.fetching re-subscribes this effect, which runs again with real
    // membership once the result settles. Deferring the scroll reset along with
    // it is safe: with no rows, restore() bails at hasContent() and takes no
    // ownership, so no offset is clamped in the meantime.
    //
    // The emptiness half is load-bearing, not belt-and-braces. queryStore merges
    // emissions, so a REFETCH keeps the previous data and only a re-key starts at
    // undefined — and the table refetches on every nibChanged event, so any
    // create, update or delete leaves this true while the incoming lens is
    // already rendered. Guarding on fetching alone would hold the transition
    // exactly when there is real membership to reconcile against, which is the
    // stale-row state this seam exists to close.
    if (dataSource.fetching && allNibs.length === 0) return;
    const memberIds = viewMemberIds;
    // The plan reads selection state that every sink below then writes. Read it
    // inside untrack so this effect's dependencies stay exactly "a switch was
    // recorded" plus the membership to reconcile against: subscribing to what it
    // writes would re-run it on every selection change, and only the
    // consumed-slot guard above — not the dependency set — would then stand
    // between that and the loop the pruner warns about.
    untrack(() => {
      const plan = planViewTransition(transition, {
        focusedNibId: selection.focusedNibId,
        selectedNibId: selection.selectedNibId,
        memberIds,
      });
      // The view the live scrollTop was actually measured in, so the offset is
      // filed under the geometry it describes. NOT transition.from: two switches
      // inside one flush collapse into a single pending slot whose `from` names a
      // view that was never rendered, and that view's memory would be overwritten
      // with an offset belonging to someone else. Read BEFORE clearTransition(),
      // which advances activeLevel to the destination.
      const from = treeView.activeLevel;
      // Consume the slot first: every path out of here is done with it, and a
      // re-run triggered by these writes then returns at the guard above.
      treeView.clearTransition();
      if (plan.retainIds) selection.retainOnly(plan.retainIds);
      if (plan.switchScroll) {
        treeView.switchScroll(from, transition.to);
        // Put the adopted offset on the ELEMENT here, not two effects later:
        // everything downstream measures the DOM — ensureVisible decides whether
        // the anchor row is already in view, and claim() persists whatever the
        // container is holding as the destination's memory. The scroll-restore
        // effect runs after both, so leaving the write to it lets the outgoing
        // view's offset be measured and then filed under the incoming view.
        // On the ordinary switch switchScroll has bumped the epoch, so this call
        // takes ownership and the later effect finds the element already owned
        // and no-ops. Two paths reach here without that, both benign and both
        // left to the later effect: a self-transition takes no bump and nothing
        // needs restoring, since the lens on screen never changed; and a
        // destination with no rows has no container to write to yet.
        scrollRestore.restore();
      }
      // Precedence: the destination's remembered offset is applied FIRST, and a
      // surviving anchor adjusts it only when the row is not already visible at
      // that offset (scrollIntoView({block:"nearest"}) is a no-op when it is).
      // An anchor behind a collapsed ancestor is reached the same way — the
      // ensure-visible effect expands and re-runs against the restored offset.
      // Deliberate: a row the user was working with is brought on screen, but it
      // does not throw away a position they left behind to do it.
      if (plan.anchorId) selection.ensureVisible(plan.anchorId);
    });
  });

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
    // A grouping bucket is focusable — keyboard nav lands on it so Enter toggles
    // the group — but names no nib, so the walk above can never yield it and the
    // dataset set alone drops a live focus on every emission of the list.
    // Admitting them as an ADDITION is what keeps this a no-op for real nibs:
    // the rendered set omits containers ranked above the lens's tier (a
    // milestone has no row under Epics), so retaining it INSTEAD would prune
    // nibs still in the dataset. Drawing from the current view rather than
    // testing `isSyntheticRowId` alone is what still prunes another lens's
    // bucket key. The source is the RENDERED rows rather than the lens's
    // membership because a bucket is no nib: it never matches the filter
    // itself, so it has a row only while some member does, and focus has to
    // follow the row. Collapse cannot cost it that row — buildGroupedTree
    // returns every section as a root, and collapsing hides a node's
    // descendants, not the node.
    for (const id of visibleRowIds) {
      if (isSyntheticRowId(id)) matchingIds.add(id);
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

    // Whether the RESPONSE holds it, which is a different question from whether
    // the current lens gives it a row.
    const inDataset = allNibs.some((nib) => nib.id === nibId);

    // The nib isn't in the dataset. Two distinct cases must NOT be conflated:
    //   - Query still loading (cold deep-link fires syncFromUrl before the
    //     GraphQL result lands, so allNibs is []): keep the pending request so
    //     the expand/scroll runs once data arrives. Reading
    //     dataSource.fetching also subscribes the effect to re-run on settle.
    //   - Query settled and the nib is genuinely absent (archived/bad URL):
    //     clear and bail — there is nothing to scroll to.
    if (!inDataset) {
      if (!dataSource.fetching) {
        selection.clearEnsureVisible();
      }
      return;
    }

    // The nib is in the dataset but not currently visible. Open every container
    // drawn around it so it becomes reachable.
    if (!visibleRowIds.includes(nibId)) {
      // The WHOLE chain, not the nearest container: sections nest, and opening
      // only the innermost one leaves the row inside a section still shut. One
      // chain covers both kinds of container — a real parent and one holding its
      // rows by arrangement — which is why no ancestor walk sits beside it.
      const next = new Set(treeView.collapsedIds);
      for (const id of containment.chainOf(nibId)) next.delete(id);
      // Expansion changes nothing yet the nib is still not visible: it is in the
      // dataset but has no row whatever the collapse set says. Either a client
      // filter excludes it, or this lens has no node for it at all — a grouping
      // lens is lossless in WORK ITEMS but not in ROWS, hiding a container
      // ranked above its tier while descending into it, and `chainOf` names no
      // container for an id it has no node for. Neither is reachable by
      // expanding, so clear and bail — otherwise reassigning `collapsedIds` to a
      // new Set every pass would loop forever (effect_update_depth_exceeded).
      if (sameSet(next, treeView.collapsedIds)) {
        selection.clearEnsureVisible();
        return;
      }
      // Containers were collapsed — expand them and let the effect re-run once
      // visibleRowIds updates (either the nib appears, or the next pass hits
      // the filtered-out guard above and clears).
      treeView.setCollapsed(next);
      return;
    }

    // Nib is visible — scroll into view and clear
    const scrollContainer = scrollContainerEl;
    if (scrollContainer) {
      const tr = scrollContainer.querySelector(`tr[data-nib-id="${CSS.escape(nibId)}"]`);
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
  // Descendants are resolved against the DISPLAYED view tree, not raw parentId,
  // so the grouping lens (headers, hidden containers, "No X" buckets) is
  // honored. TreeViewState owns the collapse set; these compute the next set and
  // hand it to setCollapsed.
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
    // Collapse the row itself plus every descendant that actually has children,
    // so re-expanding the row reveals exactly one level at a time. parentIds
    // already folds in the containers that hold their rows by display rather
    // than by parentage (tableData Stage 5a).
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
    getEpoch: () => treeView.scrollEpoch,
  });

  // Re-attempt the restore whenever the container binds, the rows change, or a
  // view transition retired the scroll ownership: after a {#key} remount the fresh
  // container starts at scrollTop=0, and restore() only applies the saved offset
  // once content is present (then it's a no-op). The epoch is named as a dep of its
  // own even though `rows` happens to cover a view switch today — `rows` is a
  // fresh array on every recompute, so reading `.length` subscribes to identity
  // rather than to the count — because what has to re-apply here is the offset the
  // switch swapped in, and tying that to an incidental property of a neighbouring
  // derived is how it would quietly stop happening.
  $effect(() => {
    void scrollContainerEl; void rows.length; void treeView.scrollEpoch;
    // untrack the restore call so the effect keeps only its three intended deps
    // (container binding + row count + epoch) and takes no incidental dependency on
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

  // Row-open guard for rows that name no nib. A "No X" bucket row is fabricated
  // by the view layer, so routing its synthetic id through view.open resolves an
  // empty detail query and fires the missing-nib ("no longer exists") heal path.
  // Opening one toggles/collapses its group instead — the same effect as its
  // caret — mirroring the drag handlers, which skip such rows via the same
  // isSyntheticRowId test. The question is identity, not whether the row heads a
  // section: a real nib heading one opens its own detail like any other row.
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
    if (isSyntheticRowId(nibId)) {
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
    // A drag owns the pointer until it is released or Escaped. Nothing here
    // touches drag state, so opening the menu over a live gesture leaves its
    // drop plan armed, and the release that dismisses the menu executes it —
    // a reorder the user was not making.
    //
    // Suppressing rather than canceling is the deliberate choice: right-click
    // is not an abort, so the gesture completes on release exactly as if the
    // menu had never been asked for, and Escape stays the only way to cancel.
    if (drag.isDragging) return;

    const nibId = getNibIdFromEvent(e);
    if (!nibId) return;

    const row = rows.find(r => r.nib.id === nibId);
    if (!row) return;

    e.preventDefault();
    onrowcontextmenu?.(nibId, e, row.nib, {
      hasChildren: row.hasChildren,
      expandChildren: () => expandSubtree(nibId),
      collapseChildren: () => collapseSubtree(nibId),
    }, etagFor);
  }

  // The loaded nibs live here, so the etag lookup the context menu's batch
  // mutations need has to be handed up with the event — the menu is rendered
  // from App and only ever sees the row that was right-clicked.
  function etagFor(id: string): string | undefined {
    return allNibs.find((n) => n.id === id)?.etag;
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
    if (isSyntheticRowId(nibId)) return;

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

  // The Areas view groups by the DECLARED vocabulary, so a project with none has
  // nothing to group by and every row would land in one "No area" section. Which
  // of the three no-vocabulary states this is decides what to say about it, and
  // `AreaVocabulary.status` is the only thing that can tell them apart: they are
  // all "no sections", but one is a wait, one is a healthy project, and one is a
  // failure.
  let areasStatus = $derived(viewSpine().areas.status);

  // The way back out, offered on the two states the user cannot resolve from
  // here. Same write path as the blocked-drag toast's remedy.
  function leaveAreasView() {
    switchViewLevel(prefs, onviewlevelchange, treeView, resolvedViewLevel, TREE_VIEW_LEVEL);
  }
</script>

<div data-testid="tree-table" class="h-full">
{#if resolvedViewLevel === "areas" && areasStatus !== "ready"}
  <!-- Ahead of every other branch, including the nib query's own loading and
       error states: this view's sections ARE the vocabulary, so with none there
       is nothing to group by however the nib query answers. -->
  {#if areasStatus === "loading"}
    <div data-testid="empty-areas-loading" class="flex items-center justify-center py-12 text-body text-muted-foreground">
      <span>Loading areas...</span>
    </div>
  {:else if areasStatus === "unavailable"}
    <!-- The only automatic re-ask is App's, and it is gated on the WEBSOCKET
         recovering. `CONFIG_QUERY` is an ordinary query, which urql routes
         through `fetchExchange` over HTTP, so a config failure that left the
         socket healthy — a 502, a resolver error — never reaches that gate. The
         copy therefore names the remedies the user actually has rather than
         promising a wait. -->
    <div data-testid="empty-areas-unavailable" class="flex flex-col items-center gap-3 py-12 text-body text-muted-foreground">
      <span class="text-foreground">Areas are unavailable</span>
      <span class="max-w-md text-center">
        The project configuration could not be read, so there is no area vocabulary to
        group by. Reload to try again, or switch to another view.
      </span>
      <Button variant="outline" size="sm" onclick={leaveAreasView}>Switch to Tree</Button>
    </div>
  {:else}
    <div data-testid="empty-areas-none" class="flex flex-col items-center gap-3 py-12 text-body text-muted-foreground">
      <span class="text-foreground">This project declares no areas</span>
      <span class="max-w-md text-center">
        Areas group work by the part of the project it belongs to. Declare an
        <code class="whitespace-nowrap rounded bg-muted px-1 py-0.5 text-foreground">areas:</code>
        block in the store's
        <code class="whitespace-nowrap rounded bg-muted px-1 py-0.5 text-foreground">config.yml</code>
        and each nib's area can name one.
      </span>
      <Button variant="outline" size="sm" onclick={leaveAreasView}>Switch to Tree</Button>
    </div>
  {/if}
{:else if dataSource.fetching && allNibs.length === 0}
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
      {#each rows as row, i (row.nib.id)}
        <TreeTableRow
          regionBand={regionBands[i] ?? null}
          nib={row.nib}
          depth={row.depth}
          hasChildren={row.hasChildren}
          dimmed={row.dimmed}
          collapsed={treeView.isCollapsed(row.nib.id)}
          parentNib={row.parentNib}
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
{/if}
</div>
