<script lang="ts">
  /**
   * ActiveNibView — the buffered nib view, rendering `useActiveView`'s state.
   * Docked and expanded share one layout, keyed off measured width.
   *
   * `gone` shows a notice and disables inputs; `creating` hides relationships,
   * documents, archive and delete. The add-child type picker is not a state here:
   * App hosts it (`view.typePicker`).
   *
   * Edits are buffered on `view.form` until Save. The buffer, dirty guard and
   * navigation live in the presenter.
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

  import { renderMarkdown, toggleTaskLine } from "../markdown";
  import { getValidChildTypes } from "../typeHierarchy";
  import { takesAssignmentAxes } from "../membership";
  import { copyToClipboard } from "$lib/clipboard";
  import { getMutationStore } from "$lib/mutations";
  import { useActiveView, useConfirmDialog } from "$lib/contexts";
  import {
    deleteNib as deleteNibCmd,
    archiveNib as archiveNibCmd,
  } from "$lib/mutations/commands";
  import type { CreateForm, EditForm } from "../nibForm.svelte";
  import type { BlockedEmphasis } from "../types";
  import { DEFAULT_BLOCKED_EMPHASIS, DEFAULT_PREVIEW_OPEN, blockedVariantFor } from "../types";
  import type { Preferences } from "../preferences.svelte";
  import type { DetailNibRef, AnchorRect } from "../composables/useActiveView.svelte";

  import StatusSelect from "./StatusSelect.svelte";
  import TypeSelect from "./TypeSelect.svelte";
  import PrioritySelect from "./PrioritySelect.svelte";
  import EstimateSelect from "./EstimateSelect.svelte";
  import MilestoneSelect from "./MilestoneSelect.svelte";
  import AreaSelect from "./AreaSelect.svelte";
  import TagEditor from "./TagEditor.svelte";
  import RelationBadge from "./RelationBadge.svelte";
  import MarkdownEditor from "./MarkdownEditor.svelte";
  import RelatedNibGroup from "./RelatedNibGroup.svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu/index.js";

  interface Props {
    /** Every known tag; TagEditor excludes the applied ones. */
    suggestions?: string[];
    blockedEmphasis?: BlockedEmphasis;
    /** Backs the Preview toggle (`prefs.previewOpen`). Defaults apply without it. */
    prefs?: Preferences;
  }

  let { suggestions = [], blockedEmphasis = DEFAULT_BLOCKED_EMPHASIS, prefs }: Props = $props();

  const view = useActiveView();
  const mutations = getMutationStore();
  const confirmDialog = useConfirmDialog();

  // --- presenter-derived views -------------------------------------------
  const viewState = $derived(view.state);
  const form = $derived(view.form);
  const detailNib = $derived(view.detail?.nib ?? null);

  const isCreating = $derived(viewState.kind === "creating");
  const isGone = $derived(viewState.kind === "gone");
  const goneReason = $derived(viewState.kind === "gone" ? viewState.reason : null);

  // The detail query's blockedBy/blocking exclude completed and scrapped nibs.
  const blockedByCount = $derived(detailNib?.blockedBy?.length ?? 0);
  const blockingCount = $derived(detailNib?.blocking?.length ?? 0);
  const blockedVariant = $derived(blockedVariantFor(blockedEmphasis));
  // An edit buffer is a blank placeholder until seeded (by the create hand-off or
  // the detail query), so inputs stay disabled until then. `form.etag` marks a
  // seeded form.
  const loadingUnseeded = $derived.by(() => {
    if (form?.mode !== "edit" || form.etag) return false;
    const d = view.detail;
    return !!d?.fetching && !d?.nib?.etag;
  });
  const disabled = $derived(isGone || loadingUnseeded);

  const nibId = $derived(form && form.mode === "edit" ? form.id : null);
  const currentType = $derived(form?.type ?? "task");
  // Valid child types follow the saved type, not the buffered one.
  const childParentType = $derived(detailNib?.type ?? currentType);
  const childTypes = $derived(getValidChildTypes(childParentType));

  // --- mention resolver (preview) ----------------------------------------
  // A token matches a mentioned id exactly, or by the suffix after its prefix dash.
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
  // A preference, so it survives docked↔expanded remounts.
  const previewOn = $derived(prefs?.previewOpen ?? DEFAULT_PREVIEW_OPEN);
  const bodyModeEffective = $derived<"preview" | "edit">(disabled ? "preview" : bodyMode);

  // Each buffer session opens a new nib in edit mode and an existing one in
  // preview. This view is not re-keyed per create, but every create/open swaps the
  // form instance (useActiveView.reconcileBuffer), so tracking form identity
  // resets at session boundaries and keeps a toggle made within one.
  let bodyModeSession: CreateForm | EditForm | null = null;
  $effect(() => {
    const f = form;
    if (f === bodyModeSession) return;
    bodyModeSession = f;
    bodyMode = isCreating ? "edit" : "preview";
  });

  // Focus the title once per create buffer, keyed on form identity as above; the
  // create→edit hand-off does not pull focus. Deferred so the input is bound, and
  // titleEl is read inside the callback so it is not a tracked dependency.
  let titleFocusSession: CreateForm | EditForm | null = null;
  $effect(() => {
    const f = form;
    if (!isCreating || !f) {
      titleFocusSession = null;
      return;
    }
    if (f === titleFocusSession) return;
    titleFocusSession = f;
    queueMicrotask(() => titleEl?.focus());
  });

  // Toast when the presenter silently rebaselines a clean buffer onto an incoming
  // change. The first observed count is adopted without a toast.
  let externalAppliedSeen = -1;
  $effect(() => {
    const n = view.externalApplied;
    if (externalAppliedSeen === -1) {
      externalAppliedSeen = n;
      return;
    }
    if (n !== externalAppliedSeen) {
      externalAppliedSeen = n;
      toast.success("Nib updated");
    }
  });

  // Two INDEPENDENT width breakpoints, both fed by one ResizeObserver:
  //   (a) rootWidth >= 720  -> relationships move to a right rail (else stack)
  //   (b) bodyColWidth >= 560 -> editor + preview sit side-by-side (else stack)
  let rootEl: HTMLDivElement | undefined = $state();
  let bodyColEl: HTMLDivElement | undefined = $state();
  let titleEl: HTMLInputElement | undefined = $state();
  // "New child nib" anchors the type picker here; its menu item is gone by then.
  let menuTriggerEl: HTMLElement | null = $state(null);
  let rootWidth = $state(0);
  let bodyColWidth = $state(0);
  const wide = $derived(rootWidth >= 720);
  const sideBySide = $derived(bodyColWidth >= 560);
  const showRail = $derived(wide && !isCreating && hasRelated);

  // Reading rootEl/bodyColEl re-creates the observer when either remounts.
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

    // Task checkbox: flip its source line in the working copy, saved with the
    // buffer. preventDefault so the native toggle does not fight the re-render.
    const checkbox = target?.closest("input[data-task-ordinal]") as HTMLInputElement | null;
    if (checkbox) {
      event.preventDefault();
      const f = form;
      if (!f || disabled) return;
      // An empty attribute must not coerce to ordinal 0.
      const raw = checkbox.dataset.taskOrdinal ?? "";
      if (!/^\d+$/.test(raw)) return;
      // setBody syncs an open editor in place, without a remount.
      f.setBody(toggleTaskLine(f.body, Number(raw)));
      return;
    }

    const anchor = target?.closest("a[data-nib-id]") as HTMLAnchorElement | null;
    if (!anchor) return;
    event.preventDefault();
    const id = anchor.dataset.nibId;
    if (id) view.open(id);
  }

  async function handleSave() {
    const f = form;
    // `isGone`: no save starts from a gone panel, archived included (an archived
    // buffer saves through the dirty-nav prompt instead).
    // `view.savePending`: a conflict fallback is in flight after `f.saving` reset.
    if (isGone || !f || !f.dirty || f.saving || view.savePending) return;
    // The buffer can swap mid-await: capture the id now and check `form === f`
    // after.
    const savedId = f.mode === "edit" ? f.id : null;
    const outcome = await view.save();
    if (!outcome) return;
    if (outcome.kind === "missing") {
      // view.save() routed the buffer to gone/deleted; its notice replaces a toast.
    } else if (outcome.kind === "error") {
      toast.error(outcome.message ?? "Save failed");
    } else if (outcome.kind === "conflict") {
      if (outcome.remote && form === f && f.mode === "edit") f.noteExternalChange(outcome.remote);
    } else if (outcome.kind === "created") {
      toast.success(`Created ${outcome.id}`);
    } else if (outcome.kind === "saved") {
      if (form === f) toast.success(`Updated ${savedId ?? ""}`.trim());
    }
  }

  function handleDiscard() {
    form?.discard();
  }

  // Drop unsaved edits and adopt the incoming snapshot. Not while saving: the
  // in-flight save would rebaseline against the old fields.
  function handleLoadTheirs() {
    const f = form;
    if (isGone || !f || f.mode !== "edit" || f.saving || !f.externalChange) return;
    f.applyExternal(f.externalChange);
  }

  // Keep unsaved edits and force-save over the remote. Requires `dirty`, so a
  // clean buffer cannot force a stale write.
  async function handleOverwrite() {
    const f = form;
    if (isGone || !f || f.mode !== "edit" || f.saving || !f.dirty) return;
    const savedId = f.id; // captured before the await, as in handleSave
    const outcome = await f.save({ overwrite: true });
    if (!outcome) return;
    if (outcome.kind === "missing") {
      // f.save() bypasses view.save(), so route the deletion here. Check
      // `form === f`: after Close→Discard and a reopen of the same id,
      // noteMissing's id check passes and it would close the fresh buffer.
      if (form === f) view.noteMissing(f.id);
    } else if (outcome.kind === "error") toast.error(outcome.message ?? "Save failed");
    else if (outcome.kind === "saved" && form === f) toast.success(`Updated ${savedId}`);
  }

  function handleNewChild(anchor: AnchorRect) {
    if (nibId) view.startCreateChild(nibId, childParentType, anchor);
  }

  function handleCopyId() {
    if (nibId) copyToClipboard(nibId);
  }

  // A gone buffer shows only the rendered preview; this copies the markdown source.
  function handleCopyBody() {
    const f = form;
    if (f) copyToClipboard(f.body, "body");
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
            onaction={childTypes.length > 0
              ? (e) => handleNewChild((e.currentTarget as HTMLElement).getBoundingClientRect())
              : undefined}
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

{#if form}
  <div
    bind:this={rootEl}
    class="anv"
    class:anv-expanded={view.presentation === "expanded"}
    data-testid="active-nib-view"
    role="complementary"
    aria-label="Nib detail"
  >
    <!-- Top region: header + metadata band, which the type-color band spans. -->
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

          {#if !isCreating && blockedByCount > 0}
            <RelationBadge kind="blocked" count={blockedByCount} variant={blockedVariant} />
          {/if}
          {#if !isCreating && blockingCount > 0}
            <RelationBadge kind="blocking" count={blockingCount} variant={blockedVariant} />
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
                    bind:ref={menuTriggerEl}
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
                  onSelect={() => menuTriggerEl && handleNewChild(menuTriggerEl.getBoundingClientRect())}
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

        <!-- Row 2: title. `readonly` while gone keeps unsaved edits selectable for
             copying; an unseeded title has nothing to recover and stays
             `disabled`. The metadata band is not selectable while gone: the
             vendored select trigger sets `select-none` unconditionally. -->
        <input
          bind:this={titleEl}
          type="text"
          class="anv-title"
          data-testid="anv-title"
          aria-label="Title"
          placeholder="Nib title..."
          title={form.title}
          bind:value={form.title}
          readonly={isGone}
          disabled={loadingUnseeded}
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
              size="default"
              data-testid="anv-save"
              disabled={!form.dirty || form.saving || disabled || view.savePending}
              onclick={handleSave}
            >
              {form.saving || view.savePending ? "Saving..." : isCreating ? "Create" : "Save"}
            </Button>
            <Button
              variant="outline"
              size="default"
              data-testid="anv-discard"
              disabled={!form.dirty || form.saving || disabled}
              onclick={handleDiscard}
            >
              Discard
            </Button>
          </div>
        </div>
      </div>

      <!-- ============ Gone notice (deleted / archived) ============
           "Copy body" is offered only for deleted, the one state `canSaveState`
           refuses: an archived buffer can still be saved from the close guard's
           prompt. -->
      {#if isGone}
        <div
          class="anv-gone-notice"
          class:anv-gone-notice--archived={goneReason === "archived"}
          data-testid="anv-gone-notice"
        >
          <span>{goneReason === "archived" ? "This nib was archived" : "This nib was deleted"}</span>
          {#if form.body && goneReason === "deleted"}
            <div class="anv-gone-actions">
              <!-- Colors follow the notice band: `outline` paints bg-background
                   under the band's inherited text color. Each `dark:` / `hover:`
                   class needs its own override, since tailwind-merge keeps a
                   modifier'd class beside a later bare one (utils.test.ts).
                   `bg-black/10` is not a semantic token because the band uses the
                   same `--destructive` pair in every theme. -->
              <Button
                variant="outline"
                size="sm"
                class="border-current bg-transparent text-current hover:bg-black/10 hover:text-current dark:bg-transparent dark:border-current dark:hover:bg-black/10"
                data-testid="anv-gone-copy-body"
                title="Copy the description's markdown source"
                onclick={handleCopyBody}
              >
                <Copy size={14} />
                Copy body
              </Button>
            </div>
          {/if}
        </div>
      {/if}

      <!-- ===== External-change resolver (non-modal) =====
           Dirty buffers only: a clean one is rebaselined silently, and Overwrite
           on it could force stale content over the remote. role="dialog"
           aria-modal="false" as in SettingsSheet; not role="alert", which is for
           non-interactive status text. -->
      {#if !isGone && form.mode === "edit" && form.externalChange && form.dirty}
        <div
          class="anv-conflict"
          data-testid="anv-conflict-banner"
          role="dialog"
          aria-modal="false"
          aria-label="This nib changed elsewhere — resolve the conflict"
        >
          <span>This nib changed elsewhere while you were editing — keep your edits or load the new version.</span>
          <div class="anv-conflict-actions">
            <Button
              variant="outline"
              size="sm"
              data-testid="anv-conflict-load-theirs"
              disabled={form.saving}
              onclick={handleLoadTheirs}
            >
              Load theirs
            </Button>
            <Button
              variant="outline"
              size="sm"
              data-testid="anv-conflict-overwrite"
              disabled={!form.dirty || form.saving}
              onclick={handleOverwrite}
            >
              Overwrite
            </Button>
          </div>
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
        <!-- A milestone takes neither axis (`takesAssignmentAxes`). Milestone is
             also edit-only: CreateNibInput has no milestone field. -->
        {#if form.mode === "edit" && takesAssignmentAxes(form.type)}
          <div class="anv-field">
            <span class="anv-field-label">Milestone</span>
            <MilestoneSelect
              value={form.milestone}
              subjectStatus={form.status}
              onchange={(v) => (form.milestone = v)}
              testId="anv-milestone"
              {disabled}
            />
          </div>
        {/if}
        <!-- CreateNibInput has `area`, so Area is available while creating. -->
        {#if takesAssignmentAxes(form.type)}
          <div class="anv-field">
            <span class="anv-field-label">Area</span>
            <AreaSelect
              value={form.area}
              onchange={(v) => (form.area = v)}
              testId="anv-area"
              {disabled}
            />
          </div>
        {/if}
      </div>
      </div>
    </div>

    <!-- ============ Content: body column (+ optional rail) ============ -->
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
                  onclick={() => { if (prefs) prefs.previewOpen = !prefs.previewOpen; }}
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
              {#if form.body.trim()}
                {@html bodyHtml}
              {:else}
                <span class="anv-empty-body">No description yet.</span>
              {/if}
            </div>
          {:else}
            <div class="anv-editwrap" class:anv-editwrap-side={previewOn && sideBySide}>
              <div class="anv-editor" data-testid="anv-editor-container">
                <!-- onchange's value goes back as initialValue verbatim (see
                     MarkdownEditor's echo-loop contract). {#key bodyVersion}
                     remounts only on a baseline reset. -->
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
                  {#if form.body.trim()}
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
    font-size: var(--text-label-size);
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
    /* No type-scale token is this size, so apply --font-scale directly. */
    font-size: calc(1.25rem * var(--font-scale));
    font-weight: 620;
    letter-spacing: -0.01em;
    outline: none;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* Hover border only on an editable title (:read-write excludes readonly and
     disabled). The focus ring stays ungated: a readonly title is focusable. */
  .anv-title:read-write:hover {
    border-color: var(--border);
  }

  .anv-title:focus {
    border-color: var(--ring);
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

  /* ---------- gone / conflict banners ---------- */
  /* Same shape as .anv-conflict below: message left, actions right. */
  .anv-gone-notice {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    margin: 0 0.8rem 0.5rem;
    padding: 0.5rem 0.75rem;
    background-color: var(--destructive);
    color: var(--destructive-foreground);
    border-radius: var(--radius-md);
    font-size: var(--text-body-size);
    font-weight: 400;
  }

  .anv-gone-actions {
    display: flex;
    gap: 0.4rem;
    flex: none;
  }

  /* An archived nib still exists and saves: warning, not destructive. */
  .anv-gone-notice--archived {
    background-color: var(--warning);
    color: var(--warning-foreground, white);
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
    font-size: var(--text-body-size);
    font-weight: 400;
  }

  .anv-conflict-actions {
    display: flex;
    gap: 0.4rem;
    flex: none;
  }

  /* ---------- metadata band (full-bleed tinted strip) ---------- */
  .anv-metaband {
    display: flex;
    flex-wrap: wrap;
    gap: 0.65rem 1rem;
    align-items: flex-end;
    /* Same background as the rail. */
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
    font-size: var(--text-label-size);
    font-weight: var(--text-label-weight);
    color: var(--muted-foreground);
  }

  /* ---------- content columns ---------- */
  /* The row is bounded (minmax(0, 1fr)) so .anv-body and .anv-rail each scroll
     on their own. */
  .anv-content {
    display: grid;
    grid-template-columns: 1fr;
    grid-template-rows: minmax(0, 1fr);
    flex: 1;
    min-height: 0;
    overflow: hidden;
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
    min-height: 0;
    overflow-y: auto;
  }

  .anv-section-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .anv-section-label {
    font-size: var(--text-label-size);
    font-weight: var(--text-label-weight);
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
    font-size: var(--text-label-size);
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
    font-size: var(--text-label-size);
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

  /* Panes share the remaining height and scroll internally instead of growing. */
  .anv-editwrap {
    display: grid;
    grid-template-columns: 1fr;
    grid-auto-rows: minmax(0, 1fr);
    gap: 0.7rem;
    flex: 1;
    min-height: 230px;
  }

  .anv-editwrap-side {
    grid-template-columns: 1fr 1fr;
  }

  /* min-height: 0 lets CodeMirror's own scroller engage. */
  .anv-editor {
    min-height: 0;
    min-width: 0;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }

  .anv-preview-pane {
    min-height: 0;
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
    min-height: 0;
    overflow-y: auto;
  }

  .anv-rail-head {
    font-size: var(--text-label-size);
    font-weight: var(--text-label-weight);
    letter-spacing: 0.02em;
    color: var(--muted-foreground);
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
    font-size: var(--text-body-size);
  }

  .anv-document-path {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
