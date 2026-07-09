<script lang="ts">
  /**
   * ActiveNibView — the single, buffered nib view (#nibs-893d).
   *
   * Renders the `useActiveView` presenter's current state. One component,
   * docked (narrow, single column) and expanded (wide, two columns + rail);
   * the layout keys off measured container width, not the dock position.
   *
   * State-driven render (per `view.state.kind`):
   *   - `pickingType` -> the shared TypePickerPopover.
   *   - `viewing` / `gone` / `creating` -> the full three-row-header nib view.
   *     `gone` adds a "deleted" notice and disables inputs; `creating` hides
   *     relationships/documents/archive/delete and its primary button is "Create".
   *
   * All edits are buffered on `view.form` (CreateForm | EditForm) — nothing
   * persists until Save. This component owns only presentational/local state
   * (body edit-vs-preview toggle, responsive breakpoints); the buffer, the
   * dirty-guard, and navigation live in the presenter.
   */
  import { toast } from "svelte-sonner";
  import {
    X,
    Maximize2,
    Minimize2,
    Ellipsis,
    Plus,
    Copy,
    Archive,
    Trash2,
    SquarePen,
    FileText,
  } from "@lucide/svelte";

  import { renderMarkdown } from "../markdown";
  import { getValidChildTypes } from "../typeHierarchy";
  import { copyToClipboard } from "$lib/clipboard";
  import { getMutationStore } from "$lib/mutations";
  import { useActiveView, useConfirmDialog } from "$lib/contexts";
  import {
    deleteNib as deleteNibCmd,
    archiveNib as archiveNibCmd,
  } from "$lib/mutations/commands";
  import type { EditForm } from "../nibForm.svelte";
  import type { DetailNibRef } from "../composables/useActiveView.svelte";

  import StatusSelect from "./StatusSelect.svelte";
  import TypeSelect from "./TypeSelect.svelte";
  import PrioritySelect from "./PrioritySelect.svelte";
  import EstimateSelect from "./EstimateSelect.svelte";
  import TagEditor from "./TagEditor.svelte";
  import MarkdownEditor from "./MarkdownEditor.svelte";
  import RelatedNibGroup from "./RelatedNibGroup.svelte";
  import TypePickerPopover from "./TypePickerPopover.svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu/index.js";

  interface Props {
    /** The full tag universe, offered as TagEditor suggestions (already-applied
     *  tags are excluded downstream). Empty while none are known yet. */
    suggestions?: string[];
  }

  let { suggestions = [] }: Props = $props();

  const view = useActiveView();
  const mutations = getMutationStore();
  const confirmDialog = useConfirmDialog();

  // --- presenter-derived views -------------------------------------------
  const viewState = $derived(view.state);
  const form = $derived(view.form);
  const detailNib = $derived(view.detail?.nib ?? null);

  const isCreating = $derived(viewState.kind === "creating");
  const isGone = $derived(viewState.kind === "gone");
  // An edit buffer renders a blank placeholder form until its detail query
  // resolves (or, for a create→edit hand-off, until it's seeded). Disable inputs
  // during that window so there's no editable-blank flash. `form.etag` is the
  // "seeded" signal (set by the create hand-off seed OR by the async detail
  // seed's applyExternal); `!view.detail?.nib?.etag` covers the tick before the
  // seed effect runs.
  const loadingUnseeded = $derived.by(() => {
    if (form?.mode !== "edit" || form.etag) return false;
    const d = view.detail;
    return !!d?.fetching && !d?.nib?.etag;
  });
  const disabled = $derived(isGone || loadingUnseeded); // gone / still-loading -> read-only

  // The nib id the buffer targets (edit forms only; null while creating).
  const nibId = $derived(form && form.mode === "edit" ? form.id : null);
  // Current buffered type drives the edge band + valid-child-type logic.
  const currentType = $derived(form?.type ?? "task");
  // Parent type for child creation uses the *saved* type when available.
  const childParentType = $derived(detailNib?.type ?? currentType);
  const childTypes = $derived(getValidChildTypes(childParentType));

  // --- mention resolver (preview) ----------------------------------------
  // Resolve against the detail query's `mentions` (full ids). A token matches
  // full-form (`#nibs-gx0f`) via equality, short-form (`#gx0f`) via the id's
  // suffix after the prefix dash — no config lookup needed.
  const mentionIds = $derived(
    new Set<string>((detailNib?.mentions ?? []).map((m) => m.id)),
  );
  const bodyHtml = $derived.by(() => {
    const ids = mentionIds;
    const resolve = (token: string): string | null => {
      if (ids.has(token)) return token;
      for (const id of ids) if (id.endsWith(`-${token}`)) return id;
      return null;
    };
    return renderMarkdown(form?.body ?? "", resolve);
  });

  // --- relationships presence --------------------------------------------
  const hasRelated = $derived(
    !!detailNib &&
      (!!detailNib.parent ||
        (detailNib.children?.length ?? 0) > 0 ||
        (detailNib.blockedBy?.length ?? 0) > 0 ||
        (detailNib.blocking?.length ?? 0) > 0 ||
        (detailNib.mentions?.length ?? 0) > 0 ||
        (detailNib.mentionedBy?.length ?? 0) > 0),
  );
  const hasDocuments = $derived((detailNib?.documents?.length ?? 0) > 0);

  function refs(items: readonly DetailNibRef[] | undefined) {
    return (items ?? []).map((r) => ({ id: r.id, title: r.title, status: r.status }));
  }

  // --- local presentational state ----------------------------------------
  let bodyMode: "preview" | "edit" = $state("preview");
  let previewOn = $state(true);
  // `gone` nibs are read-only: never surface the editor even if edit mode was
  // toggled on before the nib was deleted out from under us.
  const bodyModeEffective = $derived<"preview" | "edit">(disabled ? "preview" : bodyMode);

  // Two INDEPENDENT width breakpoints, both fed by one ResizeObserver:
  //   (a) rootWidth >= 720  -> relationships move to a right rail (else stack)
  //   (b) bodyColWidth >= 560 -> editor + preview sit side-by-side (else stack)
  let rootEl: HTMLDivElement | undefined = $state();
  let bodyColEl: HTMLDivElement | undefined = $state();
  let rootWidth = $state(0);
  let bodyColWidth = $state(0);
  const wide = $derived(rootWidth >= 720);
  const sideBySide = $derived(bodyColWidth >= 560);
  // Relationships/documents only exist for a real nib (not while creating).
  const showRail = $derived(wide && !isCreating && hasRelated);

  // One observer, re-created whenever either measured element (re)mounts —
  // reading rootEl/bodyColEl makes the effect re-run on layout swaps.
  $effect(() => {
    const root = rootEl;
    const col = bodyColEl;
    if (!root) return;
    const ro = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const w = entry.contentRect.width;
        if (entry.target === root) rootWidth = w;
        else if (entry.target === col) bodyColWidth = w;
      }
    });
    ro.observe(root);
    if (col) ro.observe(col);
    return () => ro.disconnect();
  });

  // --- actions ------------------------------------------------------------
  function handleProseClick(event: MouseEvent) {
    const target = event.target as HTMLElement | null;
    const anchor = target?.closest("a[data-nib-id]") as HTMLAnchorElement | null;
    if (!anchor) return;
    event.preventDefault();
    const id = anchor.dataset.nibId;
    if (id) view.open(id);
  }

  async function handleSave() {
    const f = form;
    if (!f || !f.dirty || f.saving) return;
    const outcome = await view.save();
    if (!outcome) return;
    if (outcome.kind === "error") {
      toast.error(outcome.message ?? "Save failed");
    } else if (outcome.kind === "conflict") {
      confirmDialog.showConfirm({
        title: "Overwrite external changes?",
        message: "This nib was modified externally. Saving will overwrite those changes.",
        label: "Overwrite",
        variant: "warning",
        action: async () => {
          confirmDialog.close();
          const editForm = form as EditForm;
          await editForm.save({ overwrite: true });
        },
      });
    } else if (outcome.kind === "created") {
      toast.success(`Created ${outcome.id}`);
    } else if (outcome.kind === "saved") {
      toast.success(`Updated ${nibId ?? ""}`.trim());
    }
  }

  function handleDiscard() {
    form?.discard();
  }

  function handleReloadExternal() {
    const f = form;
    if (f && f.mode === "edit" && f.externalChange) f.applyExternal(f.externalChange);
  }

  function handleNewChild() {
    if (nibId) view.startCreateChild(nibId, childParentType);
  }

  function handleCopyId() {
    if (nibId) copyToClipboard(nibId);
  }

  function handleArchive() {
    if (!nibId) return;
    const id = nibId;
    confirmDialog.showConfirm({
      title: "Archive nib?",
      message: `This will move ${id} to the archive.`,
      label: "Archive",
      variant: "warning",
      action: async () => {
        confirmDialog.close();
        const result = await mutations.execute(archiveNibCmd(id));
        if (result.ok) view.requestClose();
      },
    });
  }

  function handleDelete() {
    if (!nibId) return;
    const id = nibId;
    confirmDialog.showConfirm({
      title: "Delete nib?",
      message: `This will permanently delete ${id}. This action cannot be undone.`,
      label: "Delete",
      variant: "danger",
      action: async () => {
        confirmDialog.close();
        const result = await mutations.execute(deleteNibCmd(id));
        if (result.ok) view.requestClose();
      },
    });
  }
</script>

{#snippet relatedGroups()}
  {#if detailNib && hasRelated}
    {#key nibId}
      <div class="anv-related" data-testid="anv-related-section">
        {#if detailNib.parent}
          <RelatedNibGroup
            label="Parent"
            items={refs([detailNib.parent])}
            onnibselect={(id) => view.open(id)}
            testId="anv-related-parent"
          />
        {/if}
        {#if (detailNib.children?.length ?? 0) > 0}
          <RelatedNibGroup
            label="Children"
            items={refs(detailNib.children)}
            onnibselect={(id) => view.open(id)}
            onaction={childTypes.length > 0 ? handleNewChild : undefined}
            actionLabel="Add child nib"
            testId="anv-related-children"
          />
        {/if}
        {#if (detailNib.blockedBy?.length ?? 0) > 0}
          <RelatedNibGroup
            label="Blocked by"
            items={refs(detailNib.blockedBy)}
            onnibselect={(id) => view.open(id)}
            testId="anv-related-blocked-by"
          />
        {/if}
        {#if (detailNib.blocking?.length ?? 0) > 0}
          <RelatedNibGroup
            label="Blocking"
            items={refs(detailNib.blocking)}
            onnibselect={(id) => view.open(id)}
            testId="anv-related-blocking"
          />
        {/if}
        {#if (detailNib.mentions?.length ?? 0) > 0}
          <RelatedNibGroup
            label="Mentions"
            items={refs(detailNib.mentions)}
            onnibselect={(id) => view.open(id)}
            testId="anv-related-mentions"
          />
        {/if}
        {#if (detailNib.mentionedBy?.length ?? 0) > 0}
          <RelatedNibGroup
            label="Mentioned by"
            items={refs(detailNib.mentionedBy)}
            onnibselect={(id) => view.open(id)}
            testId="anv-related-mentioned-by"
          />
        {/if}
      </div>
    {/key}
  {/if}
{/snippet}

{#snippet documentsList()}
  {#if hasDocuments && detailNib}
    <div class="anv-documents" data-testid="anv-documents-section">
      <span class="anv-section-label">Documents</span>
      <ul class="anv-documents-list">
        {#each detailNib.documents ?? [] as doc}
          <li class="anv-document-item" data-testid="anv-document">
            <FileText size={14} />
            <span class="anv-document-path" title={doc}>{doc}</span>
          </li>
        {/each}
      </ul>
    </div>
  {/if}
{/snippet}

{#if viewState.kind === "pickingType"}
  <TypePickerPopover
    parentType={viewState.parentType}
    onselect={(t) => view.chooseType(t)}
    oncancel={() => view.cancelType()}
  />
{:else if form}
  <div
    bind:this={rootEl}
    class="anv"
    class:anv-expanded={view.presentation === "expanded"}
    data-testid="active-nib-view"
    role="complementary"
    aria-label="Nib detail"
  >
    <!-- Top region: header + metadata band. The type-color band spans only this
         region (it stops at the metaband's bottom edge, not the body/rail). -->
    <div class="anv-top">
      <div class="anv-band" style="background: var(--type-{currentType})" aria-hidden="true"></div>

      <div class="anv-topmain">
      <!-- ============ Header: three rows ============ -->
      <div class="anv-head">
        <!-- Row 1: id + unsaved dot | expand/collapse · overflow · close -->
        <div class="anv-head-top">
          {#if isCreating}
            <span class="anv-id anv-id-new" data-testid="anv-id">
              {#if form.dirty}
                <span class="anv-unsaved-dot" title="Unsaved changes" data-testid="anv-unsaved-dot"></span>
              {/if}
              New {currentType}
            </span>
          {:else}
            <Button
              variant="ghost"
              size="sm"
              class="-ml-2 cursor-pointer font-normal text-muted-foreground"
              data-testid="anv-id"
              title={`Copy nib ID ${nibId}`}
              aria-label={`Copy nib ID ${nibId}`}
              onclick={handleCopyId}
            >
              {#if form.dirty}
                <span class="anv-unsaved-dot" title="Unsaved changes" data-testid="anv-unsaved-dot"></span>
              {/if}
              {nibId}
            </Button>
          {/if}

          <span class="anv-grow"></span>

          {#if view.presentation === "docked"}
            <Button
              variant="ghost"
              size="icon-sm"
              data-testid="anv-expand"
              title="Expand to full screen"
              aria-label="Expand"
              onclick={() => view.expand()}
            >
              <Maximize2 size={16} />
            </Button>
          {:else}
            <Button
              variant="ghost"
              size="icon-sm"
              data-testid="anv-collapse"
              title="Collapse to sidebar"
              aria-label="Collapse"
              onclick={() => view.collapse()}
            >
              <Minimize2 size={16} />
            </Button>
          {/if}

          {#if !isCreating}
            <DropdownMenu.Root>
              <DropdownMenu.Trigger>
                {#snippet child({ props })}
                  <Button
                    {...props}
                    variant="ghost"
                    size="icon-sm"
                    data-testid="anv-overflow"
                    title="More actions"
                    aria-label="More actions"
                  >
                    <Ellipsis size={16} />
                  </Button>
                {/snippet}
              </DropdownMenu.Trigger>
              <DropdownMenu.Content align="end" class="w-52">
                <DropdownMenu.Item
                  data-testid="anv-menu-new-child"
                  disabled={childTypes.length === 0 || disabled}
                  onSelect={handleNewChild}
                >
                  <Plus size={15} />
                  New child nib
                </DropdownMenu.Item>
                <DropdownMenu.Item data-testid="anv-menu-copy-id" onSelect={handleCopyId}>
                  <Copy size={15} />
                  Copy ID
                </DropdownMenu.Item>
                <DropdownMenu.Separator />
                <DropdownMenu.Item
                  data-testid="anv-menu-archive"
                  class="text-archive"
                  disabled={disabled}
                  onSelect={handleArchive}
                >
                  <Archive size={15} />
                  Archive
                </DropdownMenu.Item>
                <DropdownMenu.Item
                  data-testid="anv-menu-delete"
                  class="text-delete"
                  disabled={disabled}
                  onSelect={handleDelete}
                >
                  <Trash2 size={15} />
                  Delete
                </DropdownMenu.Item>
              </DropdownMenu.Content>
            </DropdownMenu.Root>
          {/if}

          <Button
            variant="ghost"
            size="icon-sm"
            data-testid="anv-close"
            title="Close"
            aria-label="Close detail panel"
            onclick={() => view.requestClose()}
          >
            <X size={16} />
          </Button>
        </div>

        <!-- Row 2: title (single line, ellipsis, full-text tooltip) -->
        <input
          type="text"
          class="anv-title"
          data-testid="anv-title"
          aria-label="Title"
          placeholder="Nib title..."
          title={form.title}
          bind:value={form.title}
          {disabled}
        />

        <!-- Row 3: tags (inline) | Save + Discard -->
        <div class="anv-head-actions">
          <div class="anv-tags">
            <TagEditor
              tags={[...form.tags]}
              {suggestions}
              onadd={(t) => form.addTag(t)}
              onremove={(t) => form.removeTag(t)}
              chipTestId="anv-tag"
              removeTestId="anv-tag-remove"
              inputTestId="anv-tag-input"
              addTestId="anv-tag-add"
              {disabled}
            />
          </div>
          <div class="anv-saveset">
            <Button
              size="sm"
              class="text-sm"
              data-testid="anv-save"
              disabled={!form.dirty || form.saving || disabled}
              onclick={handleSave}
            >
              {form.saving ? "Saving..." : isCreating ? "Create" : "Save"}
            </Button>
            <Button
              variant="outline"
              size="sm"
              class="text-sm"
              data-testid="anv-discard"
              disabled={!form.dirty || form.saving || disabled}
              onclick={handleDiscard}
            >
              Discard
            </Button>
          </div>
        </div>
      </div>

      <!-- ============ Deleted notice (gone) ============ -->
      {#if isGone}
        <div class="anv-deleted-notice" data-testid="anv-deleted-notice">This nib was deleted</div>
      {/if}

      <!-- ============ External-change (conflict) banner ============ -->
      {#if form.mode === "edit" && form.externalChange}
        <div class="anv-conflict" data-testid="anv-conflict-banner">
          <span>This nib was modified externally.</span>
          <Button variant="outline" size="sm" data-testid="anv-conflict-reload" onclick={handleReloadExternal}>
            Reload
          </Button>
        </div>
      {/if}

      <!-- ============ Metadata band (full-bleed tinted strip) ============ -->
      <div class="anv-metaband" data-testid="anv-metaband">
        <div class="anv-field">
          <span class="anv-field-label">Status</span>
          <StatusSelect value={form.status} onchange={(v) => (form.status = v)} testId="anv-status" {disabled} />
        </div>
        <div class="anv-field">
          <span class="anv-field-label">Type</span>
          <TypeSelect value={form.type} onchange={(v) => (form.type = v)} testId="anv-type" {disabled} />
        </div>
        <div class="anv-field">
          <span class="anv-field-label">Priority</span>
          <PrioritySelect value={form.priority} onchange={(v) => (form.priority = v)} testId="anv-priority" {disabled} />
        </div>
        <div class="anv-field">
          <span class="anv-field-label">Estimate</span>
          <EstimateSelect value={form.estimate} onchange={(v) => (form.estimate = v)} testId="anv-estimate" {disabled} />
        </div>
      </div>
      </div>
    </div>

    <!-- ============ Content: body column (+ optional rail) ============ -->
    <!-- Fills the remaining panel height and scrolls; the rail's right column
         therefore stretches to the bottom of the panel. -->
    <div class="anv-content" class:anv-two-col={showRail}>
        <div class="anv-body" bind:this={bodyColEl}>
          <div class="anv-section-head">
            <span class="anv-section-label">Description</span>
            <div class="anv-mini-actions">
              <button
                type="button"
                class="anv-mini-btn"
                class:anv-mini-on={bodyModeEffective === "edit"}
                data-testid="anv-edit-toggle"
                aria-pressed={bodyModeEffective === "edit"}
                {disabled}
                onclick={() => (bodyMode = bodyMode === "edit" ? "preview" : "edit")}
              >
                <SquarePen size={13} />
                {bodyModeEffective === "edit" ? "Editing" : "Edit"}
              </button>
              {#if bodyModeEffective === "edit"}
                <button
                  type="button"
                  class="anv-switch"
                  role="switch"
                  aria-checked={previewOn}
                  aria-label="Preview"
                  data-testid="anv-preview-toggle"
                  onclick={() => (previewOn = !previewOn)}
                >
                  <span class="anv-switch-label">Preview</span>
                  <span class="anv-switch-track" class:anv-switch-on={previewOn}>
                    <span class="anv-switch-knob"></span>
                  </span>
                </button>
              {/if}
            </div>
          </div>

          {#if bodyModeEffective === "preview"}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="prose-nib anv-prose" data-testid="anv-body-prose" onclick={handleProseClick}>
              {#if form.body}
                {@html bodyHtml}
              {:else}
                <span class="anv-empty-body">No description yet.</span>
              {/if}
            </div>
          {:else}
            <div class="anv-editwrap" class:anv-editwrap-side={previewOn && sideBySide}>
              <div class="anv-editor" data-testid="anv-editor-container">
                {#key form.bodyVersion}
                  <MarkdownEditor
                    initialValue={form.body}
                    onchange={(v) => (form.body = v)}
                    onsave={handleSave}
                  />
                {/key}
              </div>
              {#if previewOn}
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <div class="prose-nib anv-prose anv-preview-pane" data-testid="anv-preview-pane" onclick={handleProseClick}>
                  {#if form.body}
                    {@html bodyHtml}
                  {:else}
                    <span class="anv-empty-body">Preview will appear here...</span>
                  {/if}
                </div>
              {/if}
            </div>
          {/if}

          <!-- Narrow / no-rail: relationships + documents stack under the body. -->
          {#if !isCreating && !showRail}
            {@render relatedGroups()}
            {@render documentsList()}
          {/if}
        </div>

        <!-- Wide: relationships + documents move to a right rail. -->
        {#if showRail}
          <aside class="anv-rail" data-testid="anv-rail">
            <div>
              <div class="anv-rail-head">Related work</div>
              {@render relatedGroups()}
            </div>
            {@render documentsList()}
          </aside>
        {/if}
      </div>
  </div>
{/if}

<style>
  .anv {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-width: 0;
    overflow: hidden;
    background: var(--background);
    color: var(--foreground);
  }

  /* Top region: band + header + metadata band. Fixed height (does not scroll);
     the band is a flex sibling that stretches to this region's bottom edge. */
  .anv-top {
    display: flex;
    flex: none;
  }

  .anv-band {
    width: 5px;
    flex: none;
  }

  .anv-topmain {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }

  /* ---------- header (three rows) ---------- */
  .anv-head {
    display: flex;
    flex-direction: column;
    gap: 0.55rem;
    padding: 0.6rem 0.8rem 0.75rem;
  }

  .anv-head-top {
    display: flex;
    align-items: center;
    gap: 0.25rem;
  }

  .anv-id {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    font-size: 0.8rem;
    color: var(--muted-foreground);
  }

  .anv-id-new {
    padding: 0.24rem 0.1rem;
    text-transform: capitalize;
  }

  .anv-grow {
    flex: 1;
  }

  .anv-unsaved-dot {
    width: 0.5rem;
    height: 0.5rem;
    border-radius: 50%;
    background: var(--primary);
    box-shadow: 0 0 0 3px color-mix(in oklab, var(--primary), transparent 78%);
    flex: none;
  }

  .anv-title {
    width: 100%;
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--radius-md);
    padding: 0.12rem 0.35rem;
    color: var(--foreground);
    font-size: 1.25rem;
    font-weight: 620;
    letter-spacing: -0.01em;
    outline: none;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .anv-title:hover:not(:disabled) {
    border-color: var(--border);
  }

  .anv-title:focus {
    border-color: var(--ring);
    background: var(--accent);
  }

  .anv-title::placeholder {
    color: var(--muted-foreground);
  }

  .anv-head-actions {
    display: flex;
    align-items: center;
    gap: 0.6rem;
  }

  .anv-tags {
    flex: 1;
    min-width: 0;
  }

  .anv-saveset {
    display: flex;
    gap: 0.4rem;
    flex: none;
  }

  /* ---------- deleted / conflict banners ---------- */
  .anv-deleted-notice {
    margin: 0 0.8rem 0.5rem;
    padding: 0.5rem 0.75rem;
    background-color: var(--destructive);
    color: var(--destructive-foreground);
    border-radius: var(--radius-md);
    font-size: 0.875rem;
    font-weight: 500;
  }

  .anv-conflict {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    margin: 0 0.8rem 0.5rem;
    padding: 0.5rem 0.75rem;
    background-color: var(--warning);
    color: var(--warning-foreground, white);
    border-radius: var(--radius-md);
    font-size: 0.8125rem;
    font-weight: 500;
  }

  /* ---------- metadata band (full-bleed tinted strip) ---------- */
  .anv-metaband {
    display: flex;
    flex-wrap: wrap;
    gap: 0.65rem 1rem;
    align-items: flex-end;
    /* Match the rail background exactly; the top/bottom hairlines are the
       separator between the top region and the body/rail below. */
    background: color-mix(in oklab, var(--background), var(--card) 35%);
    border-top: 1px solid var(--border);
    border-bottom: 1px solid var(--border);
    padding: 0.65rem 0.9rem;
  }

  .anv-field {
    display: flex;
    flex-direction: column;
    gap: 0.18rem;
  }

  .anv-field-label {
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--muted-foreground);
  }

  /* ---------- content columns ---------- */
  /* Fills the remaining panel height and owns the scroll. The single grid row
     stretches (align-content) so the rail cell reaches the panel bottom when the
     content is short; it grows past the container and scrolls when content is
     tall. */
  .anv-content {
    display: grid;
    grid-template-columns: 1fr;
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  .anv-content.anv-two-col {
    grid-template-columns: 1fr 268px;
  }

  .anv-body {
    padding: 0.85rem 0.9rem;
    display: flex;
    flex-direction: column;
    gap: 0.85rem;
    min-width: 0;
  }

  .anv-section-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .anv-section-label {
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--muted-foreground);
  }

  .anv-mini-actions {
    display: inline-flex;
    gap: 0.15rem;
  }

  .anv-mini-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    height: 1.5rem;
    padding: 0 0.4rem;
    border-radius: var(--radius-md);
    font-size: 0.72rem;
    color: var(--muted-foreground);
    background: none;
    border: 0;
    cursor: pointer;
  }

  .anv-mini-btn:hover {
    background: var(--accent);
    color: var(--foreground);
  }

  .anv-mini-btn.anv-mini-on {
    color: var(--primary);
    background: color-mix(in oklab, var(--primary), transparent 88%);
  }

  /* Preview toggle rendered as a small switch (pill track + sliding knob). */
  .anv-switch {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    height: 1.5rem;
    padding: 0 0.2rem 0 0.4rem;
    border: 0;
    background: none;
    color: var(--muted-foreground);
    font-size: 0.72rem;
    cursor: pointer;
    border-radius: var(--radius-md);
  }

  .anv-switch:hover {
    color: var(--foreground);
  }

  .anv-switch-track {
    position: relative;
    width: 1.6rem;
    height: 0.9rem;
    flex: none;
    border-radius: 9999px;
    background: var(--border);
    transition: background 0.15s;
  }

  .anv-switch-track.anv-switch-on {
    background: var(--primary);
  }

  .anv-switch-knob {
    position: absolute;
    top: 0.1rem;
    left: 0.1rem;
    width: 0.7rem;
    height: 0.7rem;
    border-radius: 50%;
    background: var(--primary-foreground);
    box-shadow: 0 1px 2px oklch(0 0 0 / 0.3);
    transition: transform 0.15s;
  }

  .anv-switch-track.anv-switch-on .anv-switch-knob {
    transform: translateX(0.7rem);
  }

  .anv-prose {
    min-width: 0;
  }

  .anv-empty-body {
    color: var(--muted-foreground);
    font-style: italic;
  }

  .anv-editwrap {
    display: grid;
    grid-template-columns: 1fr;
    gap: 0.7rem;
    min-height: 230px;
  }

  .anv-editwrap-side {
    grid-template-columns: 1fr 1fr;
  }

  .anv-editor {
    min-height: 200px;
    min-width: 0;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }

  .anv-preview-pane {
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 0.7rem 0.8rem;
    background: var(--background);
    overflow: auto;
  }

  /* ---------- rail ---------- */
  .anv-rail {
    border-left: 1px solid var(--border);
    padding: 0.85rem 0.9rem;
    display: flex;
    flex-direction: column;
    gap: 1.1rem;
    background: color-mix(in oklab, var(--background), var(--card) 35%);
    min-width: 0;
  }

  .anv-rail-head {
    font-size: 0.72rem;
    font-weight: 600;
    letter-spacing: 0.02em;
    color: var(--foreground);
    margin-bottom: 0.3rem;
  }

  .anv-related {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  /* ---------- documents ---------- */
  .anv-documents {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .anv-documents-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .anv-document-item {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    color: var(--link);
    padding: 0.25rem 0.4rem;
    border-radius: var(--radius-md);
  }

  .anv-document-path {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
