<script lang="ts">
  import type { TreeTableNib } from "../types";
  import type { RelIdKey } from "$lib/query";
  import { STATUS_WORKFLOW, PRIORITIES } from "../constants";
  import { canHaveChildren } from "../typeHierarchy";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu/index.js";
  import { useSelection, useConfirmDialog, useActiveView, useHistoryNav, useMilestones } from "$lib/contexts";
  import { takesAssignmentAxes } from "../membership";
  import { NO_MILESTONE, fromSelectValue, milestoneChoices, toSelectValue } from "../milestones";
  import { getMutationStore } from "$lib/mutations";
  import {
    setStatusBatch,
    setPriorityBatch,
    setMilestoneBatch,
    deleteBatch,
    archiveBatch,
  } from "$lib/mutations/commands";
  import type { EtagResolver } from "$lib/mutations/commands";
  import { copyToClipboard } from "$lib/clipboard";
  import { getActionTargetIds, clearAfterMutation } from "$lib/actionTarget";

  interface Props {
    open: boolean;
    position: { x: number; y: number };
    nib: TreeTableNib | null;
    selectedCount?: number;
    /** Whether the right-clicked row has (visible) children — gates the
     *  expand/collapse-children options. */
    hasChildren?: boolean;
    onexpandchildren?: () => void;
    oncollapsechildren?: () => void;
    /** Compose a relationship-id filter onto the current filter, targeting this
     *  row (`nib.id`). Single-target only. */
    onfilterrelated?: (field: RelIdKey, id: string) => void;
    /** Resolves each selected nib's etag so batch mutations carry ifMatch.
     *  Supplied by the table, which holds the loaded nibs. */
    etagOf?: EtagResolver;
  }

  let {
    open = $bindable(false),
    position,
    nib,
    selectedCount = 1,
    hasChildren = false,
    onexpandchildren,
    oncollapsechildren,
    onfilterrelated,
    etagOf,
  }: Props = $props();

  // Each label names the result set. Derive it from what the field selects, not
  // from its name: `blockingId` keeps this row's blockers, `blockedById` what it
  // blocks, `ancestorId` its descendants, `descendantId` its ancestors.
  const FILTER_RELATIONS: { label: string; field: RelIdKey }[] = [
    { label: "Items blocking this", field: "blockingId" },
    { label: "Items this blocks", field: "blockedById" },
    { label: "Children of this", field: "parentId" },
    { label: "Descendants of this", field: "ancestorId" },
    { label: "Ancestors of this", field: "descendantId" },
    { label: "Siblings of this", field: "siblingId" },
    { label: "Items mentioning this", field: "mentionsId" },
    { label: "Items this mentions", field: "mentionedById" },
  ];

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

  function handleOpen() {
    if (nib) {
      view.open(nib.id);
    }
  }

  function handleEdit() {
    if (nib) {
      // The view is both viewer and editor.
      view.open(nib.id);
    }
  }

  function handleAddChild(anchor: DOMRect) {
    if (nib) {
      view.startCreateChild(nib.id, nib.type, anchor);
    }
  }

  function handleFilterRelated(field: RelIdKey) {
    if (nib) onfilterrelated?.(field, nib.id);
  }

  async function handleStatusChange(status: string) {
    const ids = getActionTargetIds(selection, nib?.id ?? null);
    if (ids.length === 0) return;
    await mutations.execute(setStatusBatch(ids, status, etagOf));
  }

  async function handlePriorityChange(priority: string) {
    const ids = getActionTargetIds(selection, nib?.id ?? null);
    if (ids.length === 0) return;
    await mutations.execute(setPriorityBatch(ids, priority, etagOf));
  }

  /** One row of a metadata submenu. A milestone's value is an id, its label a title. */
  interface MenuEntry {
    value: string;
    label: string;
    disabled?: boolean;
    title?: string;
  }
  const plain = (v: string): MenuEntry => ({ value: v, label: v });

  const milestones = useMilestones();
  // Refusals are shown only for a single target: in a bulk selection only the
  // right-clicked row's status is known, and the server refuses per row.
  let milestoneEntries = $derived([
    { value: NO_MILESTONE, label: "None" },
    ...milestoneChoices(milestones(), {
      status: nib?.status ?? "",
      milestone: nib?.milestone ?? "",
    }).map((c) => ({
      value: c.id,
      label: c.title,
      disabled: !isBulk && c.refusal !== null,
      title: !isBulk && c.refusal !== null ? c.refusal : undefined,
    })),
  ]);

  async function handleMilestoneChange(value: string) {
    const ids = getActionTargetIds(selection, nib?.id ?? null);
    if (ids.length === 0) return;
    await mutations.execute(setMilestoneBatch(ids, fromSelectValue(value), etagOf));
  }

  function handleCopyId() {
    if (!nib) return;
    copyToClipboard(nib.id);
  }

  function handleDelete() {
    const ids = getActionTargetIds(selection, nib?.id ?? null);
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
          clearAfterMutation(selection, nav, ids);
        }
      },
    });
  }

  function handleArchive() {
    const ids = getActionTargetIds(selection, nib?.id ?? null);
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
          clearAfterMutation(selection, nav, ids);
        }
      },
    });
  }
</script>

{#if open && nib}
  <!-- Declared outside DropdownMenu.Content, where it would become a prop. -->
  {#snippet metadataSubmenu(label: string, entries: readonly MenuEntry[], currentValue: string,
      onchange: (v: string) => void, testId: string)}
    <DropdownMenu.Sub>
      <DropdownMenu.SubTrigger data-testid="ctx-{testId}-trigger">
        {label}
      </DropdownMenu.SubTrigger>
      <DropdownMenu.SubContent>
        {#if isBulk}
          {#each entries as e (e.value)}
            <DropdownMenu.Item
              data-testid="ctx-{testId}-{e.value}"
              disabled={e.disabled}
              title={e.title}
              onclick={() => { open = false; onchange(e.value); }}
            >
              {e.label}
            </DropdownMenu.Item>
          {/each}
        {:else}
          <DropdownMenu.RadioGroup
            value={currentValue}
            onValueChange={(v) => { if (v) { open = false; onchange(v); } }}
          >
            {#each entries as e (e.value)}
              <DropdownMenu.RadioItem
                data-testid="ctx-{testId}-{e.value}"
                value={e.value}
                disabled={e.disabled}
                title={e.title}
              >
                {e.label}
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

      {#if !isBulk}
        <DropdownMenu.Sub>
          <DropdownMenu.SubTrigger data-testid="ctx-filter-related-trigger">
            Filter related
          </DropdownMenu.SubTrigger>
          <DropdownMenu.SubContent>
            {#each FILTER_RELATIONS as rel}
              <DropdownMenu.Item
                data-testid="ctx-filter-{rel.field}"
                onclick={() => { open = false; handleFilterRelated(rel.field); }}
              >
                {rel.label}
              </DropdownMenu.Item>
            {/each}
          </DropdownMenu.SubContent>
        </DropdownMenu.Sub>
        <DropdownMenu.Separator />
      {/if}

      {@render metadataSubmenu("Status", STATUS_WORKFLOW.map(plain), nib.status, handleStatusChange, "status")}

      {@render metadataSubmenu("Priority", PRIORITIES.map(plain), nib.priority, handlePriorityChange, "priority")}

      <!-- A milestone takes no milestone. In a bulk selection only the
           right-clicked row's type is checked; the server refuses the rest. -->
      {#if takesAssignmentAxes(nib.type)}
        {@render metadataSubmenu("Milestone", milestoneEntries, toSelectValue(nib.milestone), handleMilestoneChange, "milestone")}
      {/if}

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
