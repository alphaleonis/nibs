import { toast } from "svelte-sonner";

/**
 * Copy `text` to the clipboard and toast the outcome. Never rejects.
 *
 * Pass `label` for long text (a nib body): the toast names it instead of
 * quoting the text. Test the label for `undefined`, not truthiness, so an empty
 * label never quotes unbounded text.
 */
export async function copyToClipboard(text: string, label?: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(label !== undefined ? `Copied ${label} to clipboard` : `Copied "${text}" to clipboard`);
  } catch {
    toast.error("Failed to copy to clipboard");
  }
}
