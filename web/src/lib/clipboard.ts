import { toast } from "svelte-sonner";

/**
 * Copy `text` to the system clipboard and surface a toast for the outcome.
 *
 * Success shows a confirmation toast; failure (e.g. clipboard permission
 * denied, or an insecure context where `navigator.clipboard` is unavailable)
 * shows an error toast. The promise always resolves — callers do not need to
 * handle rejection.
 */
export async function copyToClipboard(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(`Copied "${text}" to clipboard`);
  } catch {
    toast.error("Failed to copy to clipboard");
  }
}
