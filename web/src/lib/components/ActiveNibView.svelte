<script lang="ts">
  /**
   * ActiveNibView — the single, buffered nib view.
   *
   * Renders the `useActiveView` presenter's current state. One component,
   * docked (narrow, single column) and expanded (wide, two columns + rail);
   * the layout keys off measured container width, not the dock position.
   *
   * State-driven render (per `view.state.kind`):
   *   - `viewing` / `gone` / `creating` -> the full three-row-header nib view.
   *     `gone` adds a "deleted" notice and disables inputs; `creating` hides
   *     relationships/documents/archive/delete and its primary button is "Create".
   *
   * The add-child type picker is NOT a state here — it overlays as an anchored
   * popover hosted by App (`view.typePicker`), so it never replaces this view.
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

  import { renderMarkdown, toggleTaskLine } from "../markdown";
  import { getValidChildTypes } from "../typeHierarchy";
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
  import TagEditor from "./TagEditor.svelte";
  import BlockedBadge from "./BlockedBadge.svelte";
  import MarkdownEditor from "./MarkdownEditor.svelte";
  import RelatedNibGroup from "./RelatedNibGroup.svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu/index.js";

  interface Props {
    /** The full tag universe, offered as TagEditor suggestions (already-applied
     *  tags are excluded downstream). Empty while none are known yet. */
    suggestions?: string[];
    /** How the blocked state is emphasized in the header (see BlockedEmphasis). */
    blockedEmphasis?: BlockedEmphasis;
    /** User preferences. The side-by-side Preview toggle reads/writes
     *  `prefs.previewOpen` so the choice persists across remounts and sessions.
     *  Optional (undefined in some tests) → falls back to the default. */
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

  // Blocked-state emphasis in the header (mirrors the tree row): the count comes
  // from the detail query's active-only `blockedBy` (completed/scrapped filtered
  // server-side). `subtle` shows the bare lock; `pill`/`pill-dim` show the pill
  // (nothing to dim in the header, so `pill-dim` renders the same as `pill`).
  const blockedByCount = $derived(detailNib?.blockedBy?.length ?? 0);
  const blockedVariant = $derived(blockedVariantFor(blockedEmphasis));
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
  // The side-by-side Preview pane toggle is backed by the user preference so it
  // survives remounts (docked↔expanded) and sessions. Read through the pref
  // (falling back to the default when no prefs instance is supplied); the toggle
  // writes back to `prefs.previewOpen`, which auto-persists like other prefs.
  const previewOn = $derived(prefs?.previewOpen ?? DEFAULT_PREVIEW_OPEN);
  // `gone` nibs are read-only: never surface the editor even if edit mode was
  // toggled on before the nib was deleted out from under us.
  const bodyModeEffective = $derived<"preview" | "edit">(disabled ? "preview" : bodyMode);

  // A brand-new nib opens in edit mode so the user can type the body straight
  // away; an existing nib opens in preview. This runs on mount and re-applies
  // the default at each new buffer session, so it also fires on in-place
  // transitions (viewing→creating, closed→creating) — ActiveNibView is NOT
  // re-keyed per create. Each create/open swaps the form instance (see
  // useActiveView.reconcileBuffer), so tracking form identity scopes the reset
  // to session boundaries: a user's later toggle within the same session is
  // preserved (we only act on a change).
  let bodyModeSession: CreateForm | EditForm | null = null;
  $effect(() => {
    const f = form;
    if (f === bodyModeSession) return;
    bodyModeSession = f;
    bodyMode = isCreating ? "edit" : "preview";
  });

  // Auto-focus the (empty) title input when a create buffer first appears,
  // so the user can type the title straight away — for both entry
  // points (toolbar New menu and a row's add-child [+]), which both land the
  // view in `creating` with a fresh create form. Scoped to the create form
  // instance (like bodyModeSession above): each create swaps the form (see
  // useActiveView.reconcileBuffer), so this fires once per entry — typing/edits
  // within the same buffer keep the same instance and never re-steal focus. The
  // `isCreating` guard means the create→edit hand-off (form swaps to an edit
  // buffer) does not pull focus. Deferred via queueMicrotask so the input is
  // mounted/bound before we focus (mirrors TagEditor's tick + SettingsSheet's
  // microtask deferral); titleEl is read only inside the callback, so it is not
  // a tracked dependency and binding it never re-runs this effect.
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

  // Minor "Nib updated" toast when the presenter silently rebaselines a CLEAN
  // buffer onto an incoming change (in-app status change, on-disk edit, ...).
  // Tracks the presenter's monotonic counter; the first observed value is
  // adopted without toasting, so mount / nib-swap never fires a spurious toast.
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
  // The title <input>, focused on entering a create buffer (see the effect below).
  let titleEl: HTMLInputElement | undefined = $state();
  // The overflow (⋯) trigger — the type picker anchors to it when "New child
  // nib" is chosen from that menu (the menu item itself is gone by then).
  let menuTriggerEl: HTMLElement | null = $state(null);
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

    // Task-list checkbox: flip the matching source line in the WORKING COPY so it
    // marks the buffer dirty and persists on Save like any other edit — NO
    // auto-save. The ordinal on the checkbox maps to the Nth task line in
    // form.body (see toggleTaskLine). preventDefault so the native toggle doesn't
    // fight the re-render, which re-derives the checked state from the flipped body.
    const checkbox = target?.closest("input[data-task-ordinal]") as HTMLInputElement | null;
    if (checkbox) {
      event.preventDefault();
      const f = form;
      if (!f || disabled) return; // read-only (gone / still-loading) -> ignore
      // Provenance is enforced in markdown.ts (only our nonce-stamped checkboxes
      // keep `data-task-ordinal`), but parse strictly here too: require an
      // all-digits value so an empty/whitespace attr can't coerce to 0.
      const raw = checkbox.dataset.taskOrdinal ?? "";
      if (!/^\d+$/.test(raw)) return;
      // setBody's default is in-place (non-remounting) for exactly this kind of
      // out-of-band edit: an open editor pane syncs the flipped body
      // via a minimal-diff CodeMirror transaction, keeping its undo history /
      // cursor / scroll intact (rather than the {#key} remount).
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
    // `view.savePending`: a null-remote conflict fallback is in flight for this
    // form. `f.saving` is already false by then (EditForm.save resets it before
    // the presenter's fallback fetch), so without this a re-trigger would
    // re-dispatch mid-fallback.
    if (!f || !f.dirty || f.saving || view.savePending) return;
    // Capture the saved instance + id BEFORE the await. The buffer can swap to
    // another nib mid-flight (popstate / syncTo guard-bypass), so the live
    // `form`/`nibId` getters read AFTER the await may point at a different nib.
    // Re-check `form === f` before touching the saved instance and
    // never read the live getters for these branches.
    const savedId = f.mode === "edit" ? f.id : null;
    const outcome = await view.save();
    if (!outcome) return;
    if (outcome.kind === "error") {
      toast.error(outcome.message ?? "Save failed");
    } else if (outcome.kind === "conflict") {
      // Stale-base save: route into the SAME persistent, non-modal resolver as
      // the proactive path (Load theirs / Overwrite) — never a modal. Attach the
      // conflicting snapshot to the SAVED instance only if it is still the live
      // buffer; a swap means nib A's snapshot must never land on nib B's form.
      if (outcome.remote && form === f && f.mode === "edit") f.noteExternalChange(outcome.remote);
    } else if (outcome.kind === "created") {
      toast.success(`Created ${outcome.id}`);
    } else if (outcome.kind === "saved") {
      // Only toast for a save whose buffer is still on screen, and name it with
      // the captured id (not the possibly-swapped live `nibId`).
      if (form === f) toast.success(`Updated ${savedId ?? ""}`.trim());
    }
  }

  function handleDiscard() {
    form?.discard();
  }

  // "Load theirs": drop my unsaved edits and adopt the incoming snapshot. Guard
  // `saving` (F5): a Load-theirs mid-Overwrite would diverge the buffer from disk
  // (the in-flight save's continuation rebaselines against pre-interleave fields).
  function handleLoadTheirs() {
    const f = form;
    // `isGone`: a deleted nib is read-only; never resolve a conflict against it
    // (the resolver is also hidden in that state — MEDIUM #3, belt-and-braces).
    if (isGone || !f || f.mode !== "edit" || f.saving || !f.externalChange) return;
    f.applyExternal(f.externalChange);
  }

  // "Overwrite": keep my edits and force-save over the remote (last-write-wins).
  // Guards `dirty` (F1) so a reverted/clean buffer can't force a stale write, and
  // `saving` to block a double-Overwrite.
  async function handleOverwrite() {
    const f = form;
    // `isGone`: never force-save over a deleted nib (would error / risk
    // resurrecting stale content); the resolver is hidden here too — MEDIUM #3.
    if (isGone || !f || f.mode !== "edit" || f.saving || !f.dirty) return;
    // Capture the id BEFORE the await so the success toast names the saved nib,
    // not whatever the live `nibId` derived reads after the buffer may have swapped.
    const savedId = f.id;
    const outcome = await f.save({ overwrite: true });
    if (!outcome) return;
    if (outcome.kind === "error") toast.error(outcome.message ?? "Save failed");
    // Only toast for a save whose buffer is still on screen (mirror handleSave):
    // a mid-overwrite swap means this success belongs to a nib no longer shown.
    else if (outcome.kind === "saved" && form === f) toast.success(`Updated ${savedId}`);
  }

  function handleNewChild(anchor: AnchorRect) {
    if (nibId) view.startCreateChild(nibId, childParentType, anchor);
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

          {#if !isCreating && blockedByCount > 0}
            <BlockedBadge count={blockedByCount} variant={blockedVariant} />
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

        <!-- Row 2: title (single line, ellipsis, full-text tooltip) -->
        <input
          bind:this={titleEl}
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

      <!-- ============ Deleted notice (gone) ============ -->
      {#if isGone}
        <div class="anv-deleted-notice" data-testid="anv-deleted-notice">This nib was deleted</div>
      {/if}

      <!-- ===== External-change resolver (persistent, non-modal surface) =====
           Only shows when the change arrived while the buffer had unsaved edits
           (a clean buffer is rebaselined silently by the presenter). The
           `form.dirty` gate is load-bearing (F1): a not-dirty buffer must never
           expose Overwrite, or it could force stale/reverted content over the
           remote's newer change. It stays until resolved via Load theirs or
           Overwrite. role="dialog" + aria-modal="false" follows the SettingsSheet
           non-modal idiom (F6) — NOT role="alert" (that assertive live region is
           for brief non-interactive status text, not focusable controls).
           Hidden in the `gone` state (MEDIUM #3): a deleted nib is read-only, so
           the deleted notice alone shows — never it and this resolver together. -->
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
              {#if form.body}
                {@html bodyHtml}
              {:else}
                <span class="anv-empty-body">No description yet.</span>
              {/if}
            </div>
          {:else}
            <div class="anv-editwrap" class:anv-editwrap-side={previewOn && sideBySide}>
              <div class="anv-editor" data-testid="anv-editor-container">
                <!-- ECHO-LOOP CONTRACT (see MarkdownEditor Props):
                     onchange's value is stored VERBATIM into form.body and fed
                     straight back as initialValue — no transform, and NOT through
                     a bumping setBody. The {#key bodyVersion} remount is reserved
                     for genuine baseline resets (discard / applyExternal /
                     create->edit); out-of-band edits (checkbox flip) sync in place. -->
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
    /* Scale with the global font-size preference: the title is
       larger than the body role, so there is no shared token — multiply the
       base size by --font-scale directly, mirroring the type-scale tokens. */
    font-size: calc(1.25rem * var(--font-scale));
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
    font-size: var(--text-body-size);
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
    font-size: var(--text-body-size);
    font-weight: 500;
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
    font-size: var(--text-label-size);
    font-weight: var(--text-label-weight);
    color: var(--muted-foreground);
  }

  /* ---------- content columns ---------- */
  /* Fills the remaining panel height. The single grid row is bounded to the
     panel height (minmax(0, 1fr)) so the body and rail cells are viewport-sized
     rather than content-sized — that lets the body editor fill and scroll
     internally. Scroll is owned by .anv-body / .anv-rail, which each
     scroll independently when their content overflows. */
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

  /* Fills the remaining body-column height so the editor pane is a fixed size
     from the start and scrolls internally, rather than growing with content.
     grid-auto-rows: minmax(0, 1fr) makes every pane share the height equally and
     stay bounded — so the stacked editor+preview layout also scrolls internally
     instead of growing. min-height keeps a sensible floor on very short panels. */
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

  /* min-height: 0 lets the editor shrink into its grid track so CodeMirror's
     internal scroller engages instead of the pane growing to fit content. */
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
