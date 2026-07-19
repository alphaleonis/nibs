<script lang="ts">
  import { tick } from "svelte";
  import { X, Plus } from "@lucide/svelte";
  import { TAG_REGEX } from "../markdown";
  import { Button } from "$lib/components/ui/button/index.js";

  interface Props {
    tags: string[];
    /** Available tags to offer as suggestions. The already-applied `tags` are
     *  excluded before display, so the caller can pass the full tag universe. */
    suggestions?: string[];
    onadd: (tag: string) => void | Promise<void>;
    onremove: (tag: string) => void;
    testId?: string;
    chipTestId?: string;
    removeTestId?: string;
    inputTestId?: string;
    /** Testid for the "Add tag" / "+" affordance that reveals the input. */
    addTestId?: string;
    disabled?: boolean;
  }

  let {
    tags,
    suggestions = [],
    onadd,
    onremove,
    testId = "tag-editor",
    chipTestId = "tag-chip",
    removeTestId = "tag-remove",
    inputTestId = "tag-input",
    addTestId = "tag-add",
    disabled = false,
  }: Props = $props();

  // ADO-style tag input: chips are always visible; the free-text input is
  // revealed on demand (an "Add tag" button when empty, a "+" button after
  // chips otherwise) and offers a filtered suggestions dropdown.
  let editing = $state(false); // is the input revealed?
  let newTag: string = $state("");
  let tagError: string | null = $state(null);
  let activeIndex = $state(-1); // highlighted suggestion (-1 = none)
  let inputEl: HTMLInputElement | undefined = $state();
  let blurTimer: ReturnType<typeof setTimeout> | null = null;

  // Suggestions minus already-applied tags, filtered by the typed query
  // (case-insensitive substring). Empty when no available tag matches.
  const filtered = $derived.by(() => {
    const applied = new Set(tags);
    const q = newTag.trim().toLowerCase();
    return suggestions
      .filter((s) => !applied.has(s))
      .filter((s) => q === "" || s.toLowerCase().includes(q));
  });

  async function reveal() {
    if (blurTimer) { clearTimeout(blurTimer); blurTimer = null; }
    tagError = null;
    newTag = "";
    activeIndex = -1;
    editing = true;
    await tick();
    inputEl?.focus();
  }

  function close() {
    if (blurTimer) { clearTimeout(blurTimer); blurTimer = null; }
    editing = false;
    newTag = "";
    tagError = null;
    activeIndex = -1;
  }

  async function commit(candidate: string) {
    const tag = candidate.trim();
    if (!tag) return;
    if (!TAG_REGEX.test(tag)) {
      tagError = "Tags must be lowercase, start with a letter, and use hyphens as separators";
      return;
    }
    if (tags.includes(tag)) {
      tagError = "Tag already exists";
      return;
    }
    tagError = null;
    try {
      await onadd(tag);
      // Keep the input open so several tags can be added in a row (ADO-style).
      newTag = "";
      activeIndex = -1;
      await tick();
      inputEl?.focus();
    } catch {
      tagError = "Failed to add tag";
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === "ArrowDown") {
      if (filtered.length === 0) return;
      e.preventDefault();
      activeIndex = (activeIndex + 1) % filtered.length;
    } else if (e.key === "ArrowUp") {
      if (filtered.length === 0) return;
      e.preventDefault();
      activeIndex = activeIndex <= 0 ? filtered.length - 1 : activeIndex - 1;
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (activeIndex >= 0 && activeIndex < filtered.length) {
        commit(filtered[activeIndex]);
      } else if (newTag.trim()) {
        commit(newTag);
      }
    } else if (e.key === "Escape") {
      e.preventDefault();
      close();
    }
  }

  function handleInput() {
    tagError = null;
    activeIndex = -1;
  }

  function handleBlur() {
    // Defer so a suggestion click (which fires after blur) is not cut off.
    // Suggestion buttons also preventDefault on mousedown to keep focus, so this
    // primarily handles clicking away from the editor entirely.
    if (blurTimer) clearTimeout(blurTimer);
    blurTimer = setTimeout(() => {
      blurTimer = null;
      close();
    }, 150);
  }
</script>

<div data-testid={testId} class="tag-editor">
  <div class="tag-row">
    {#each tags as tag}
      <span class="tag-chip" data-testid={chipTestId}>
        {tag}
        {#if !disabled}
          <!-- Raw button: tiny circular remove control nested inside a tag pill,
               smaller than the Button primitive's minimum icon size. -->
          <button
            class="tag-remove"
            data-testid={removeTestId}
            onclick={() => onremove(tag)}
            aria-label="Remove tag {tag}"
          >
            <X size={12} />
          </button>
        {/if}
      </span>
    {/each}

    {#if !disabled}
      {#if editing}
        <div class="tag-input-wrap">
          <input
            bind:this={inputEl}
            data-testid={inputTestId}
            type="text"
            class="tag-input"
            placeholder="Add tag..."
            bind:value={newTag}
            oninput={handleInput}
            onkeydown={handleKeydown}
            onblur={handleBlur}
          />
          {#if filtered.length > 0}
            <ul class="tag-suggestions" data-testid="tag-suggestions">
              {#each filtered as suggestion, i}
                <li>
                  <!-- Raw button: an autocomplete option row. mousedown is
                       prevented so clicking it doesn't blur (and close) the
                       input before the click commits. -->
                  <button
                    type="button"
                    class="tag-suggestion"
                    class:active={i === activeIndex}
                    data-testid="tag-suggestion"
                    onmousedown={(e) => e.preventDefault()}
                    onclick={() => commit(suggestion)}
                  >
                    {suggestion}
                  </button>
                </li>
              {/each}
            </ul>
          {/if}
        </div>
      {:else if tags.length === 0}
        <Button
          variant="outline"
          size="sm"
          class="text-sm"
          data-testid={addTestId}
          aria-label="Add tag"
          onclick={reveal}
        >
          <Plus size={14} />
          Add tag
        </Button>
      {:else}
        <Button
          variant="ghost"
          size="icon-sm"
          data-testid={addTestId}
          aria-label="Add tag"
          title="Add tag"
          onclick={reveal}
        >
          <Plus size={14} />
        </Button>
      {/if}
    {/if}
  </div>
  {#if tagError}
    <div class="tag-error">{tagError}</div>
  {/if}
</div>

<style>
  .tag-editor {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }

  .tag-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.375rem;
  }

  .tag-chip {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    background-color: var(--tag-bg);
    color: var(--tag-text);
    border: 1px solid var(--tag-border);
    border-radius: 9999px;
    padding: 0.125rem 0.5rem;
    font-size: var(--text-body-size);
  }

  .tag-remove {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: none;
    color: var(--tag-text);
    cursor: pointer;
    padding: 0;
    border-radius: 50%;
  }

  .tag-remove:hover {
    color: var(--foreground);
  }

  /* Width-constrained input (ADO-style), not full-width. */
  .tag-input-wrap {
    position: relative;
    width: 200px;
    max-width: 100%;
  }

  .tag-input {
    width: 100%;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 0.2rem 0.5rem;
    color: var(--foreground);
    font-size: var(--text-body-size);
    outline: none;
  }

  .tag-input:focus {
    border-color: var(--ring);
  }

  .tag-input::placeholder {
    color: var(--muted-foreground);
  }

  .tag-suggestions {
    position: absolute;
    top: calc(100% + 0.2rem);
    left: 0;
    z-index: var(--z-dropdown);
    min-width: 100%;
    max-height: 12rem;
    overflow-y: auto;
    margin: 0;
    padding: 0.2rem;
    list-style: none;
    background: var(--popover);
    color: var(--popover-foreground);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: 0 6px 20px oklch(0 0 0 / 0.25);
  }

  .tag-suggestion {
    display: block;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    border-radius: var(--radius-sm);
    padding: 0.28rem 0.5rem;
    color: var(--popover-foreground);
    font-size: var(--text-body-size);
    cursor: pointer;
  }

  .tag-suggestion:hover,
  .tag-suggestion.active {
    background: var(--accent);
    color: var(--accent-foreground);
  }

  .tag-error {
    font-size: var(--text-caption-size);
    color: var(--error-text);
  }
</style>
