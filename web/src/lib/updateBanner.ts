// Per-version dismissal of the "update available" banner. Kept separate from
// filter preferences (storage.ts) because it is an unrelated concern with its
// own lifecycle: dismissing v0.6.0 must not suppress a later v0.7.0 banner.

export const UPDATE_DISMISS_KEY = "nibs-update-dismissed-version";

// isUpdateDismissed reports whether the banner for this exact version was
// already dismissed. Never throws (private-mode / disabled storage → false).
export function isUpdateDismissed(version: string): boolean {
  if (!version) return false;
  try {
    return localStorage.getItem(UPDATE_DISMISS_KEY) === version;
  } catch {
    return false;
  }
}

// dismissUpdate records that the banner for this version was dismissed, so it
// stays hidden until a newer version ships. Best-effort; storage errors are
// swallowed (the banner simply reappears next load).
export function dismissUpdate(version: string): void {
  if (!version) return;
  try {
    localStorage.setItem(UPDATE_DISMISS_KEY, version);
  } catch {
    // ignore — dismissal is a convenience, not correctness-critical
  }
}
