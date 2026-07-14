<script lang="ts">
  import type { TreeTableNib } from "../types";
  import { STATUSES, PRIORITIES } from "../constants";
  import { canHaveChildren } from "../typeHierarchy";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu/index.js";
  import { useSelection, useConfirmDialog, useActiveView, useHistoryNav } from "$lib/contexts";
  import { getMutationStore } from "$lib/mutations";
  import {
    setStatusBatch,
    setPriorityBatch,
    deleteBatch,
    archiveBatch,
  } from "$lib/mutations/commands";
  import { copyToClipboard } from "$lib/clipboard";

  interface Props {
    open: boolean;
    position: { x: number; y: number };
    nib: TreeTableNib | null;
    selectedCount?: number;
    /** Whether the right-clicked row has (visible) children — gates the
     *  expand/collapse-children options (nibs-iyw3). */
    hasChildren?: boolean;
    onexpandchildren?: () => void;
    oncollapsechildren?: () => void;
  }

  let {
    open = $bindable(false),
    position,
    nib,
    selectedCount = 1,
    hasChildren = false,
    onexpandchildren,
    oncollapsechildren,
  }: Props = $props();

  const selection = useSelection();
  const confirmDialog = useConfirmDialog();
  const view = useActiveView();
  const nav = useHistoryNav();
  const mutations = getMutationStore();

  let isBulk = $derived(selectedCount > 1);
  let showAddChild = $derived(!isBulk && nib && canHaveChildren(nib.type));
  let showSubtreeActions = $derived(!isBulk && hasChildren);

  function handleOpenChange(newOpen: boolean) {
    if (!newOpen) {
      open = false;
    }
  }

  /** Resolves which nib IDs an action should target. */
  function getActionTargetIds(): string[] {
    if (selection.hasMultiSelect) return [...selection.selectedIds];
    if (selection.focusedNibId) return [selection.focusedNibId];
    if (nib) return [nib.id];
    return [];
  }

  function handleOpen() {
    if (nib) {
      // Open the unified view (guarded); it routes through nav so the URL/history
      // stay in sync (nibs-58c3).
      view.open(nib.id);
    }
  }

  function handleEdit() {
    if (nib) {
      // Edit is just opening the unified buffered view (create/edit are one view).
      view.open(nib.id);
    }
  }

  function handleAddChild(anchor: DOMRect) {
    if (nib) {
      view.startCreateChild(nib.id, nib.type, anchor);
    }
  }

  async function handleStatusChange(status: string) {
    const ids = getActionTargetIds();
    if (ids.length === 0) return;
    await mutations.execute(setStatusBatch(ids, status));
  }

  async function handlePriorityChange(priority: string) {
    const ids = getActionTargetIds();
    if (ids.length === 0) return;
    await mutations.execute(setPriorityBatch(ids, priority));
  }

  function handleCopyId() {
    if (!nib) return;
    copyToClipboard(nib.id);
  }

  function handleDelete() {
    const ids = getActionTargetIds();
    if (ids.length === 0) return;

    const count = ids.length;
    confirmDialog.showConfirm({
      title: count > 1 ? `Delete ${count} items` : "Delete nib",
      message: count > 1
        ? `Are you sure you want to delete ${count} items? This action cannot be undone.`
        : `Are you sure you want to delete this nib? This action cannot be undone.`,
      label: count > 1 ? `Delete ${count} items` : "Delete",
      variant: "danger",
      action: async () => {
        confirmDialog.close();
        const result = await mutations.execute(deleteBatch(ids));
        if (result.ok) {
          selection.clearAll();
          nav.replaceClosed(); // heal a stale ?nib=<deleted> URL (nibs-etk3)
        }
      },
    });
  }

  function handleArchive() {
    const ids = getActionTargetIds();
    if (ids.length === 0) return;

    const count = ids.length;
    confirmDialog.showConfirm({
      title: count > 1 ? `Archive ${count} items` : "Archive nib",
      message: count > 1
        ? `Are you sure you want to archive ${count} items?`
        : `Are you sure you want to archive this nib?`,
      label: count > 1 ? `Archive ${count} items` : "Archive",
      variant: "warning",
      action: async () => {
        confirmDialog.close();
        const result = await mutations.execute(archiveBatch(ids));
        if (result.ok) {
          selection.clearAll();
          nav.replaceClosed(); // heal a stale ?nib=<archived> URL (nibs-etk3)
        }
      },
    });
  }
</script>

{#if open && nib}
  <!-- Declared here (a child of the {#if} block, not of DropdownMenu.Content)
       so it is a local snippet in lexical scope for the {@render} calls below,
       rather than being interpreted as an unknown prop of Content. -->
  {#snippet metadataSubmenu(label: string, values: readonly string[], currentValue: string,
      onchange: (v: string) => void, testId: string)}
    <DropdownMenu.Sub>
      <DropdownMenu.SubTrigger data-testid="ctx-{testId}-trigger">
        {label}
      </DropdownMenu.SubTrigger>
      <DropdownMenu.SubContent>
        {#if isBulk}
          {#each values as v}
            <DropdownMenu.Item
              data-testid="ctx-{testId}-{v}"
              onclick={() => { open = false; onchange(v); }}
            >
              {v}
            </DropdownMenu.Item>
          {/each}
        {:else}
          <DropdownMenu.RadioGroup
            value={currentValue}
            onValueChange={(v) => { if (v) { open = false; onchange(v); } }}
          >
            {#each values as v}
              <DropdownMenu.RadioItem
                data-testid="ctx-{testId}-{v}"
                value={v}
              >
                {v}
              </DropdownMenu.RadioItem>
            {/each}
          </DropdownMenu.RadioGroup>
        {/if}
      </DropdownMenu.SubContent>
    </DropdownMenu.Sub>
  {/snippet}

  <DropdownMenu.Root open={true} onOpenChange={handleOpenChange}>
    <!-- Hidden trigger positioned at cursor -->
    <DropdownMenu.Trigger
      style="position: fixed; left: {position.x}px; top: {position.y}px; width: 0; height: 0; padding: 0; border: 0; opacity: 0; pointer-events: none;"
    >
    </DropdownMenu.Trigger>

    <DropdownMenu.Content
      data-testid="context-menu"
      class="w-48"
      align="start"
      side="bottom"
    >
      {#if !isBulk}
        <DropdownMenu.Item
          data-testid="ctx-open"
          onclick={() => { open = false; handleOpen(); }}
        >
          Open
        </DropdownMenu.Item>
        <DropdownMenu.Item
          data-testid="ctx-edit"
          onclick={() => { open = false; handleEdit(); }}
        >
          Edit
        </DropdownMenu.Item>
        <DropdownMenu.Separator />
      {/if}

      {#if showSubtreeActions}
        <DropdownMenu.Item
          data-testid="ctx-expand-children"
          onclick={() => { open = false; onexpandchildren?.(); }}
        >
          Expand children
        </DropdownMenu.Item>
        <DropdownMenu.Item
          data-testid="ctx-collapse-children"
          onclick={() => { open = false; oncollapsechildren?.(); }}
        >
          Collapse children
        </DropdownMenu.Item>
        <DropdownMenu.Separator />
      {/if}

      {#if showAddChild}
        <DropdownMenu.Item
          data-testid="ctx-add-child"
          onclick={(e) => {
            // Capture the item's rect before the menu closes; the picker anchors there.
            const anchor = (e.currentTarget as HTMLElement).getBoundingClientRect();
            open = false;
            handleAddChild(anchor);
          }}
        >
          Add child
        </DropdownMenu.Item>
        <DropdownMenu.Separator />
      {/if}

      {@render metadataSubmenu("Status", STATUSES, nib.status, handleStatusChange, "status")}

      {@render metadataSubmenu("Priority", PRIORITIES, nib.priority, handlePriorityChange, "priority")}

      <DropdownMenu.Separator />

      {#if !isBulk}
        <DropdownMenu.Item
          data-testid="ctx-copy-id"
          onclick={() => { open = false; handleCopyId(); }}
        >
          Copy ID
        </DropdownMenu.Item>
        <DropdownMenu.Separator />
      {/if}

      <DropdownMenu.Item
        data-testid="ctx-delete"
        variant="destructive"
        onclick={() => { open = false; handleDelete(); }}
      >
        {isBulk ? `Delete ${selectedCount} items` : "Delete"}
      </DropdownMenu.Item>
      <DropdownMenu.Item
        data-testid="ctx-archive"
        onclick={() => { open = false; handleArchive(); }}
      >
        {isBulk ? `Archive ${selectedCount} items` : "Archive"}
      </DropdownMenu.Item>
    </DropdownMenu.Content>
  </DropdownMenu.Root>
{/if}
