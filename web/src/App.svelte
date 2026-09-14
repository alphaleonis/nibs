<script lang="ts">
  import { untrack } from "svelte";
  import { setContextClient, queryStore, subscriptionStore } from "@urql/svelte";
  import { createClient } from "./lib/graphql";
  import { useLiveConfig } from "./lib/composables/useLiveConfig.svelte";
  import {
    CONFIG_QUERY,
    CONFIG_CHANGED_SUBSCRIPTION,
    MILESTONES_QUERY,
    NIB_DETAIL_QUERY,
    NIB_CONFLICT_SNAPSHOT_QUERY,
  } from "./lib/queries";
  import { Preferences } from "./lib/preferences.svelte";
  import Toolbar from "./lib/components/Toolbar.svelte";
  import UpdateBanner from "./lib/components/UpdateBanner.svelte";

  import TreeTable from "./lib/components/TreeTable.svelte";
  import ColumnAdapters from "./lib/ColumnAdapters.svelte";
  import ActiveNibView from "./lib/components/ActiveNibView.svelte";
  import TypePickerPopover from "./lib/components/TypePickerPopover.svelte";
  import RowContextMenu from "./lib/components/RowContextMenu.svelte";
  import DragBadge from "./lib/components/DragBadge.svelte";
  import ConfirmDialog from "./lib/components/ConfirmDialog.svelte";
  import { SelectionState } from "./lib/selection.svelte";
  import { DragState } from "./lib/drag.svelte";
  import { DROP_REFUSAL_TOAST_ID, refusalAction, type DropPlan } from "./lib/ordering/dropPlan";
  import type { AnyCommand } from "./lib/mutations/types";
  import { TreeViewState } from "./lib/treeView.svelte";
  import { provideSelection, provideDrag, provideTreeView, provideConfirmDialog, provideActiveView, provideHistoryNav, provideConnection, provideViewSpine, provideMilestones, provideConfigRetry } from "./lib/contexts";
  import { createAreaVocabulary } from "./lib/areas";
  import { makeViewSpine, LOADING_SPINE, UNAVAILABLE_SPINE } from "./lib/viewSpine";
  import { useConnectionRecovery } from "./lib/composables/useConnectionRecovery.svelte";
  import type { ConnectionRecovery } from "./lib/connectionRecovery";
  import { createHistoryNav } from "./lib/composables/useHistoryNav.svelte";
  import { createQueryUrl } from "./lib/composables/useQueryUrl.svelte";
  import { createConfirmDialog } from "./lib/composables/useConfirmDialog.svelte";
  import { createActiveView } from "./lib/composables/useActiveView.svelte";
  import type { ActiveView, DetailView, DetailNib, ConfirmChoice } from "./lib/composables/useActiveView.svelte";
  import { useKeyboardShortcuts } from "./lib/composables/useKeyboardShortcuts.svelte";
  import { createDetailPaneLayout } from "./lib/composables/detailPaneLayout.svelte";
  import { orientationOf } from "./lib/composables/detailPaneLayout";
  import { createNibForm, editNibForm } from "./lib/nibForm.svelte";
  import type { CreateForm, EditForm, CreateDefaults, NibSnapshot } from "./lib/nibForm.svelte";
  import { toNibSnapshot } from "./lib/nibChange";
  import type { RawNibPayload } from "./lib/nibChange";
  import { createLiveNib } from "./lib/liveNib.svelte";
  import type { LiveNib } from "./lib/liveNib.svelte";
  import type { TreeTableNib, RowSubtreeActions } from "./lib/types";
  import type { RelIdKey } from "./lib/query";
  import * as Resizable from "./lib/components/ui/resizable";
  import type ResizablePane from "./lib/components/ui/resizable/resizable-pane.svelte";
  import { Toaster } from "./lib/components/ui/sonner";
  import { toast } from "svelte-sonner";
  import { initMutationStore } from "./lib/mutations";
  import { applyTheme } from "./lib/theme";
  import { applyFontScale } from "./lib/fontScale";

  // The client needs the recovery hooks at construction, and the recovery needs
  // the client's `reconnect`. Reads through `socket` happen after construction.
  const socket: { recovery: ConnectionRecovery | null } = { recovery: null };
  const live = createClient({
    onConnected: () => socket.recovery?.onConnected(),
    onClosed: () => socket.recovery?.onClosed(),
  });
  const client = live.client;
  setContextClient(client);
  const recovery = useConnectionRecovery({ reconnect: () => live.reconnect() });
  socket.recovery = recovery;
  provideConnection(recovery);
  const mutations = initMutationStore(client);

  const configResult = queryStore({ client, query: CONFIG_QUERY });

  // The server pushes the whole config when it reloads the store's areas.yml
  // (e.g. after `nibs area rename`), selecting the same fields as CONFIG_QUERY.
  // It fires only on a change, so `data` is usually undefined and the query stays
  // the base answer.
  const configChanged = subscriptionStore({ client, query: CONFIG_CHANGED_SUBSCRIPTION });

  // Push-over-query precedence, the last-good latch and the re-ask backoff.
  const config = useLiveConfig({
    queried: () => $configResult.data?.config,
    pushed: () => $configChanged.data?.configChanged,
    error: () => $configResult.error,
    fetching: () => $configResult.fetching,
    reask: () => configResult.reexecute({ requestPolicy: "network-only" }),
  });
  const liveConfig = $derived(config.config);
  provideConfigRetry(() => config.retry());

  let projectName = $derived(liveConfig?.projectName ?? "");

  // The areas vocabulary arrives at runtime and binds the view core. First paint
  // is not gated on it: view levels other than Areas need none of it, and the
  // pre-load spine's `validity()` answers "unknown" rather than "undeclared".
  //
  // `Config.areas` is `[Area!]!`, so a project declaring none sends `[]`; the
  // `?? null` sentinel means "no answer yet", never "declares none". A failed
  // query with no config held takes UNAVAILABLE_SPINE, so a consumer can offer a
  // retry instead of a loading state.
  let declaredAreas = $derived(liveConfig?.areas ?? null);
  let viewSpine = $derived.by(() => {
    if (declaredAreas !== null) return makeViewSpine(createAreaVocabulary(declaredAreas));
    return config.unavailable ? UNAVAILABLE_SPINE : LOADING_SPINE;
  });
  provideViewSpine(() => viewSpine);

  // Milestones for the detail panel's field and the context menu. A query of its
  // own, not the table's rows: those carry the user's filter, so `type:bug` would
  // empty the picker.
  //
  // urql's document cache re-executes it after a `Nib`-returning mutation. A
  // subscription event invalidates only its `additionalTypenames`, so another
  // client's change can lag until the reconnect re-ask below.
  const milestonesResult = queryStore({ client, query: MILESTONES_QUERY });
  let milestones = $derived($milestonesResult.data?.nibs ?? []);
  provideMilestones(() => milestones);

  $effect(() => {
    if (projectName) {
      document.title = `Nibs - ${projectName}`;
    }
  });

  const prefs = new Preferences();

  // Filter query ↔ `?q=`. A `?q=` in the initial URL wins over the
  // localStorage-restored filter, applied before first paint.
  const queryUrl = createQueryUrl();
  const initialUrlQuery = queryUrl.currentQuery();
  if (initialUrlQuery !== null) prefs.setQuery(initialUrlQuery);

  // Mirror the canonical query into `?q=` (debounced replaceState). Also runs on
  // mount, normalizing a hand-typed link. An empty query removes the param.
  $effect(() => {
    queryUrl.push(prefs.query);
    return () => queryUrl.cancel();
  });

  // index.html sets the initial data-theme before first paint; this keeps it in sync.
  $effect(() => {
    applyTheme(prefs.theme);
  });

  $effect(() => {
    applyFontScale(prefs.fontSize);
  });

  const selection = new SelectionState();
  const drag = new DragState();
  // Created outside the {#key position} block so collapse state survives the
  // PaneGroup remount. Seeded with the restored view level: TreeTable's transition
  // applier parks the outgoing scroll offset under `activeLevel`, so a wrong seed
  // files the first offset under the wrong view.
  const treeView = new TreeViewState(prefs.viewLevel);
  const confirmDialog = createConfirmDialog();

  // --- active-nib-view presenter (unified detail/editor) ------------------
  // `nav.isBlocked` and the factories below close over the presenter, which is
  // constructed later. A const holder, not a reassigned `let`, keeps the reactive
  // reads of `holder.view.state` warning-clean.
  const holder: { view: ActiveView | null } = { view: null };

  // While blocked (dirty buffer, open type picker), Back/Forward must not
  // navigate the panel behind it.
  const nav = createHistoryNav({
    selection,
    isBlocked: () => holder.view?.blocksHistoryNav ?? false,
  });

  // The one detail query: renders relations and seeds the edit form. Paused
  // unless the view is `viewing` or `gone`.
  const detailTargetId = $derived.by(() => {
    const s = holder.view?.state;
    return s && (s.kind === "viewing" || s.kind === "gone") ? s.nibId : null;
  });
  const detailStore = $derived(
    queryStore({
      client,
      query: NIB_DETAIL_QUERY,
      variables: { id: detailTargetId ?? "" },
      pause: !detailTargetId,
    }),
  );
  // The shown nib missed every change while the socket was down; re-read it.
  $effect(() => recovery.onRecovered(() => {
    if (!detailTargetId) return;
    // The seed is one-shot per buffer: invalidate it, or the refetched result is
    // discarded as already-seeded. A dirty buffer still wins.
    holder.view?.invalidateDetailSeed();
    detailStore.reexecute({ requestPolicy: "network-only" });
  }));

  // Everything read while the socket was down is stale. The config re-ask is safe
  // unconditionally because `useLiveConfig` holds the last good config, and goes
  // through `retry()` so the automatic backoff budget is restored too.
  $effect(() => recovery.onRecovered(() => {
    config.retry();
    milestonesResult.reexecute({ requestPolicy: "network-only" });
  }));

  // Do not cast: `DetailNib` derives from NIB_DETAIL_QUERY's generated type, so a
  // field dropped from the query breaks snapshotFromDetail at compile time.
  const detailNib = $derived($detailStore.data?.nib ?? null);
  const detailFetching = $derived($detailStore.fetching);
  const detailError = $derived($detailStore.error);

  /** Project a loaded detail nib onto the form's committed-snapshot shape. */
  function snapshotFromDetail(n: DetailNib): NibSnapshot {
    return {
      id: n.id,
      title: n.title,
      status: n.status,
      type: n.type,
      priority: n.priority ?? "",
      estimate: n.estimate ?? "",
      milestone: n.milestone ?? "",
      area: n.area ?? "",
      tags: n.tags ? [...n.tags] : [],
      body: n.body ?? "",
      etag: n.etag,
    };
  }

  // --- presenter dependency factories -------------------------------------
  // A reactive window onto the detail query, which is keyed on the view's target.
  const detail = (_nibId: string): DetailView => ({
    get nib() { return detailNib; },
    get fetching() { return detailFetching; },
  });

  // Seeds from the detail query if it holds this nib, else a placeholder the
  // presenter replaces when `detail.nib` lands. A create→edit hand-off passes the
  // created snapshot as `seed`, so the form does not flash blank.
  const editForm = (nibId: string, seed?: NibSnapshot): EditForm => {
    const initial: NibSnapshot =
      seed ??
      (detailNib && detailNib.id === nibId
        ? snapshotFromDetail(detailNib)
        : { id: nibId, title: "", status: "", type: "task", priority: "", estimate: "", milestone: "", area: "", tags: [], body: "", etag: "" });
    return editNibForm({ mutations }, initial);
  };

  const createForm = (defaults: CreateDefaults): CreateForm => createNibForm({ mutations }, defaults);

  // A network-only read of a nib's committed snapshot, for the presenter's
  // null-remote conflict fallback. Uses its own query so a `{ nib: null }`
  // response does not feed detailStore and drop the buffer.
  //
  // Resolves the snapshot, or `null` when the nib no longer exists; throws on a
  // transport/GraphQL error, which `.toPromise()` resolves rather than rejects.
  // The fallback relies on the throw to tell a failed load from a deletion.
  const fetchSnapshot = async (nibId: string): Promise<NibSnapshot | null> => {
    const result = await client
      .query(NIB_CONFLICT_SNAPSHOT_QUERY, { id: nibId }, { requestPolicy: "network-only" })
      .toPromise();
    if (result.error) {
      console.warn("fetchSnapshot query error:", result.error);
      throw result.error;
    }
    const nib = result.data?.nib as RawNibPayload | null | undefined;
    return nib ? toNibSnapshot(nib) : null;
  };

  const liveNib = (nibId: string): LiveNib =>
    createLiveNib({
      client,
      nibId: () => nibId,
      // The live edit-form etag, so a post-save echo is filtered out.
      selfEtag: () => {
        const f = holder.view?.form;
        return f && f.mode === "edit" ? f.etag : undefined;
      },
    });

  // The dirty-nav confirm. Resolves "save", "discard", or "cancel" on dismissal or
  // when a later confirm supersedes it. Each call owns its resolver through an
  // idempotent `settle`, so overlapping confirms cannot resolve each other.
  //
  // `canSave: false` means the nib was deleted (an archived buffer still saves).
  // Omitting saveLabel/saveAction renders the dialog Discard-only.
  function confirmDiscard({ canSave }: { canSave: boolean }): Promise<ConfirmChoice> {
    return new Promise((resolve) => {
      let settled = false;
      const settle = (choice: ConfirmChoice) => {
        if (settled) return;
        settled = true;
        resolve(choice);
      };
      confirmDialog.showConfirm({
        title: canSave ? "Unsaved changes" : "This nib was deleted",
        message: canSave
          ? "You have unsaved changes. Save them and continue, or discard them."
          : "This nib no longer exists, so your unsaved changes can't be saved. Discard them and continue?",
        label: "Discard",
        variant: "warning",
        ...(canSave
          ? {
              saveLabel: "Save",
              saveAction: () => {
                confirmDialog.close();
                settle("save");
              },
            }
          : {}),
        action: () => {
          confirmDialog.close();
          settle("discard");
        },
        // Dismissed or superseded: keep the edits and stay put.
        onDismiss: () => settle("cancel"),
      });
    });
  }

  const view = createActiveView({
    nav,
    detail,
    editForm,
    createForm,
    liveNib,
    fetchSnapshot,
    notifyError: (message: string) => toast.error(message),
    confirm: confirmDiscard,
  });
  holder.view = view;

  provideSelection(selection);
  provideDrag(drag);
  provideTreeView(treeView);
  provideHistoryNav(nav);
  provideConfirmDialog(confirmDialog);
  provideActiveView(view);

  // The expanded presentation uses the modal below instead.
  const dockOpen = $derived(view.isOpen && view.presentation === "docked");

  // Sync selection from the initial URL and let the view follow; popstate drives
  // it afterward. Untracked so it runs once: `syncTo` reads and writes view
  // state and would re-trigger the effect in a loop.
  $effect(() => {
    untrack(() => {
      nav.syncFromUrl();
      view.syncTo(selection.selectedNibId);
    });
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  });

  function onPopState(e: PopStateEvent) {
    // syncTo skips the dirty guard: history has already moved, so a confirm would
    // come too late.
    nav.handlePopState(e);
    view.syncTo(selection.selectedNibId);
    // The URL is the source of truth for the query on history navigation. An
    // absent `?q=` means an empty filter, since the writer removes the param for one.
    prefs.setQuery(queryUrl.currentQuery() ?? "");
  }

  // A viewed nib that resolves to nothing. `view.noteMissing` decides: a pristine
  // buffer closes, a dirty one is held in "gone". The live bridge can also reach
  // "gone" without this effect reporting.
  let reportedMissingFor: string | null = null;
  $effect(() => {
    const s = view.state;
    // Only `viewing` reports; the early return keeps a report from repeating.
    // Getting back to `viewing` takes an open, after which noteMissing decides
    // afresh, so clearing the latch here is safe.
    if (s.kind !== "viewing") {
      reportedMissingFor = null;
      return;
    }
    if (!detailFetching && detailError === undefined && detailNib === null && reportedMissingFor !== s.nibId) {
      reportedMissingFor = s.nibId;
      handleMissingNib(s.nibId);
    }
  });

  function handleMissingNib(id: string) {
    // Deferred so no state is mutated during the detail query's effect flush.
    queueMicrotask(() => {
      const outcome = view.noteMissing(id);
      if (outcome === "stale") {
        // The view moved off `id`: act on nothing, and release the latch if it is
        // still ours so a later report is not suppressed.
        if (reportedMissingFor === id) reportedMissingFor = null;
        return;
      }
      if (outcome === "kept") {
        // The buffer is on screen in "gone": keep the selection and ?nib=. The
        // view's own notice reports the deletion.
        return;
      }
      // "closed" — nothing for `id` is on screen any more: heal the URL and say why.
      selection.close();
      nav.replaceClosed();
      toast.error(`Nib ${id} no longer exists`);
    });
  }

  // Unique tags in the query results, reported by TreeTable.
  let availableTags: string[] = $state([]);

  function handleTagsChange(tags: string[]) {
    availableTags = tags;
  }

  // --- Drag-and-drop handlers ---

  /** Execute the drop the drag decided on, or say why it cannot happen. */
  async function handleDrop(plan: DropPlan) {
    // Copied before any await: `endDrag()` runs once this suspends and clears
    // draggedIds, and a refusal's action outlives the gesture. Never empty:
    // `ondrop` fires only during a drag.
    const ids = [...drag.draggedIds];

    if (!plan.ok) {
      // Releasing on the grabbed row is a cancel and stays silent; every other
      // refusal is shown.
      if (plan.refusal.reason !== "drop-on-self") {
        const remedy = refusalAction(plan.refusal);
        // Always pass `action`, even as undefined: sonner merges a repeat raise
        // into the live toast, so an omitted key keeps the previous button.
        toast.error(plan.refusal.message, {
          id: DROP_REFUSAL_TOAST_ID,
          action:
            remedy === null
              ? undefined
              : { label: remedy.label, onClick: () => void applyRemedy(remedy.command, ids) },
        });
      }
      return;
    }

    await mutations.execute(plan.command);

    // TreeTable expands collapsed ancestors and scrolls it into view.
    selection.ensureVisible(ids[0]);
  }

  /** Run the write a refusal offered. The rows move to a group the drag could
   *  not reach, so the accepted path's follow-up applies here too. */
  async function applyRemedy(command: AnyCommand, ids: string[]) {
    await mutations.execute(command);
    selection.ensureVisible(ids[0]);
  }

  // Context menu state
  let contextMenuOpen = $state(false);
  let contextMenuPosition = $state({ x: 0, y: 0 });
  let contextMenuNibId: string | null = $state(null);
  let contextMenuNib: TreeTableNib | null = $state(null);
  // From the table, which holds the loaded nibs; batch mutations need etags for ifMatch.
  let contextMenuEtagOf: ((id: string) => string | undefined) | undefined = $state(undefined);
  // Subtree expand/collapse closures for the right-clicked row, from TreeTable.
  let contextMenuSubtree: RowSubtreeActions | null = $state(null);

  function handleRowContextMenu(nibId: string, event: MouseEvent, nib: TreeTableNib, subtree: RowSubtreeActions, etagOf: (id: string) => string | undefined) {
    contextMenuEtagOf = etagOf;
    // The menu reads its target from the selection. "single" opens an unselected
    // row through the view; "double" selects it without opening the panel.
    if (!selection.isSelected(nibId)) {
      if (prefs.openDetailOn === "double") {
        selection.selectOnly(nibId);
      } else {
        view.open(nibId);
      }
    }
    contextMenuNibId = nibId;
    contextMenuNib = nib;
    contextMenuSubtree = subtree;
    contextMenuPosition = { x: event.clientX, y: event.clientY };
    contextMenuOpen = true;
  }

  // AND the relationship id onto the current filter; a same-kind key is replaced.
  function handleFilterRelated(field: RelIdKey, id: string) {
    prefs.filter = { ...prefs.filter, [field]: id };
  }

  // --- Global keyboard shortcuts ---
  useKeyboardShortcuts({
    selection,
    nav,
    view,
    confirmDialog,
    mutations,
    getContextMenuNibId: () => contextMenuNibId,
  });

  // --- Detail-pane sizing (the layout composable owns the math; App measures and wires PaneForge) ---
  let paneGroupEl: HTMLElement | null = $state(null);
  let detailPaneComponent: ReturnType<typeof ResizablePane> | undefined = $state(undefined);
  let containerWidth = $state(0);
  let containerHeight = $state(0);

  // "right" splits horizontally (size = width), "bottom" vertically (size = height).
  const position = $derived(prefs.detailPanelPosition);
  const direction = $derived(orientationOf(position).direction);
  const containerSize = $derived(direction === "vertical" ? containerHeight : containerWidth);

  const layout = createDetailPaneLayout({
    prefs,
    position: () => position,
    containerSize: () => containerSize,
  });

  // Both dimensions, since the measured axis depends on the dock orientation.
  $effect(() => {
    if (!paneGroupEl) return;
    // Read synchronously so sizes are valid before PaneForge's first onResize.
    containerWidth = paneGroupEl.offsetWidth;
    containerHeight = paneGroupEl.offsetHeight;
    const observer = new ResizeObserver(([entry]) => {
      containerWidth = entry.contentRect.width;
      containerHeight = entry.contentRect.height;
    });
    observer.observe(paneGroupEl);
    return () => observer.disconnect();
  });

  function handleResizeHandleDblClick() {
    detailPaneComponent?.resize(layout.reset());
  }

  $effect(() => {
    if (!detailPaneComponent) return;
    if (dockOpen) {
      if (detailPaneComponent.isCollapsed()) {
        detailPaneComponent.expand();
        detailPaneComponent.resize(layout.defaultPercent);
      }
    } else {
      if (!detailPaneComponent.isCollapsed()) {
        detailPaneComponent.collapse();
      }
    }
  });

  // A pane that mounts already open before the container is measured gets a
  // fallback size near its minimum, which the effect above does not correct.
  // Runs once only, so it never fights the user's resizes.
  let paneInitialSized = false;
  $effect(() => {
    if (paneInitialSized) return;
    if (!detailPaneComponent || !dockOpen || containerSize <= 0) return;
    detailPaneComponent.resize(layout.defaultPercent);
    paneInitialSized = true;
  });
</script>

<Toaster richColors />

<div class="h-screen flex flex-col">
  <UpdateBanner />
  <Toolbar
    {prefs}
    {treeView}
    {projectName}
    {availableTags}
    areas={viewSpine.areas}
    connectionStatus={recovery.status}
    oncreatenew={(type) => view.startCreate({ type })}
  />

  <!-- Provides the per-column header/cell adapters to the table region. -->
  <ColumnAdapters>
  <main class="flex-1 min-h-0 flex flex-col px-6 py-6">
    <!-- Remounts the PaneGroup when the dock toggles. State that must survive
         lives in contexts provided outside this block. -->
    {#key position}
    <Resizable.PaneGroup
      direction={direction}
      class="flex-1 min-h-0"
      bind:ref={paneGroupEl}
    >
      <Resizable.Pane class="min-w-0 min-h-0">
        <TreeTable
          {prefs}
          ontagschange={handleTagsChange}
          onrowcontextmenu={handleRowContextMenu}
          onaddchild={(parentId, parentType, anchor) => view.startCreateChild(parentId, parentType, anchor)}
          rowDensity={prefs.rowDensity}
          blockedEmphasis={prefs.blockedEmphasis}
          regionBands={prefs.regionBands}
          ondrop={handleDrop}
        />
      </Resizable.Pane>
      <Resizable.Handle
        data-testid="resize-handle"
        class={dockOpen ? "" : "hidden"}
        ondblclick={handleResizeHandleDblClick}
        onDraggingChange={layout.onDraggingChange}
      />
      <Resizable.Pane
        defaultSize={dockOpen ? layout.defaultPercent : 0}
        minSize={dockOpen ? layout.minPercent : 0}
        maxSize={dockOpen ? layout.maxPercent : 0}
        collapsible={true}
        collapsedSize={0}
        onResize={layout.onResize}
        onCollapse={() => {
          if (!dockOpen) return;
          // Close only if still docked-open when the frame fires; re-expand if
          // the dirty guard refuses.
          requestAnimationFrame(async () => {
            if (!dockOpen) return;
            await view.requestClose();
            if (dockOpen && detailPaneComponent?.isCollapsed()) {
              detailPaneComponent.expand();
              detailPaneComponent.resize(layout.defaultPercent);
            }
          });
        }}
        bind:this={detailPaneComponent}
        data-testid="detail-pane"
      >
        {#if dockOpen}
          <ActiveNibView suggestions={availableTags} blockedEmphasis={prefs.blockedEmphasis} {prefs} />
        {/if}
      </Resizable.Pane>
    </Resizable.PaneGroup>
    {/key}
  </main>
  </ColumnAdapters>
</div>

<!-- Expanded presentation. The buffer and query live in the presenter, so they
     survive the swap from docked. -->
{#if view.isOpen && view.presentation === "expanded"}
  <div class="anv-modal-backdrop" data-testid="active-nib-modal" role="presentation">
    <div class="anv-modal-shell">
      <ActiveNibView suggestions={availableTags} blockedEmphasis={prefs.blockedEmphasis} {prefs} />
    </div>
  </div>
{/if}

<!-- Add-child type picker, anchored over the whole app. -->
{#if view.typePicker}
  <TypePickerPopover
    parentType={view.typePicker.parentType}
    anchor={view.typePicker.anchor}
    onselect={(t) => view.chooseType(t)}
    oncancel={() => view.cancelType()}
  />
{/if}

<DragBadge />

<RowContextMenu
  bind:open={contextMenuOpen}
  position={contextMenuPosition}
  nib={contextMenuNib}
  selectedCount={selection.hasMultiSelect ? selection.selectedIds.size : 1}
  hasChildren={contextMenuSubtree?.hasChildren ?? false}
  onexpandchildren={() => contextMenuSubtree?.expandChildren()}
  oncollapsechildren={() => contextMenuSubtree?.collapseChildren()}
  onfilterrelated={handleFilterRelated}
  etagOf={contextMenuEtagOf}
/>

<!-- The dialog owns every route (confirm, Save, dismiss); do not wire them here. -->
<ConfirmDialog confirm={confirmDialog} />

<style>
  .anv-modal-backdrop {
    position: fixed;
    inset: 0;
    z-index: var(--z-modal);
    display: flex;
    align-items: stretch;
    justify-content: center;
    padding: 1rem;
    background: color-mix(in oklab, var(--background), transparent 15%);
  }

  @media (min-width: 640px) {
    .anv-modal-backdrop {
      padding: 2rem;
    }
  }

  .anv-modal-shell {
    width: 100%;
    max-width: 1100px;
    max-height: 100%;
    display: flex;
    background: var(--background);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    overflow: hidden;
    box-shadow: 0 10px 40px oklch(0 0 0 / 0.35);
  }

  .anv-modal-shell :global(.anv) {
    flex: 1;
  }
</style>
