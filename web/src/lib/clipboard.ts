import { toast } from "svelte-sonner";

/**
 * Copy `text` to the system clipboard and surface a toast for the outcome.
 *
 * Success shows a confirmation toast; failure (e.g. clipboard permission
 * denied, or an insecure context where `navigator.clipboard` is unavailable)
 * shows an error toast. The promise always resolves — callers do not need to
 * handle rejection.
 *
 * `label` names what was copied instead of quoting it, for text that is too
 * long to sit in a toast (a nib body). Omit it for short values like an id,
 * where quoting the text is the more useful confirmation. The branch turns on
 * the label's PRESENCE, not its truthiness: quoting the text is the unbounded
 * branch, so an empty label must not fall back to it.
 */
export async function copyToClipboard(text: string, label?: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(label !== undefined ? `Copied ${label} to clipboard` : `Copied "${text}" to clipboard`);
  } catch {
    toast.error("Failed to copy to clipboard");
  }
}
