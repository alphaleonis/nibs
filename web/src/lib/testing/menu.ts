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
 * The sub-trigger is focused first: bits-ui routes the keystroke through the
 * focused element, and closes a submenu whose trigger has lost focus. The menu
 * takes focus back for itself when it opens, and can do so after the trigger
 * has been focused, so the keystroke is retried until the submenu is open.
 *
 * The hover path is real and stays covered — against a real browser, in
 * `web/e2e/context-menu.test.ts`.
 */
export async function openSubmenu(user: User, trigger: HTMLElement): Promise<void> {
  await waitFor(async () => {
    trigger.focus();
    await user.keyboard("{ArrowRight}");
    if (trigger.getAttribute("data-state") !== "open") {
      throw new Error("submenu did not open");
    }
    // Wait for the open to SETTLE, not merely to have happened. bits-ui
    // announces an opening menu to the others so they can close themselves, and
    // that announcement is deferred — it can still be in flight at the moment
    // the trigger first reads `open`. The next `await` in the calling test is
    // then where it lands, which closes this submenu and detaches the element
    // the test just queried; the click that follows dispatches into a node no
    // longer in the document and reaches no handler. Yielding to the macrotask
    // queue here gives a pending announcement its chance to land while we can
    // still see it, so the retry re-opens instead of the caller failing.
    await new Promise(resolve => setTimeout(resolve, 0));
    if (trigger.getAttribute("data-state") !== "open") {
      throw new Error("submenu closed again after opening");
    }
  });
}
