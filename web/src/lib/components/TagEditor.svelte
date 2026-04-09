<script lang="ts">
  import { X } from "@lucide/svelte";
  import { TAG_REGEX } from "../markdown";

  interface Props {
    tags: string[];
    onadd: (tag: string) => void | Promise<void>;
    onremove: (tag: string) => void;
    testId?: string;
    chipTestId?: string;
    removeTestId?: string;
    inputTestId?: string;
    disabled?: boolean;
  }

  let { tags, onadd, onremove, testId = "tag-editor", chipTestId = "tag-chip", removeTestId = "tag-remove", inputTestId = "tag-input", disabled = false }: Props = $props();

  let newTag: string = $state("");
  let tagError: string | null = $state(null);

  async function handleKeydown(e: KeyboardEvent) {
    if (e.key !== "Enter" || !newTag.trim()) return;
    e.preventDefault();
    const tag = newTag.trim();
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
      newTag = "";
    } catch {
      tagError = "Failed to add tag";
    }
  }

  function handleInput() {
    tagError = null;
  }
</script>

<div data-testid={testId} class="tag-editor">
  <div class="tag-chips">
    {#each tags as tag}
      <span class="tag-chip" data-testid={chipTestId}>
        {tag}
        {#if !disabled}
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
  </div>
  <input
    data-testid={inputTestId}
    type="text"
    class="tag-input"
    placeholder="Add tag..."
    bind:value={newTag}
    oninput={handleInput}
    onkeydown={handleKeydown}
    {disabled}
  />
  {#if tagError}
    <div class="tag-error">{tagError}</div>
  {/if}
</div>

<style>
  .tag-editor {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .tag-chips {
    display: flex;
    flex-wrap: wrap;
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
    font-size: 0.75rem;
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

  .tag-input {
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 0.375rem;
    padding: 0.25rem 0.5rem;
    color: var(--foreground);
    font-size: 0.8125rem;
    outline: none;
  }

  .tag-input:focus {
    border-color: var(--ring);
  }

  .tag-input::placeholder {
    color: var(--muted-foreground);
  }

  .tag-error {
    font-size: 0.75rem;
    color: var(--error-text);
  }
</style>
