<script lang="ts">
  import { onDestroy } from "svelte";
  import { X, FileText, Trash2, Archive } from "@lucide/svelte";
  import { getContextClient, queryStore, subscriptionStore } from "@urql/svelte";
  import { NIB_DETAIL_QUERY, NIB_CHANGED_SUBSCRIPTION, CONFIG_QUERY } from "../queries";
  import { renderMarkdown } from "../markdown";

  import StatusSelect from "./StatusSelect.svelte";
  import TypeSelect from "./TypeSelect.svelte";
  import PrioritySelect from "./PrioritySelect.svelte";
  import EstimateSelect from "./EstimateSelect.svelte";
  import TagEditor from "./TagEditor.svelte";
  import RelatedNibGroup from "./RelatedNibGroup.svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import { copyToClipboard } from "$lib/clipboard";
  import { getMutationStore } from "$lib/mutations";
  import { useConfirmDialog } from "$lib/contexts";
  import { updateNib as updateNibCmd, deleteNib as deleteNibCmd, archiveNib as archiveNibCmd } from "$lib/mutations/commands";
  import type { UpdateNibInput } from "$lib/mutations/types";

  interface Props {
    nibId: string;
    onclose: () => void;
    onnibselect?: (nibId: string) => void;
    onedit?: (nibId: string) => void;
    onaddchild?: (parentId: string, parentType: string) => void;
    /** Fired once when this nibId resolves to a nonexistent nib (deleted /
     *  archived / bad link). Lets the app heal the stale ?nib= URL + close
     *  the panel (nibs-etk3). Not fired for the "deleted-while-viewing" notice. */
    onmissing?: (nibId: string) => void;
  }

  let { nibId, onclose, onnibselect, onedit, onaddchild, onmissing }: Props = $props();

  const client = getContextClient();
  const mutations = getMutationStore();
  const confirmDialog = useConfirmDialog();

  let editTitle: string = $state("");
  let deleted: boolean = $state(false);
  let highlighted: boolean = $state(false);

  const result = $derived(
    queryStore({
      client,
      query: NIB_DETAIL_QUERY,
      variables: { id: nibId },
    })
  );

  // Subscribe to CONFIG_QUERY to read the configured nib ID prefix for
  // resolving short-form mentions. Urql's cache-first default dedupes this
  // against App.svelte's subscription.
  const configResult = $derived(
    queryStore({
      client,
      query: CONFIG_QUERY,
    })
  );

  let nib = $derived($result.data?.nib ?? null);
  let fetching = $derived($result.fetching);

  // When the detail query settles with no nib (and no error), this nibId no
  // longer exists (deleted / archived / stale link). Fire onmissing ONCE per id
  // so the app can heal the stale ?nib= URL and close the panel (nibs-etk3).
  // A query *error* is NOT treated as missing (it may be transient). This is
  // distinct from the "deleted-while-viewing" notice, where `nib` stays non-null
  // (cached) and the panel deliberately stays open.
  let reportedMissingFor: string | null = null;
  $effect(() => {
    if (!fetching && !$result.error && nib === null && reportedMissingFor !== nibId) {
      reportedMissingFor = nibId;
      onmissing?.(nibId);
    }
  });
  let prefix = $derived($configResult.data?.config?.prefix ?? "");
  // Set of full mention IDs from the nib's resolved mentions. Used by the
  // resolver below to decide which `#<id>` tokens to rewrite as anchors.
  let mentionIdSet = $derived(new Set<string>((nib?.mentions ?? []).map((m: { id: string }) => m.id)));
  // Use `$derived.by` with an explicit reactive read of `mentionIdSet` inside
  // the body of the derivation. If we closed over `mentionIdSet` via a plain
  // function passed to `renderMarkdown`, the derivation would not re-run when
  // `mentionIdSet` changes while `nib.body` stays constant.
  let bodyHtml = $derived.by(() => {
    const ids = mentionIdSet; // explicit reactive read
    const pfx = prefix; // explicit reactive read
    const resolve = (token: string): string | null => {
      // User may write either `#gx0f` (short) or `#nibs-gx0f` (full). Since
      // nib.mentions returns full IDs, the first branch only matches when the
      // token is already a full ID; the second branch handles the short form
      // with the configured prefix.
      if (ids.has(token)) return token; // full form: e.g. #nibs-gx0f
      if (!pfx) return null; // no prefix configured, short form is meaningless
      const full = `${pfx}${token}`;
      if (ids.has(full)) return full; // short form: e.g. #gx0f
      return null;
    };
    return renderMarkdown(nib?.body ?? "", resolve);
  });
  let actionPending = $derived(mutations.isMutating(nibId));

  // Delegated handler on the .prose-nib container for mention anchors. Non-
  // mention anchors (`<a href="https://...">`) are NOT intercepted — normal
  // browser navigation proceeds for those.
  //
  // Enter-key activation on a focused `<a href>` is handled by the browser: it
  // synthesizes a click event, which this handler receives. No separate keydown
  // handler is needed — adding one would double-fire onnibselect because
  // preventDefault on keydown does not suppress the subsequent synthetic click.
  function handleProseClick(event: MouseEvent) {
    const target = event.target as HTMLElement | null;
    if (!target) return;
    const anchor = target.closest("a[data-nib-id]") as HTMLAnchorElement | null;
    if (!anchor) return;
    event.preventDefault();
    const id = anchor.dataset.nibId;
    if (id) onnibselect?.(id);
  }

  // Sync editTitle when nib data changes
  let lastSyncedId: string | null = $state(null);
  let lastSyncedEtag: string | null = $state(null);

  $effect(() => {
    if (nib && (nib.id !== lastSyncedId || nib.etag !== lastSyncedEtag)) {
      editTitle = nib.title;
      lastSyncedId = nib.id;
      lastSyncedEtag = nib.etag;
    }
  });

  // Subscribe to real-time changes for the viewed nib
  const subscription = $derived(
    subscriptionStore({
      client,
      query: NIB_CHANGED_SUBSCRIPTION,
      variables: { id: nibId },
    })
  );

  // Handle subscription events — use plain variables (not $state) to avoid proxy identity issues
  let lastSubNibId: string | null = null;
  let lastSubData: unknown = null;
  let highlightTimeout: ReturnType<typeof setTimeout> | null = null;
  $effect(() => {
    // Reset deleted state when nibId changes
    if (nibId !== lastSubNibId) {
      deleted = false;
      lastSubNibId = nibId;
      lastSubData = null;
    }

    const data = $subscription.data;
    if (data && data !== lastSubData) {
      lastSubData = data;
      const event = data.nibChanged;
      if (event?.type === "deleted") {
        deleted = true;
      } else if (event?.type === "updated" || event?.type === "created") {
        if (highlightTimeout) clearTimeout(highlightTimeout);
        highlighted = true;
        highlightTimeout = setTimeout(() => { highlighted = false; }, 1000);
      }
    }
  });

  onDestroy(() => { if (highlightTimeout) clearTimeout(highlightTimeout); });

  $effect(() => {
    if ($subscription.error) {
      console.warn("Detail panel subscription error:", $subscription.error);
    }
  });

  async function doUpdateNib(input: UpdateNibInput) {
    if (deleted) return false;
    const result = await mutations.execute(updateNibCmd(nibId, input, nib?.etag));
    return result.ok;
  }

  async function handleTitleBlur() {
    if (!nib || editTitle === nib.title) return;
    const ok = await doUpdateNib({ title: editTitle });
    if (!ok && nib) {
      editTitle = nib.title;
    }
  }

  function handleTitleKeydown(e: KeyboardEvent) {
    if (e.key === "Enter") {
      (e.target as HTMLInputElement).blur();
    }
  }

  async function handleStatusChange(value: string) {
    await doUpdateNib({ status: value });
  }

  async function handleTypeChange(value: string) {
    await doUpdateNib({ type: value });
  }

  async function handlePriorityChange(value: string) {
    await doUpdateNib({ priority: value || null });
  }

  async function handleEstimateChange(value: string) {
    await doUpdateNib({ estimate: value || null });
  }

  async function handleAddTag(tag: string) {
    // Duplicate-tag validation is handled by TagEditor before calling onadd
    await doUpdateNib({ addTags: [tag] });
  }

  async function handleRemoveTag(tag: string) {
    await doUpdateNib({ removeTags: [tag] });
  }

  function handleDeleteNib() {
    confirmDialog.showConfirm({
      title: "Delete nib?",
      message: `This will permanently delete ${nibId}. This action cannot be undone.`,
      label: "Delete",
      variant: "danger",
      action: async () => {
        confirmDialog.close();
        const result = await mutations.execute(deleteNibCmd(nibId));
        if (result.ok) {
          onclose();
        }
      },
    });
  }

  function handleArchiveNib() {
    confirmDialog.showConfirm({
      title: "Archive nib?",
      message: `This will move ${nibId} to the archive.`,
      label: "Archive",
      variant: "warning",
      action: async () => {
        confirmDialog.close();
        const result = await mutations.execute(archiveNibCmd(nibId));
        if (result.ok) {
          onclose();
        }
      },
    });
  }
</script>

<div data-testid="detail-panel" class="detail-panel" role="complementary" aria-label="Nib detail">
  <div class="detail-header">
    <!-- -ml-2.5 cancels the button's px-2.5 so the ID stays flush with the panel's
         left edge, matching the prior static <span>. aria-label/title include the
         visible ID so the accessible name contains it (WCAG 2.5.3 Label in Name). -->
    <Button
      variant="ghost"
      size="sm"
      class="-ml-2.5 cursor-pointer font-normal text-sm text-muted-foreground"
      data-testid="detail-copy-id"
      title={`Copy nib ID ${nibId}`}
      aria-label={`Copy nib ID ${nibId}`}
      onclick={() => copyToClipboard(nibId)}
    >
      {nibId}
    </Button>
    <Button variant="ghost" size="icon-sm" data-testid="detail-close" onclick={onclose} title="Close" aria-label="Close detail panel">
      <X size={16} />
    </Button>
  </div>

  {#if fetching && !nib}
    <div data-testid="detail-loading" class="detail-loading">Loading...</div>
  {:else if $result.error}
    <div data-testid="detail-not-found" class="detail-loading">Error loading nib</div>
  {:else if nib}
    {#if deleted}
      <div data-testid="detail-deleted-notice" class="detail-deleted-notice">This nib was deleted</div>
    {/if}
    <div class="detail-body" data-testid="detail-body-container" class:nib-detail-highlighted={highlighted}>
      <input
        data-testid="detail-title"
        type="text"
        class="detail-title-input"
        aria-label="Title"
        bind:value={editTitle}
        onblur={handleTitleBlur}
        onkeydown={handleTitleKeydown}
        disabled={deleted}
      />

      <div class="detail-fields">
        <div class="detail-field-group">
          <span class="detail-field-label">Status</span>
          <StatusSelect value={nib.status} onchange={handleStatusChange} testId="detail-status" disabled={deleted} />
        </div>
        <div class="detail-field-group">
          <span class="detail-field-label">Type</span>
          <TypeSelect value={nib.type} onchange={handleTypeChange} testId="detail-type" disabled={deleted} />
        </div>
        <div class="detail-field-group">
          <span class="detail-field-label">Priority</span>
          <PrioritySelect value={nib.priority || ""} onchange={handlePriorityChange} testId="detail-priority" disabled={deleted} />
        </div>
        <div class="detail-field-group">
          <span class="detail-field-label">Estimate</span>
          <EstimateSelect value={nib.estimate || ""} onchange={handleEstimateChange} testId="detail-estimate" disabled={deleted} />
        </div>
      </div>

      <div class="detail-tags-section">
        <span class="detail-label">Tags</span>
        <TagEditor
          tags={nib.tags}
          onadd={handleAddTag}
          onremove={handleRemoveTag}
          chipTestId="detail-tag"
          removeTestId="detail-tag-remove"
          inputTestId="detail-tag-input"
          disabled={deleted}
        />
      </div>

      {#if nib.body}
        <div class="detail-body-section" data-testid="detail-body-section">
          <div class="detail-body-section-header">
            <span class="detail-label">Description</span>
            <Button
              variant="outline"
              size="xs"
              data-testid="detail-body-edit"
              disabled={!onedit || deleted}
              title={onedit ? "Edit in full-screen editor" : "Edit description (coming soon)"}
              onclick={() => onedit?.(nibId)}
            >
              Edit
            </Button>
          </div>
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div
            class="prose-nib"
            data-testid="detail-body-prose"
            onclick={handleProseClick}
          >
            {@html bodyHtml}
          </div>
        </div>
      {/if}

      {#if nib.documents && nib.documents.length > 0}
        <div class="detail-documents-section" data-testid="detail-documents-section">
          <span class="detail-label">Documents</span>
          <ul class="detail-documents-list">
            {#each nib.documents as doc}
              <li class="detail-document-item" data-testid="detail-document">
                <FileText size={14} />
                <span class="detail-document-path" title={doc}>{doc}</span>
              </li>
            {/each}
          </ul>
        </div>
      {/if}

      {#if nib.parent || nib.children?.length > 0 || nib.blockedBy?.length > 0 || nib.blocking?.length > 0 || nib.mentions?.length > 0 || nib.mentionedBy?.length > 0}
        {#key nibId}
        <div class="detail-related-section" data-testid="detail-related-section">
          {#if nib.parent}
            <RelatedNibGroup
              label="Parent"
              items={[{ id: nib.parent.id, title: nib.parent.title, status: nib.parent.status }]}
              onnibselect={onnibselect}
              testId="detail-related-parent"
            />
          {/if}

          {#if nib.children?.length > 0}
            <RelatedNibGroup
              label="Children"
              items={nib.children.map((c) => ({ id: c.id, title: c.title, status: c.status }))}
              onnibselect={onnibselect}
              onaction={onaddchild ? () => onaddchild(nibId, nib.type) : undefined}
              actionLabel="Add child nib"
              testId="detail-related-children"
            />
          {/if}

          {#if nib.blockedBy?.length > 0}
            <RelatedNibGroup
              label="Blocked by"
              items={nib.blockedBy.map((b) => ({ id: b.id, title: b.title, status: b.status }))}
              onnibselect={onnibselect}
              testId="detail-related-blocked-by"
            />
          {/if}

          {#if nib.blocking?.length > 0}
            <RelatedNibGroup
              label="Blocking"
              items={nib.blocking.map((b) => ({ id: b.id, title: b.title, status: b.status }))}
              onnibselect={onnibselect}
              testId="detail-related-blocking"
            />
          {/if}

          {#if nib.mentions?.length > 0}
            <RelatedNibGroup
              label="Mentions"
              items={nib.mentions.map((m) => ({ id: m.id, title: m.title, status: m.status }))}
              onnibselect={onnibselect}
              testId="detail-related-mentions"
            />
          {/if}

          {#if nib.mentionedBy?.length > 0}
            <RelatedNibGroup
              label="Mentioned by"
              items={nib.mentionedBy.map((m) => ({ id: m.id, title: m.title, status: m.status }))}
              onnibselect={onnibselect}
              testId="detail-related-mentioned-by"
            />
          {/if}
        </div>
        {/key}
      {/if}

      <div class="detail-actions-section" data-testid="detail-actions">
        <Button
          variant="ghost"
          size="sm"
          class="text-archive border border-archive-border hover:bg-archive-bg-hover"
          data-testid="detail-archive-button"
          disabled={actionPending || deleted}
          onclick={handleArchiveNib}
        >
          <Archive size={14} />
          Archive
        </Button>
        <Button
          variant="ghost"
          size="sm"
          class="text-delete border border-delete-border hover:bg-delete-bg-hover"
          data-testid="detail-delete-button"
          disabled={actionPending || deleted}
          onclick={handleDeleteNib}
        >
          <Trash2 size={14} />
          Delete
        </Button>
      </div>
    </div>
  {:else}
    <div data-testid="detail-not-found" class="detail-loading">Nib not found</div>
  {/if}
</div>

<style>
  .detail-panel {
    border-left: 1px solid var(--border);
    height: 100%;
    overflow-y: auto;
    padding: 1rem;
    min-width: 300px;
  }

  .detail-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 1rem;
  }

  .detail-loading {
    color: var(--muted-foreground);
    font-size: 0.875rem;
    padding: 1rem 0;
  }

  .detail-body {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .detail-title-input {
    width: 100%;
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--radius-md);
    padding: 0.375rem 0.5rem;
    color: var(--foreground);
    font-size: 1.25rem;
    font-weight: 600;
    outline: none;
    box-sizing: border-box;
  }

  .detail-title-input:hover {
    border-color: var(--border);
  }

  .detail-title-input:focus {
    border-color: var(--ring);
    background-color: var(--accent);
  }

  .detail-fields {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }

  .detail-field-group {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;
  }

  .detail-field-label {
    font-size: 0.75rem;
    color: var(--muted-foreground);
  }

  .detail-label {
    color: var(--muted-foreground);
    white-space: nowrap;
  }

  .detail-tags-section {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .detail-body-section {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .detail-body-section-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .detail-documents-section {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .detail-documents-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .detail-document-item {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    color: var(--link);
    padding: 0.25rem 0.5rem;
    border-radius: var(--radius-md);
    cursor: default;
  }

  .detail-document-item:hover {
    background-color: var(--accent);
  }

  .detail-document-path {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .detail-related-section {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .detail-actions-section {
    display: flex;
    gap: 0.5rem;
    padding-top: 1rem;
    border-top: 1px solid var(--border);
  }

  .detail-body.nib-detail-highlighted {
    animation: nib-detail-highlight-pulse 1s ease-out;
  }

  @keyframes nib-detail-highlight-pulse {
    0% { background-color: oklch(from var(--primary) l c h / 0.25); }
    100% { background-color: transparent; }
  }

  .detail-deleted-notice {
    padding: 0.5rem 0.75rem;
    background-color: var(--destructive);
    color: var(--destructive-foreground);
    border-radius: var(--radius-md);
    font-size: 0.875rem;
    font-weight: 500;
  }

</style>
