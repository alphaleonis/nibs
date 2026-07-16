import { describe, it, expect, vi, afterEach } from "vitest";
import { flushSync } from "svelte";
import { SelectionState } from "../selection.svelte";
import { useKeyboardShortcuts } from "./useKeyboardShortcuts.svelte";
import type { HistoryNav } from "./useHistoryNav.svelte";
import type { ActiveView } from "./useActiveView.svelte";
import type { ConfirmDialogState } from "./useConfirmDialog.svelte";
import type { MutationStore } from "../mutations/store.svelte";

// The composable binds global shortcuts on `window` via tinykeys inside an
// $effect. It reads nothing about the event's target — the input-focus gate is
// `isInputFocused()`, which inspects `document.activeElement`. So exercising it
// means: mount it in an effect root, put real DOM focus somewhere, and dispatch
// a real keydown that bubbles up to the window listener.

/** A fully-stubbed dependency set with observable spies, plus the real
 *  SelectionState (so target resolution behaves as in production). Override any
 *  piece for a specific scenario. Reusable across the shortcut behaviors that
 *  land in this file (create `n`, edit `e`, delete Delete/Backspace, Escape). */
function makeHarness(
  overrides: {
    selection?: SelectionState;
    isOpen?: boolean;
    presentation?: string;
    getContextMenuNibId?: () => string | null;
  } = {},
) {
  const selection = overrides.selection ?? new SelectionState();

  const nav = {
    navigateToNib: vi.fn(),
    closePanel: vi.fn(),
    replaceClosed: vi.fn(),
    handlePopState: vi.fn(),
    syncFromUrl: vi.fn(),
  } as unknown as HistoryNav;

  const startCreate = vi.fn(async () => {});
  const open = vi.fn(async () => {});
  const requestClose = vi.fn(async () => {});
  const view = {
    isOpen: overrides.isOpen ?? false,
    presentation: overrides.presentation ?? "docked",
    startCreate,
    open,
    requestClose,
  } as unknown as ActiveView;

  const showConfirm = vi.fn();
  const close = vi.fn();
  const confirmDialog = {
    open: false,
    title: "",
    message: "",
    label: "",
    variant: "danger",
    action: null,
    saveLabel: null,
    saveAction: null,
    showConfirm,
    close,
  } as unknown as ConfirmDialogState;

  const execute = vi.fn(async () => ({ ok: true }));
  const mutations = { execute } as unknown as MutationStore;

  const getContextMenuNibId = overrides.getContextMenuNibId ?? (() => null);

  return {
    selection,
    nav,
    view,
    confirmDialog,
    showConfirm,
    mutations,
    execute,
    startCreate,
    open,
    requestClose,
    getContextMenuNibId,
  };
}

const disposers: Array<() => void> = [];

/** Register the shortcuts inside a fresh effect root and flush so the $effect
 *  runs and the window listener is live. The disposer (which unbinds the
 *  listener) is tracked and torn down in afterEach so listeners never leak
 *  across tests — an accumulated listener would fire a handler twice. */
function mount(h: ReturnType<typeof makeHarness>): void {
  const dispose = $effect.root(() => {
    useKeyboardShortcuts({
      selection: h.selection,
      nav: h.nav,
      view: h.view,
      confirmDialog: h.confirmDialog,
      mutations: h.mutations,
      getContextMenuNibId: h.getContextMenuNibId,
    });
  });
  flushSync();
  disposers.push(dispose);
}

/** Append a focusable element, focus it, and assert focus actually stuck — a
 *  no-op focus (jsdom refusing an element) would make the whole test vacuous. */
function focusEl<T extends HTMLElement>(el: T): T {
  document.body.appendChild(el);
  el.focus();
  expect(document.activeElement).toBe(el);
  return el;
}

function readonlyInput(): HTMLInputElement {
  const input = document.createElement("input");
  input.type = "text";
  input.readOnly = true;
  input.value = "tnib-abcd (deleted)";
  return input;
}

/** Dispatch a real keydown from `from` (bubbles to the window listener). */
function press(key: string, from: HTMLElement): void {
  from.dispatchEvent(
    new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true }),
  );
}

afterEach(() => {
  while (disposers.length) disposers.pop()!();
  document.body.innerHTML = "";
  // Drop focus back to <body> so no stray activeElement bleeds into the next test.
  (document.activeElement as HTMLElement | null)?.blur?.();
});

describe("useKeyboardShortcuts · readonly input gate", () => {
  // The destructive shortcuts. `isInputFocused()` gates all of them, but Delete
  // and Backspace are the dangerous pair the nib pins: a caret in a readonly
  // input (the `gone`-recovery title) must not fire a nib deletion.
  const destructive = ["Delete", "Backspace"] as const;

  describe.each(destructive)("%s", key => {
    it("does NOT fire the delete confirm while a readonly input is focused", () => {
      const h = makeHarness();
      h.selection.focus("tnib-abcd"); // give the action a valid target
      mount(h);

      const input = focusEl(readonlyInput());
      press(key, input);

      // The gate suppressed it: no confirm dialog, no deletion path entered.
      expect(h.showConfirm).not.toHaveBeenCalled();
    });

    // Positive control: identical harness, identical target, identical key —
    // the ONLY difference is that focus is on a non-input element. If this
    // fires but the readonly case above does not, `isInputFocused()` is proven
    // to be what suppressed it (not an unwired handler, wrong key, or absent
    // target). Without this control the negative test proves nothing.
    it("DOES fire the delete confirm when a non-input element is focused", () => {
      const h = makeHarness();
      h.selection.focus("tnib-abcd");
      mount(h);

      const button = focusEl(document.createElement("button"));
      press(key, button);

      expect(h.showConfirm).toHaveBeenCalledTimes(1);
    });
  });
});
