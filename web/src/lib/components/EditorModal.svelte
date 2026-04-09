<script lang="ts">
  import { toast } from "svelte-sonner";
  import { renderMarkdown } from "../markdown";
  import { getBodyTemplate } from "../bodyTemplates";
  import { NIB_CHANGED_SUBSCRIPTION } from "../queries";
  import * as Dialog from "$lib/components/ui/dialog/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import StatusSelect from "./StatusSelect.svelte";
  import TypeSelect from "./TypeSelect.svelte";
  import PrioritySelect from "./PrioritySelect.svelte";
  import EstimateSelect from "./EstimateSelect.svelte";
  import TagEditor from "./TagEditor.svelte";
  import MarkdownEditor from "./MarkdownEditor.svelte";
  import ConfirmDialog from "./ConfirmDialog.svelte";
  import { X } from "@lucide/svelte";
  import { getContextClient, subscriptionStore } from "@urql/svelte";
  import { getMutationStore } from "$lib/mutations";
  import { createNib as createNibCmd, updateNib as updateNibCmd } from "$lib/mutations/commands";
  import type { CreateNibInput, UpdateNibInput } from "$lib/mutations/types";

  export interface NibData {
    title: string;
    status: string;
    type: string;
    priority: string;
    estimate: string;
    tags: string[];
    body: string;
    etag: string;
  }

  interface Props {
    open: boolean;
    mode: "create" | "edit";
    nibId?: string;
    nibData?: NibData;
    defaultType?: string;
    defaultParent?: string;
    onclose: () => void;
    onsave?: (nibId: string) => void;
  }

  let {
    open,
    mode,
    nibId = undefined,
    nibData = undefined,
    defaultType = "task",
    defaultParent = undefined,
    onclose,
    onsave,
  }: Props = $props();

  const mutations = getMutationStore();
  const client = getContextClient();

  // Conflict detection state
  let externalNibData: NibData | null = $state(null);
  let hasExternalChange = $state(false);
  let showOverwriteConfirm = $state(false);

  // Subscribe to changes for this nib (edit mode only; create mode gets a no-op id)
  const subscription = $derived(
    subscriptionStore({
      client,
      query: NIB_CHANGED_SUBSCRIPTION,
      variables: { id: nibId ?? "__none__" },
      pause: mode !== "edit" || !nibId,
    })
  );

  // Log subscription errors (consistent with TreeTable/DetailPanel pattern)
  $effect(() => {
    if ($subscription.error) {
      console.warn("Editor subscription error:", $subscription.error);
    }
  });

  // Detect external changes from subscription events
  let lastSubData: unknown = null;
  $effect(() => {
    if (mode !== "edit" || !nibId) return;

    const data = $subscription.data;
    if (!data || data === lastSubData) return;
    lastSubData = data;

    const event = data.nibChanged;
    if (!event || event.type !== "updated" || !event.nib) return;

    // Self-echo prevention: if the event's etag matches our local etag, it's our own save
    if (event.nib.etag === etag) return;

    // External change detected
    externalNibData = {
      title: event.nib.title,
      status: event.nib.status,
      type: event.nib.type,
      priority: event.nib.priority ?? "",
      estimate: event.nib.estimate ?? "",
      tags: event.nib.tags ?? [],
      body: event.nib.body ?? "",
      etag: event.nib.etag ?? "",
    };
    hasExternalChange = true;
  });

  // Local buffered state
  let title = $state("");
  let status = $state("draft");
  let type = $state("task");
  let priority = $state("");
  let estimate = $state("");
  let tags = $state<string[]>([]);
  let body = $state("");
  let etag = $state("");
  let saving = $state(false);
  let bodyModified = $state(false);
  let showUnsavedConfirm = $state(false);
  let activeTab: "write" | "preview" = $state("write");
  let titleInputEl: HTMLInputElement | undefined = $state(undefined);
  // Incrementing bodyKey forces {#key} to destroy and recreate MarkdownEditor
  // with the new template content (component only accepts initialValue at creation)
  let bodyKey = $state(0);

  // Track the template that was used to pre-fill body
  let lastTemplateBody = $state("");

  // Initialize / reset state when open changes
  $effect(() => {
    if (open) {
      if (mode === "edit" && nibData) {
        title = nibData.title;
        status = nibData.status;
        type = nibData.type;
        priority = nibData.priority;
        estimate = nibData.estimate;
        tags = [...nibData.tags];
        body = nibData.body;
        etag = nibData.etag;
        lastTemplateBody = "";
        bodyModified = true; // Edit mode body is always "modified" (don't auto-replace)
      } else {
        // Create mode
        // Use a local variable for the default type to avoid making `type` a
        // tracked dependency of this effect. Reading `type` here would cause
        // the effect to re-fire whenever handleTypeChange updates it, resetting
        // all form fields.
        const defaultT = defaultType || "task";
        title = "";
        status = "draft";
        type = defaultT;
        priority = "";
        estimate = "";
        tags = [];
        const tmpl = getBodyTemplate(defaultT);
        body = tmpl;
        lastTemplateBody = tmpl;
        etag = "";
        bodyModified = false;
      }
      saving = false;
      showUnsavedConfirm = false;
      showOverwriteConfirm = false;
      hasExternalChange = false;
      externalNibData = null;
      lastSubData = null;
      activeTab = "write";

      // Auto-focus the title input after render (create mode only)
      if (mode === "create") {
        queueMicrotask(() => { titleInputEl?.focus(); });
      }
    }
  });

  // When type changes in create mode, update body template if body hasn't been modified
  function handleTypeChange(newType: string) {
    type = newType;
    if (mode === "create" && !bodyModified) {
      const tmpl = getBodyTemplate(newType);
      body = tmpl;
      lastTemplateBody = tmpl;
      bodyKey++;
    }
  }

  // Rendered markdown preview
  let previewHtml = $derived(renderMarkdown(body));

  // Check if there are unsaved changes
  let hasChanges = $derived.by(() => {
    if (mode === "create") {
      return title.trim().length > 0 || (body !== lastTemplateBody);
    }
    if (!nibData) return false;
    return (
      title !== nibData.title ||
      status !== nibData.status ||
      type !== nibData.type ||
      priority !== nibData.priority ||
      estimate !== nibData.estimate ||
      body !== nibData.body ||
      JSON.stringify(tags) !== JSON.stringify(nibData.tags)
    );
  });

  function handleAddTag(tag: string) {
    tags = [...tags, tag];
  }

  function handleRemoveTag(tag: string) {
    tags = tags.filter(t => t !== tag);
  }

  async function handleSave() {
    if (saving) return;

    // If there's an external change, show overwrite confirmation first
    if (hasExternalChange) {
      showOverwriteConfirm = true;
      return;
    }

    await doSave();
  }

  async function doSave() {
    if (saving) return;
    const trimmedTitle = title.trim();
    if (!trimmedTitle) {
      toast.error("Title is required");
      return;
    }

    saving = true;

    if (mode === "create") {
      const input: CreateNibInput = {
        title: trimmedTitle,
        type,
        status,
        ...(priority ? { priority } : {}),
        ...(estimate ? { estimate } : {}),
        ...(tags.length > 0 ? { tags } : {}),
        ...(body ? { body } : {}),
        ...(defaultParent ? { parent: defaultParent } : {}),
      };

      const result = await mutations.execute(createNibCmd(input));
      saving = false;

      if (!result.ok) return;

      const newId = result.data?.createNib?.id;
      toast.success(`Created ${newId}`);
      onsave?.(newId);
      onclose();
    } else {
      // Edit mode
      if (!nibId) {
        saving = false;
        return;
      }
      const input: UpdateNibInput = {
        title: trimmedTitle,
        status,
        type,
        priority: priority || null,
        estimate: estimate || null,
        body,
      };

      // Handle tag changes
      if (nibData) {
        const addedTags = tags.filter(t => !nibData.tags.includes(t));
        const removedTags = nibData.tags.filter(t => !tags.includes(t));
        if (addedTags.length > 0) input.addTags = addedTags;
        if (removedTags.length > 0) input.removeTags = removedTags;
      }

      const result = await mutations.execute(updateNibCmd(nibId, input, etag));
      saving = false;

      if (!result.ok) return;

      // Update local etag so self-echo filter works if component stays open
      const newEtag = result.data?.updateNib?.etag;
      if (newEtag) etag = newEtag;

      toast.success(`Updated ${nibId}`);
      onsave?.(nibId);
      onclose();
    }
  }

  function handleClose() {
    if (hasChanges) {
      showUnsavedConfirm = true;
    } else {
      onclose();
    }
  }

  function handleDiscardAndClose() {
    showUnsavedConfirm = false;
    onclose();
  }

  function applyNibData(data: NibData) {
    title = data.title;
    status = data.status;
    type = data.type;
    priority = data.priority;
    estimate = data.estimate;
    tags = [...data.tags];
    body = data.body;
    etag = data.etag;
    hasExternalChange = false;
    externalNibData = null;
    bodyKey++;
  }

  function handleRevert() {
    if (!nibData) return;
    applyNibData(nibData);
  }

  function handleReload() {
    if (!externalNibData) return;
    applyNibData(externalNibData);
  }

  function handleDialogKeydown(e: KeyboardEvent) {
    if (e.key === "s" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      handleSave();
    }
  }
</script>

{#if open}
  <Dialog.Root open={true} onOpenChange={(isOpen) => { if (!isOpen) handleClose(); }}>
    <Dialog.Content
      data-testid="editor-modal"
      class="inset-4 sm:inset-8 translate-x-0 translate-y-0 max-w-none sm:max-w-none p-0 h-auto w-auto"
      showCloseButton={false}
      onkeydown={handleDialogKeydown}
    >
      <div class="editor-modal-layout">
        <!-- Header -->
        <div class="editor-header">
          <input
            bind:this={titleInputEl}
            data-testid="editor-title"
            type="text"
            class="editor-title-input"
            placeholder="Nib title..."
            bind:value={title}
          />
          <button
            data-testid="editor-close"
            class="editor-close-btn"
            onclick={handleClose}
            title="Close"
            aria-label="Close editor"
          >
            <X size={20} />
          </button>
        </div>

        <!-- Conflict banner -->
        {#if hasExternalChange}
          <div class="editor-conflict-banner" data-testid="editor-conflict-banner">
            <span>This nib was modified externally.</span>
            <div class="editor-conflict-actions">
              <Button variant="outline" size="sm" data-testid="editor-conflict-revert" onclick={handleRevert}>
                Revert
              </Button>
              <Button variant="outline" size="sm" data-testid="editor-conflict-reload" onclick={handleReload}>
                Reload
              </Button>
            </div>
          </div>
        {/if}

        <!-- Metadata row -->
        <div class="editor-metadata">
          <div class="editor-meta-field">
            <span class="editor-meta-label">Status</span>
            <StatusSelect value={status} onchange={(v) => { status = v; }} testId="editor-status" />
          </div>

          <div class="editor-meta-field">
            <span class="editor-meta-label">Type</span>
            <TypeSelect value={type} onchange={handleTypeChange} testId="editor-type" />
          </div>

          <div class="editor-meta-field">
            <span class="editor-meta-label">Priority</span>
            <PrioritySelect value={priority} onchange={(v) => { priority = v; }} testId="editor-priority" />
          </div>

          <div class="editor-meta-field">
            <span class="editor-meta-label">Estimate</span>
            <EstimateSelect value={estimate} onchange={(v) => { estimate = v; }} testId="editor-estimate" />
          </div>

          <div class="editor-tags-field">
            <span class="editor-meta-label">Tags</span>
            <TagEditor
              {tags}
              onadd={handleAddTag}
              onremove={handleRemoveTag}
              inputTestId="editor-tag-input"
            />
          </div>
        </div>

        <!-- Main editor area -->
        <div class="editor-main">
          <!-- Tab bar for narrow screens -->
          <div class="editor-tab-bar">
            <button
              class="editor-tab"
              class:active={activeTab === "write"}
              data-testid="editor-tab-write"
              onclick={() => { activeTab = "write"; }}
            >
              Write
            </button>
            <button
              class="editor-tab"
              class:active={activeTab === "preview"}
              data-testid="editor-tab-preview"
              onclick={() => { activeTab = "preview"; }}
            >
              Preview
            </button>
          </div>

          <div class="editor-panels">
            <!-- CodeMirror editor -->
            <div
              class="editor-cm-container"
              class:hidden-mobile={activeTab !== "write"}
              data-testid="editor-cm-container"
            >
              {#key bodyKey}
                <MarkdownEditor
                  initialValue={body}
                  onchange={(v) => { body = v; bodyModified = true; }}
                  onsave={handleSave}
                />
              {/key}
            </div>

            <!-- Markdown preview -->
            <div
              class="editor-preview-container"
              class:hidden-mobile={activeTab !== "preview"}
              data-testid="editor-preview"
            >
              <div class="prose-nib">
                {#if previewHtml}
                  {@html previewHtml}
                {:else}
                  <span class="editor-preview-empty">Preview will appear here...</span>
                {/if}
              </div>
            </div>
          </div>
        </div>

        <!-- Footer -->
        <div class="editor-footer">
          <Button
            variant="ghost"
            size="sm"
            data-testid="editor-cancel"
            onclick={handleClose}
          >
            Cancel
          </Button>
          <Button
            size="sm"
            data-testid="editor-save"
            disabled={saving}
            onclick={handleSave}
          >
            {saving ? "Saving..." : mode === "create" ? "Create" : "Save"}
          </Button>
        </div>
      </div>
    </Dialog.Content>
  </Dialog.Root>
{/if}

<ConfirmDialog
  open={showUnsavedConfirm}
  title="Unsaved changes"
  message="You have unsaved changes. What would you like to do?"
  confirmLabel="Discard"
  variant="warning"
  onconfirm={handleDiscardAndClose}
  oncancel={() => { showUnsavedConfirm = false; }}
/>

<ConfirmDialog
  open={showOverwriteConfirm}
  title="Overwrite external changes?"
  message="This nib was modified externally. Saving will overwrite those changes."
  confirmLabel="Overwrite"
  variant="warning"
  testId="editor-overwrite-confirm"
  onconfirm={() => {
    showOverwriteConfirm = false;
    hasExternalChange = false;
    doSave();
  }}
  oncancel={() => { showOverwriteConfirm = false; }}
/>

<style>
  .editor-modal-layout {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
  }

  .editor-header {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--border);
  }

  .editor-title-input {
    flex: 1;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 0.375rem;
    padding: 0.375rem 0.5rem;
    color: var(--foreground);
    font-size: 1.125rem;
    font-weight: 600;
    outline: none;
  }

  .editor-title-input:hover {
    border-color: var(--border);
  }

  .editor-title-input:focus {
    border-color: var(--ring);
    background-color: var(--accent);
  }

  .editor-title-input::placeholder {
    color: var(--muted-foreground);
  }

  .editor-close-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0.375rem;
    color: var(--muted-foreground);
    background: none;
    border: none;
    cursor: pointer;
    border-radius: 0.375rem;
  }

  .editor-close-btn:hover {
    color: var(--foreground);
    background-color: var(--accent);
  }

  .editor-metadata {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--border);
    align-items: flex-start;
  }

  .editor-meta-field {
    display: flex;
    align-items: center;
    gap: 0.375rem;
  }

  .editor-meta-label {
    font-size: 0.75rem;
    color: var(--muted-foreground);
    white-space: nowrap;
  }

  .editor-tags-field {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    flex-wrap: wrap;
  }

  .editor-main {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .editor-tab-bar {
    display: flex;
    gap: 0;
    border-bottom: 1px solid var(--border);
    padding: 0 1rem;
  }

  @media (min-width: 768px) {
    .editor-tab-bar {
      display: none;
    }
  }

  .editor-tab {
    padding: 0.5rem 1rem;
    font-size: 0.8125rem;
    color: var(--muted-foreground);
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    cursor: pointer;
  }

  .editor-tab.active {
    color: var(--foreground);
    border-bottom-color: var(--primary);
  }

  .editor-panels {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: 1px;
    padding: 0.75rem;
    overflow: hidden;
  }

  .editor-cm-container {
    flex: 1;
    min-width: 0;
    overflow: auto;
    border: 1px solid var(--border);
    border-radius: 0.5rem;
  }

  .editor-preview-container {
    flex: 1;
    min-width: 0;
    overflow: auto;
    padding: 1rem;
    border: 1px solid var(--border);
    border-radius: 0.5rem;
  }

  @media (max-width: 767px) {
    .editor-cm-container,
    .editor-preview-container {
      flex: none;
      width: 100%;
    }

    .hidden-mobile {
      display: none !important;
    }
  }

  .editor-preview-empty {
    color: var(--muted-foreground);
    font-style: italic;
  }

  .editor-footer {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    padding: 0.75rem 1rem;
    border-top: 1px solid var(--border);
  }

  .editor-conflict-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.5rem 1rem;
    background-color: var(--warning);
    color: var(--warning-foreground, white);
    font-size: 0.8125rem;
    font-weight: 500;
  }

  .editor-conflict-actions {
    display: flex;
    gap: 0.375rem;
  }
</style>
