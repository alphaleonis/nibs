import { waitFor } from "@testing-library/svelte";
import type { userEvent } from "@testing-library/user-event";

type User = ReturnType<typeof userEvent.setup>;

/**
 * Open a bits-ui submenu from its sub-trigger, in jsdom.
 *
 * Keyboard, not pointer. bits-ui decides whether a submenu stays open by
 * measuring the pointer against `getBoundingClientRect()` corridors and
 * `document.elementFromPoint()`. jsdom reports every rect as 0x0 at the origin
 * and does no hit-testing, so a pointer event near the submenu can convince
 * that tracker the pointer has left, and it unmounts the submenu — frequently
 * between the query that finds an item and the click on it, which then
 * dispatches no events at all. ArrowRight is one of bits-ui's SUB_OPEN_KEYS and
 * opens the same submenu with no geometry involved.
 *
 * Wait for the menu to take focus before touching the trigger. bits-ui moves
 * focus into an opened menu on an animation frame; a frame that lands after the
 * submenu has opened takes focus out of it, and the submenu closes under the
 * test's next click (nibs-7ui1).
 *
 * The keystroke goes to the focused element, so the sub-trigger is focused
 * first, and the keystroke is retried until the submenu is open.
 *
 * The hover path is real and stays covered — against a real browser, in
 * `web/e2e/context-menu.test.ts`.
 */
export async function openSubmenu(user: User, trigger: HTMLElement): Promise<void> {
  const menu = trigger.closest('[role="menu"]');
  if (!menu) {
    throw new Error("sub-trigger is not inside a menu");
  }
  await waitFor(() => {
    if (!menu.contains(document.activeElement)) {
      throw new Error("menu has not taken focus");
    }
  });
  await waitFor(async () => {
    trigger.focus();
    await user.keyboard("{ArrowRight}");
    if (trigger.getAttribute("data-state") !== "open") {
      throw new Error("submenu did not open");
    }
    // A submenu can close again just after opening. Yield one macrotask and
    // check again, so the retry re-opens it instead of the caller failing.
    await new Promise(resolve => setTimeout(resolve, 0));
    if (trigger.getAttribute("data-state") !== "open") {
      throw new Error("submenu closed again after opening");
    }
  });
}
