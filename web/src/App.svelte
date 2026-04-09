<script lang="ts">
  import { setContextClient, queryStore } from "@urql/svelte";
  import { createClient } from "./lib/graphql";
  import { CONFIG_QUERY } from "./lib/queries";
  import { Preferences } from "./lib/preferences.svelte";
  import { DEFAULT_DETAIL_PANEL_WIDTH } from "./lib/types";
  import Toolbar from "./lib/components/Toolbar.svelte";
  import FilterBar from "./lib/components/FilterBar.svelte";
  import TreeTable from "./lib/components/TreeTable.svelte";
  import DetailPanel from "./lib/components/DetailPanel.svelte";
  import EditorModal from "./lib/components/EditorModal.svelte";
  import TypePickerPopover from "./lib/components/TypePickerPopover.svelte";
  import RowContextMenu from "./lib/components/RowContextMenu.svelte";
  import ConfirmDialog from "./lib/components/ConfirmDialog.svelte";
  import { SelectionState } from "./lib/selection.svelte";
  import { DragState } from "./lib/drag.svelte";
  import type { DropZone } from "./lib/drag.svelte";
  import { provideSelection, provideDrag, provideConfirmDialog, provideEditorOrchestration } from "./lib/contexts";
  import { createConfirmDialog } from "./lib/composables/useConfirmDialog.svelte";
  import { createEditorOrchestration } from "./lib/composables/useEditorOrchestration.svelte";
  import { useKeyboardShortcuts } from "./lib/composables/useKeyboardShortcuts.svelte";
  import type { TreeTableNib } from "./lib/types";
  import * as Resizable from "./lib/components/ui/resizable";
  import type ResizablePane from "./lib/components/ui/resizable/resizable-pane.svelte";
  import { Toaster } from "./lib/components/ui/sonner";
  import { initMutationStore } from "./lib/mutations";
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
  const selection = new SelectionState();
  const drag = new DragState();
  provideSelection(selection);
  provideDrag(drag);
  const confirmDialog = createConfirmDialog();
  provideConfirmDialog(confirmDialog);
  const editor = createEditorOrchestration({ client, selection });
  provideEditorOrchestration(editor);
  let filtersOpen = $state(false);

  // Collect unique tags from the query results via TreeTable callback
  let availableTags: string[] = $state([]);

  function handleTagsChange(tags: string[]) {
    availableTags = tags;
  }

  function handleToggleFilters() {
    filtersOpen = !filtersOpen;
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

  function handleRowContextMenu(nibId: string, event: MouseEvent, nib: TreeTableNib) {
    // If the right-clicked nib is not in the selection, select it first
    if (!selection.isSelected(nibId)) {
      selection.select(nibId);
    }
    contextMenuNibId = nibId;
    contextMenuNib = nib;
    contextMenuPosition = { x: event.clientX, y: event.clientY };
    contextMenuOpen = true;
  }

  // --- Global keyboard shortcuts ---
  useKeyboardShortcuts({
    selection,
    editor,
    confirmDialog,
    mutations,
    getContextMenuNibId: () => contextMenuNibId,
  });

  // --- PaneForge resize integration ---
  let paneGroupEl: HTMLElement | null = $state(null);
  let detailPaneComponent: ReturnType<typeof ResizablePane> | undefined = $state(undefined);
  let containerWidth = $state(0);

  // Sensible default when container hasn't been measured yet (~30% for detail panel)
  const FALLBACK_DETAIL_SIZE_PERCENT = 30;

  // Track container width reactively via ResizeObserver
  $effect(() => {
    if (!paneGroupEl) return;
    // Synchronously read the initial width so containerWidth is valid before
    // PaneForge fires its first onResize callback during mount.
    containerWidth = paneGroupEl.offsetWidth;
    const observer = new ResizeObserver(([entry]) => {
      containerWidth = entry.contentRect.width;
    });
    observer.observe(paneGroupEl);
    return () => observer.disconnect();
  });

  // Convert pixel width to percentage of the container
  function pixelToPercent(px: number): number {
    if (containerWidth <= 0) return FALLBACK_DETAIL_SIZE_PERCENT;
    return (px / containerWidth) * 100;
  }

  // Convert percentage to pixel width
  function percentToPixel(pct: number): number {
    if (containerWidth <= 0) return DEFAULT_DETAIL_PANEL_WIDTH;
    return (pct / 100) * containerWidth;
  }

  // Computed min/max as percentages (recalculated when container changes)
  let minSizePercent = $derived(pixelToPercent(200));
  let maxSizePercent = $derived(pixelToPercent(800));
  let defaultSizePercent = $derived(pixelToPercent(prefs.detailPanelWidth));

  function handleDetailPaneResize(size: number) {
    if (containerWidth <= 0) return; // Skip until ResizeObserver has fired
    const px = percentToPixel(size);
    prefs.setDetailPanelWidth(px);
  }

  function handleDraggingChange(dragging: boolean) {
    if (!dragging) {
      // Drag ended — persist the width
      prefs.flushDetailPanelWidth();
    }
  }

  function handleResizeHandleDblClick() {
    prefs.setDetailPanelWidth(DEFAULT_DETAIL_PANEL_WIDTH);
    prefs.flushDetailPanelWidth();
    // Resize the PaneForge pane to match the new pixel width
    if (detailPaneComponent) {
      detailPaneComponent.resize(pixelToPercent(DEFAULT_DETAIL_PANEL_WIDTH));
    }
  }

  // Reactively collapse/expand the detail pane based on selection state
  $effect(() => {
    if (!detailPaneComponent) return;
    if (selection.panelOpen) {
      if (detailPaneComponent.isCollapsed()) {
        detailPaneComponent.expand();
        detailPaneComponent.resize(defaultSizePercent);
      }
    } else {
      if (!detailPaneComponent.isCollapsed()) {
        detailPaneComponent.collapse();
      }
    }
  });
</script>

<Toaster richColors />

<div class="h-screen flex flex-col">
  <header class="border-b border-border px-6 py-4">
    <h1 class="text-xl font-semibold">Nibs{projectName ? ` - ${projectName}` : ""}</h1>
  </header>

  <main class="flex-1 min-h-0 flex flex-col px-6 py-6">
    <div class="mb-4 flex flex-col gap-2">
      <Toolbar
        {prefs}
        ontogglefilters={handleToggleFilters}
        {filtersOpen}
        oncreatenew={(type) => editor.handleCreateNew(type)}
      />
      {#if filtersOpen}
        <FilterBar
          {prefs}
          {availableTags}
        />
      {/if}
    </div>
    <Resizable.PaneGroup
      direction="horizontal"
      class="flex-1 min-h-0"
      bind:ref={paneGroupEl}
    >
      <Resizable.Pane class="min-w-0 min-h-0">
        <TreeTable
          {prefs}
          ontagschange={handleTagsChange}
          onrowcontextmenu={handleRowContextMenu}
          onaddchild={editor.handleAddChild}
          rowDensity={prefs.rowDensity}
          ondrop={handleDrop}
        />
      </Resizable.Pane>
      {#if selection.panelOpen}
        <Resizable.Handle
          data-testid="resize-handle"
          ondblclick={handleResizeHandleDblClick}
          onDraggingChange={handleDraggingChange}
        />
      {/if}
      <Resizable.Pane
        defaultSize={selection.panelOpen ? defaultSizePercent : 0}
        minSize={selection.panelOpen ? minSizePercent : 0}
        maxSize={selection.panelOpen ? maxSizePercent : 0}
        collapsible={true}
        collapsedSize={0}
        onResize={handleDetailPaneResize}
        onCollapse={() => { if (selection.panelOpen) selection.close(); }}
        bind:this={detailPaneComponent}
        data-testid="detail-pane"
      >
        {#if selection.panelOpen && selection.selectedNibId}
          <DetailPanel
            nibId={selection.selectedNibId}
            onclose={() => selection.close()}
            onnibselect={(nibId) => selection.select(nibId)}
            onedit={editor.handleEditNib}
            onaddchild={editor.handleAddChild}
          />
        {/if}
      </Resizable.Pane>
    </Resizable.PaneGroup>
  </main>
</div>

{#if drag.isDragging && drag.draggedIds.length > 1}
  <div
    class="fixed pointer-events-none rounded-full px-2 py-0.5 text-xs font-medium"
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

<EditorModal
  open={editor.editorOpen}
  mode={editor.editorMode}
  nibId={editor.editorNibId}
  nibData={editor.editorNibData}
  defaultType={editor.editorDefaultType}
  defaultParent={editor.editorDefaultParent}
  onclose={editor.handleEditorClose}
  onsave={editor.handleEditorSave}
/>

{#if editor.typePickerOpen}
  <TypePickerPopover
    parentType={editor.typePickerParentType}
    onselect={editor.handleTypePickerSelect}
    oncancel={() => { editor.closeTypePicker(); }}
  />
{/if}

<RowContextMenu
  bind:open={contextMenuOpen}
  position={contextMenuPosition}
  nib={contextMenuNib}
  selectedCount={selection.hasMultiSelect ? selection.selectedIds.size : 1}
/>

<ConfirmDialog
  open={confirmDialog.open}
  title={confirmDialog.title}
  message={confirmDialog.message}
  confirmLabel={confirmDialog.label}
  variant={confirmDialog.variant}
  onconfirm={() => { confirmDialog.action?.(); }}
  oncancel={() => { confirmDialog.close(); }}
/>
