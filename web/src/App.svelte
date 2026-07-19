<script lang="ts">
  import { untrack } from "svelte";
  import { setContextClient, queryStore } from "@urql/svelte";
  import { createClient } from "./lib/graphql";
  import { CONFIG_QUERY, NIB_DETAIL_QUERY, NIB_CONFLICT_SNAPSHOT_QUERY } from "./lib/queries";
  import { Preferences } from "./lib/preferences.svelte";
  import Toolbar from "./lib/components/Toolbar.svelte";
  import UpdateBanner from "./lib/components/UpdateBanner.svelte";

  import TreeTable from "./lib/components/TreeTable.svelte";
  import ActiveNibView from "./lib/components/ActiveNibView.svelte";
  import TypePickerPopover from "./lib/components/TypePickerPopover.svelte";
  import RowContextMenu from "./lib/components/RowContextMenu.svelte";
  import ConfirmDialog from "./lib/components/ConfirmDialog.svelte";
  import { SelectionState } from "./lib/selection.svelte";
  import { DragState } from "./lib/drag.svelte";
  import type { DropZone } from "./lib/drag.svelte";
  import { TreeViewState } from "./lib/treeView.svelte";
  import { provideSelection, provideDrag, provideTreeView, provideConfirmDialog, provideActiveView, provideHistoryNav } from "./lib/contexts";
  import { createHistoryNav } from "./lib/composables/useHistoryNav.svelte";
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
  import * as Resizable from "./lib/components/ui/resizable";
  import type ResizablePane from "./lib/components/ui/resizable/resizable-pane.svelte";
  import { Toaster } from "./lib/components/ui/sonner";
  import { toast } from "svelte-sonner";
  import { initMutationStore } from "./lib/mutations";
  import { applyTheme } from "./lib/theme";
  import { applyFontScale } from "./lib/fontScale";
  import {
    reparentBatch,
    reorderChain,
    reorderNib as reorderNibCmd,
    reparentAndReorder,
  } from "./lib/mutations/commands";

  const client = createClient();
  setContextClient(client);
  const mutations = initMutationStore(client);

  const configResult = queryStore({ client, query: CONFIG_QUERY });
  let projectName = $derived($configResult.data?.config?.projectName ?? "");

  $effect(() => {
    if (projectName) {
      document.title = `Nibs - ${projectName}`;
    }
  });

  const prefs = new Preferences();

  // Live-apply the selected palette: repaints the app whenever prefs.theme
  // changes (no reload). The FOUC guard in index.html sets the initial
  // data-theme before first paint; this keeps it in sync thereafter.
  $effect(() => {
    applyTheme(prefs.theme);
  });

  // Live-apply the global font-size preference: writes the S/M/L multiplier onto
  // --font-scale whenever prefs.fontSize changes, scaling the type scale only
  // (not layout/spacing). No pre-paint FOUC guard needed (a tiny reflow is fine).
  $effect(() => {
    applyFontScale(prefs.fontSize);
  });

  const selection = new SelectionState();
  const drag = new DragState();
  // Collapse state lives here — OUTSIDE the {#key position} block that remounts the
  // PaneGroup (and TreeTable) on a dock toggle — so it survives the remount, like
  // selection/drag.
  const treeView = new TreeViewState();
  const confirmDialog = createConfirmDialog();

  // --- active-nib-view presenter (unified detail/editor) ------------------
  // Forward reference: `nav.isBlocked` and the injected factories close over the
  // presenter, which is constructed below. Held in a const object (not a
  // reassigned `let`) so the reactive reads of `holder.view.state` stay
  // warning-clean; every read is lazy (after construction).
  const holder: { view: ActiveView | null } = { view: null };

  // isBlocked reads view.blocksHistoryNav (dirty buffer / open type picker).
  // While blocked, Back/Forward must not navigate the panel behind it.
  const nav = createHistoryNav({
    selection,
    isBlocked: () => holder.view?.blocksHistoryNav ?? false,
  });

  // The nib id the view currently targets for editing (viewing/gone) drives the
  // SINGLE detail query — used both to render relations AND to seed the edit
  // form. Paused when nothing is being viewed.
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
  const detailNib = $derived(($detailStore.data?.nib as DetailNib | undefined) ?? null);
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
      tags: n.tags ? [...n.tags] : [],
      body: n.body ?? "",
      etag: n.etag,
    };
  }

  // --- presenter dependency factories -------------------------------------
  // `detail` returns a thin reactive window onto the single App-level query
  // (keyed on the view's target, which equals `nibId` by the time this runs).
  const detail = (_nibId: string): DetailView => ({
    get nib() { return detailNib; },
    get fetching() { return detailFetching; },
  });

  // `editForm` seeds from the shared detail query. When the async query hasn't
  // resolved yet it starts from a placeholder; the presenter adopts the real
  // snapshot via `applyExternal` once `detail.nib` lands (no second fetch). A
  // create→edit hand-off passes the freshly-created snapshot as `seed`, so the
  // edit form renders the new nib immediately (no blank flash before the detail
  // query runs for the brand-new id).
  const editForm = (nibId: string, seed?: NibSnapshot): EditForm => {
    const initial: NibSnapshot =
      seed ??
      (detailNib && detailNib.id === nibId
        ? snapshotFromDetail(detailNib)
        : { id: nibId, title: "", status: "", type: "task", priority: "", estimate: "", tags: [], body: "", etag: "" });
    return editNibForm({ mutations }, initial);
  };

  const createForm = (defaults: CreateDefaults): CreateForm => createNibForm({ mutations }, defaults);

  // One-shot, network-authoritative fetch of a nib's current committed snapshot,
  // used by the presenter's null-remote conflict fallback.
  // `network-only` bypasses the cache so we read the server's CURRENT revision —
  // the whole point is to reconcile against what actually rejected the save. Uses
  // the DEDICATED NIB_CONFLICT_SNAPSHOT_QUERY (not NIB_DETAIL_QUERY) so its urql
  // result-source is independent of App's live detailStore — a `{ nib: null }`
  // (deleted-in-race) response must not feed detailStore and drop the buffer.
  //
  // Contract: resolves the snapshot, resolves `null` when the nib no longer
  // exists, and REJECTS (throws) on a transport/GraphQL error. urql's
  // `.toPromise()` does NOT reject on failure — it resolves an OperationResult
  // with `.error` set — so we surface it explicitly (mirrors dispatcher.ts's
  // `if (res.error)` and liveNib's warn). The presenter's fallback relies on this
  // to tell a transient load failure ("please retry") apart from a real deletion.
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
      // Read the presenter's live edit-form etag so a post-save echo is filtered.
      selfEtag: () => {
        const f = holder.view?.form;
        return f && f.mode === "edit" ? f.etag : undefined;
      },
    });

  // Promise wrapper over the shared confirm dialog for the dirty-nav guard.
  // Resolves the tri-state ConfirmChoice: "save" (the Save action),
  // "discard" (the confirm/primary action), or "cancel" on any dismissal
  // (Cancel / Escape / overlay) OR when a later confirm supersedes this one before
  // it is answered.
  //
  // Each invocation owns its OWN resolver via a latched `settle` closure — there
  // is no shared single slot to overwrite, so overlapping confirms can never leak
  // one another's promise (nibs-an5d). The three action handlers settle explicitly;
  // dismissal/supersession routes through `onDismiss`, which the dialog runs for
  // exactly this invocation. `settle` is idempotent, so a follow-on dismissal
  // after an explicit Save/Discard is inert.
  //
  // `canSave: false` — the buffer's nib was DELETED, so a Save would dispatch
  // against nothing and fail; the copy below may state that as fact. It does not
  // mean merely "gone": an archived buffer arrives with `canSave: true` and keeps
  // its Save, because its nib still exists in the archive and the write lands.
  // Omitting saveLabel/saveAction renders the dialog Discard-only (showConfirm
  // nulls both, and ConfirmDialog gates its Save button on `confirm.saveAction`),
  // the same shape Delete/Archive confirms use.
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
        // Dismissed (Cancel / Escape / overlay) or superseded before it was
        // answered: keep the edits and stay put. Owned by this invocation, so a
        // later Delete/Archive confirm's dismissal can never resolve it.
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

  // Whether the docked detail pane should be open: the view is open AND
  // presented docked (expanded routes to the full-screen modal instead).
  const dockOpen = $derived(view.isOpen && view.presentation === "docked");

  // Wire browser history: sync selection from the initial URL, then let the view
  // follow. Back/Forward drive selection via popstate; the view syncs to it.
  // Untracked so this runs ONCE on mount: `syncFromUrl` and `syncTo` both read
  // reactive state (selection, viewState) — without untrack, `syncTo`'s internal
  // viewState read+write would self-trigger the effect into an infinite loop.
  $effect(() => {
    untrack(() => {
      nav.syncFromUrl();
      view.syncTo(selection.selectedNibId);
    });
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  });

  function onPopState(e: PopStateEvent) {
    // handlePopState updates selection (honoring the blocked-overlay guard); the
    // view then syncs to the resulting selection. syncTo skips the dirty guard,
    // which is what this path needs: history has already moved, so a confirm
    // would arrive too late to prevent the navigation it asks about.
    nav.handlePopState(e);
    view.syncTo(selection.selectedNibId);
  }

  // A viewed nib that resolves to nothing (deleted / archived / stale link).
  // `view.noteMissing` owns the outcome: a pristine buffer closes (healed below),
  // a dirty one holds the unsaved edits on screen in the "gone" state.
  //
  // This effect acts only while the view is `viewing`. The live-subscription
  // deletion path (useActiveView's bridge) applies DELETED with no dirty gate, so
  // a missing nib can reach "gone" without this effect reporting at all — the
  // close/heal/toast below is not the only outcome for a missing nib. "gone" is
  // not terminal either: a later syncTo on the same id returns the view to
  // `viewing`, where this effect is live again.
  let reportedMissingFor: string | null = null;
  $effect(() => {
    const s = view.state;
    // Only `viewing` can produce a report; `closed`, `creating`, and `gone` return
    // early. That early return — not the latch — is what keeps a report from
    // repeating on the state a report just produced. (`gone` does not imply a
    // report was made: the live bridge's ungated DELETED reaches it too.) Clearing
    // the latch here is safe: getting back to `viewing` on the same still-missing
    // id takes an OPEN — via `open` or the guard-bypassing `syncTo` — after which
    // noteMissing decides afresh against the buffer's dirtiness at that moment.
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
    // Deferred to a microtask so we don't mutate state during the detail query's
    // own effect flush.
    queueMicrotask(() => {
      const outcome = view.noteMissing(id);
      if (outcome === "stale") {
        // The view is not on `id` — it moved to another nib, to "creating", or
        // closed. This report says nothing about whatever (or nothing) is on
        // screen now, so nothing here may act on it. Release the latch (only if
        // it is still ours) so a later report for `id` is not suppressed.
        if (reportedMissingFor === id) reportedMissingFor = null;
        return;
      }
      if (outcome === "kept") {
        // `id`'s buffer is on screen in the "gone" state, so the selection and
        // ?nib= URL still describe what is shown and must survive. The view's own
        // deleted notice reports the deletion — no toast on top.
        return;
      }
      // "closed" — nothing for `id` is on screen any more: heal the URL and say why.
      selection.close();
      nav.replaceClosed();
      toast.error(`Nib ${id} no longer exists`);
    });
  }

  // Collect unique tags from the query results via TreeTable callback
  let availableTags: string[] = $state([]);

  function handleTagsChange(tags: string[]) {
    availableTags = tags;
  }

  // --- Drag-and-drop handlers ---

  /** Handle drop: determine whether to reorder, reparent, or both */
  async function handleDrop(targetNibId: string, zone: DropZone, targetParentId: string | null) {
    // ondrop is called before endDrag(), so draggedIds/draggedParentId are still populated.
    // We copy them immediately since endDrag() runs synchronously after
    // this async function returns to its caller.
    const ids = [...drag.draggedIds];
    const sourceParentId = drag.draggedParentId;
    if (ids.length === 0) return;

    if (zone === "reparent") {
      await mutations.execute(reparentBatch(ids, targetNibId));
    } else {
      // Check if this is a cross-parent move (needs reparent + reorder)
      const crossParent = sourceParentId !== undefined && sourceParentId !== targetParentId;

      if (crossParent) {
        await mutations.execute(reparentAndReorder(ids, targetParentId, targetNibId, zone));
      } else if (ids.length === 1) {
        const opts = zone === "before"
          ? { beforeId: targetNibId }
          : { afterId: targetNibId };
        await mutations.execute(reorderNibCmd(ids[0], opts));
      } else {
        await mutations.execute(reorderChain(ids, targetNibId, zone));
      }
    }

    // After mutation, ensure the primary dragged nib is visible in the tree
    // (TreeTable will expand collapsed ancestors and scroll it into view)
    selection.ensureVisible(ids[0]);
  }

  // Context menu state
  let contextMenuOpen = $state(false);
  let contextMenuPosition = $state({ x: 0, y: 0 });
  let contextMenuNibId: string | null = $state(null);
  let contextMenuNib: TreeTableNib | null = $state(null);
  // Subtree expand/collapse actions for the right-clicked row.
  // TreeTable owns the collapse state, so it hands down closures that mutate it.
  let contextMenuSubtree: RowSubtreeActions | null = $state(null);

  function handleRowContextMenu(nibId: string, event: MouseEvent, nib: TreeTableNib, subtree: RowSubtreeActions) {
    // If the right-clicked nib is not in the selection, open it first — route
    // through the view so the dirty-guard + URL/history stay in sync.
    if (!selection.isSelected(nibId)) {
      view.open(nibId);
    }
    contextMenuNibId = nibId;
    contextMenuNib = nib;
    contextMenuSubtree = subtree;
    contextMenuPosition = { x: event.clientX, y: event.clientY };
    contextMenuOpen = true;
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

  // --- Detail-pane sizing (composable owns the math; App owns measurement + wiring) ---
  let paneGroupEl: HTMLElement | null = $state(null);
  let detailPaneComponent: ReturnType<typeof ResizablePane> | undefined = $state(undefined);
  let containerWidth = $state(0);
  let containerHeight = $state(0);

  // Detail-panel dock position: "right" (horizontal split, size = width) or
  // "bottom" (vertical split, size = height). The size axis, pref field, and
  // PaneForge direction all switch off this.
  const position = $derived(prefs.detailPanelPosition);

  // PaneForge split direction for the active dock. App needs it to pick which
  // measured dimension to feed the layout as `containerSize`, and to set the
  // PaneGroup `direction` — both consume the module's orientation mapping.
  const direction = $derived(orientationOf(position).direction);

  // The measured extent of the split axis: height for a vertical (bottom) split,
  // width otherwise. All px<->% conversions in the layout run against this.
  const containerSize = $derived(direction === "vertical" ? containerHeight : containerWidth);

  // Sizing math (orientation mapping, px<->%, min/max/default percent, and the
  // resize / drag-flush / reset handlers) lives in the layout composable. App
  // feeds it reactive inputs as getters and still owns the ResizeObserver
  // measurement below and the PaneForge wiring in the template.
  const layout = createDetailPaneLayout({
    prefs,
    position: () => position,
    containerSize: () => containerSize,
  });

  // Track container width AND height reactively via ResizeObserver. The size
  // axis PaneForge measures against depends on the dock orientation, so keep both.
  $effect(() => {
    if (!paneGroupEl) return;
    // Synchronously read the initial dimensions so they are valid before
    // PaneForge fires its first onResize callback during mount.
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
    // Reset persists + flushes the default size and returns the percent to
    // resize the PaneForge pane to.
    detailPaneComponent?.resize(layout.reset());
  }

  // Reactively collapse/expand the detail pane based on the docked-view state.
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

  // One-shot initial sizing: on load the pane can mount ALREADY open (a resumed
  // selection) while the container is still unmeasured (size 0), so PaneForge
  // bakes in a size from fallback percents and collapses it to ~min. The
  // collapse/expand effect above skips this case (the pane isn't collapsed), so
  // resize the pane to the correct percent once the container has been measured.
  // Guarded to fire only once — it must never fight the user's later resizes.
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
    {projectName}
    {availableTags}
    oncreatenew={(type) => view.startCreate({ type })}
  />

  <main class="flex-1 min-h-0 flex flex-col px-6 py-6">
    <!-- Re-key on position so the whole PaneGroup remounts when the dock toggles.
         PaneForge fixes the split `direction` at pane-group creation, so the
         reactive `direction` prop alone can't re-orient an existing group — the
         remount is required. Collapse/selection/drag state survive because they
         live in contexts provided OUTSIDE this block. -->
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
          // Capture at schedule time; only close if still docked-open when the
          // frame fires (avoids a resize-drag race). If the dirty-guard refuses
          // the close, re-expand so the pane stays consistent with view state.
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
</div>

<!-- Expanded presentation: the same view, hosted in a full-screen modal overlay.
     Kept separate from the docked pane; the buffer/query survive the swap because
     they live in the presenter, not this component. -->
{#if view.isOpen && view.presentation === "expanded"}
  <div class="anv-modal-backdrop" data-testid="active-nib-modal" role="presentation">
    <div class="anv-modal-shell">
      <ActiveNibView suggestions={availableTags} blockedEmphasis={prefs.blockedEmphasis} {prefs} />
    </div>
  </div>
{/if}

<!-- Add-child type picker: an anchored popover overlaying the whole app (never
     replaces the detail view). Opened from the table [+], row context menu, or
     the detail view's own add-child controls. -->
{#if view.typePicker}
  <TypePickerPopover
    parentType={view.typePicker.parentType}
    anchor={view.typePicker.anchor}
    onselect={(t) => view.chooseType(t)}
    oncancel={() => view.cancelType()}
  />
{/if}

{#if drag.isDragging && drag.draggedIds.length > 1}
  <div
    class="fixed pointer-events-none rounded-full px-2 py-0.5 text-label"
    style="
      left: {drag.cursorX + 12}px;
      top: {drag.cursorY - 12}px;
      z-index: var(--z-modal);
      background-color: var(--accent);
      color: var(--foreground);
      border: 1px solid var(--border);
    "
  >
    {drag.draggedIds.length} items
  </div>
{/if}

<RowContextMenu
  bind:open={contextMenuOpen}
  position={contextMenuPosition}
  nib={contextMenuNib}
  selectedCount={selection.hasMultiSelect ? selection.selectedIds.size : 1}
  hasChildren={contextMenuSubtree?.hasChildren ?? false}
  onexpandchildren={() => contextMenuSubtree?.expandChildren()}
  oncollapsechildren={() => contextMenuSubtree?.collapseChildren()}
/>

<!-- Hand the whole composable to the dialog. The dialog reads its display state
     from it AND owns every route (confirm, Save, dismiss), so there is no inline
     per-route wiring here that a future edit could rewrite from `dismiss()` to a
     bare `close()` (which would drop the dismissal owner and reintroduce the
     nibs-an5d promise leak). (nibs-i567) -->
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
