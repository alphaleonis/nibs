// Per-version dismissal of the "update available" banner: dismissing one
// version must not hide the banner for a later one.

export const UPDATE_DISMISS_KEY = "nibs-update-dismissed-version";

// isUpdateDismissed reports whether this exact version was dismissed. Returns
// false when storage is unavailable.
export function isUpdateDismissed(version: string): boolean {
  if (!version) return false;
  try {
    return localStorage.getItem(UPDATE_DISMISS_KEY) === version;
  } catch {
    return false;
  }
}

// dismissUpdate records a dismissal. Best-effort: on a storage error the
// banner reappears next load.
export function dismissUpdate(version: string): void {
  if (!version) return;
  try {
    localStorage.setItem(UPDATE_DISMISS_KEY, version);
  } catch {
    // Dismissal is a convenience.
  }
}
