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
    confirmOpen?: boolean;
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
  const dismiss = vi.fn();
  const confirmDialog = {
    open: overrides.confirmOpen ?? false,
    title: "",
    message: "",
    label: "",
    variant: "danger",
    action: null,
    saveLabel: null,
    saveAction: null,
    showConfirm,
    close,
    dismiss,
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

/** The physical-key name a real keyboard reports alongside `key`: letters come
 *  through as `KeyX`, named keys carry their own name. tinykeys discards any
 *  event whose `code` is empty, so an event without one is not merely
 *  incomplete — it never reaches a handler, and every assertion built on it
 *  passes or fails for the wrong reason. */
function codeFor(key: string): string {
  return /^[a-z]$/i.test(key) ? `Key${key.toUpperCase()}` : key;
}

/** Dispatch a real keydown from `from` (bubbles to the window listener). */
function press(key: string, from: HTMLElement): void {
  from.dispatchEvent(
    new KeyboardEvent("keydown", {
      key,
      code: codeFor(key),
      bubbles: true,
      cancelable: true,
    }),
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

describe("useKeyboardShortcuts · confirm-dialog gate (nibs-an5d)", () => {
  // The single shared confirm dialog is reused for every confirm. A global row
  // shortcut firing a SECOND confirm over an open one reuses that dialog and
  // strands the first confirm's pending promise. tinykeys binds a bare `window`
  // listener with no target filtering, so a key pressed while focus sits on a
  // dialog button still reaches these handlers — the gate is the only thing that
  // stops it. Each negative test carries a positive control (same key, dialog
  // closed) so it can only pass because the gate bit, not because the handler is
  // unwired.

  it("'n' does NOT start a create while a confirm dialog is open", () => {
    const h = makeHarness({ confirmOpen: true });
    mount(h);

    // Focus a plain button (isInputFocused() false) — the exact repro: focus on a
    // dialog control, confirm open, pane docked.
    const button = focusEl(document.createElement("button"));
    press("n", button);

    expect(h.startCreate).not.toHaveBeenCalled();
  });

  it("'n' DOES start a create when no confirm dialog is open (control)", () => {
    const h = makeHarness({ confirmOpen: false });
    mount(h);

    const button = focusEl(document.createElement("button"));
    press("n", button);

    expect(h.startCreate).toHaveBeenCalledTimes(1);
  });

  it("'$mod+n' does NOT start a create while a confirm dialog is open", () => {
    const h = makeHarness({ confirmOpen: true });
    mount(h);

    // $mod resolves to Ctrl on non-Mac (jsdom's default); the gate is on the
    // same confirmOpen() check as bare 'n', but that path is separately bound.
    const button = focusEl(document.createElement("button"));
    button.dispatchEvent(
      new KeyboardEvent("keydown", { key: "n", code: "KeyN", ctrlKey: true, bubbles: true, cancelable: true }),
    );

    expect(h.startCreate).not.toHaveBeenCalled();
  });

  it("'$mod+n' DOES start a create when no confirm dialog is open (control)", () => {
    const h = makeHarness({ confirmOpen: false });
    mount(h);

    const button = focusEl(document.createElement("button"));
    button.dispatchEvent(
      new KeyboardEvent("keydown", { key: "n", code: "KeyN", ctrlKey: true, bubbles: true, cancelable: true }),
    );

    expect(h.startCreate).toHaveBeenCalledTimes(1);
  });

  it("'e' does NOT open the focused nib while a confirm dialog is open", () => {
    const h = makeHarness({ confirmOpen: true });
    h.selection.focus("tnib-abcd"); // a valid edit target
    mount(h);

    const button = focusEl(document.createElement("button"));
    press("e", button);

    expect(h.open).not.toHaveBeenCalled();
  });

  it("'e' DOES open the focused nib when no confirm dialog is open (control)", () => {
    const h = makeHarness({ confirmOpen: false });
    h.selection.focus("tnib-abcd");
    mount(h);

    const button = focusEl(document.createElement("button"));
    press("e", button);

    expect(h.open).toHaveBeenCalledTimes(1);
  });

  // Delete/Backspace are the wrong-dialog-answer variant: repainting the open
  // discard confirm as a Delete confirm while its resolver is still pending.
  describe.each(["Delete", "Backspace"] as const)("%s", key => {
    it("does NOT open a second (delete) confirm while a confirm dialog is open", () => {
      const h = makeHarness({ confirmOpen: true });
      h.selection.focus("tnib-abcd"); // a valid delete target
      mount(h);

      const button = focusEl(document.createElement("button"));
      press(key, button);

      expect(h.showConfirm).not.toHaveBeenCalled();
    });

    it("DOES open the delete confirm when no confirm dialog is open (control)", () => {
      const h = makeHarness({ confirmOpen: false });
      h.selection.focus("tnib-abcd");
      mount(h);

      const button = focusEl(document.createElement("button"));
      press(key, button);

      expect(h.showConfirm).toHaveBeenCalledTimes(1);
    });
  });
});

describe("useKeyboardShortcuts · bucket target guard (nibs-oxaq)", () => {
  // A synthetic grouping-bucket id (e.g. "__no_milestone__") can be arrow-focused
  // (focus() has no bucket guard, unlike select/toggleSelect/rangeSelect) or supplied
  // as the context-menu target. getActionTargetIds must never resolve a bucket id, so
  // Delete/Backspace can't dispatch a phantom deleteBatch(["__no_milestone__"]). Each
  // negative test carries a real-id positive control (same key, same path) so it can
  // only pass because the bucket guard bit, not because the handler is unwired.

  describe.each(["Delete", "Backspace"] as const)("%s", key => {
    it("does NOT fire the confirm when a bucket row is focused", () => {
      const h = makeHarness();
      h.selection.focus("__no_milestone__");
      mount(h);

      const button = focusEl(document.createElement("button"));
      press(key, button);

      expect(h.showConfirm).not.toHaveBeenCalled();
    });

    it("DOES fire the confirm when a real nib is focused (control)", () => {
      const h = makeHarness();
      h.selection.focus("tnib-abcd");
      mount(h);

      const button = focusEl(document.createElement("button"));
      press(key, button);

      expect(h.showConfirm).toHaveBeenCalledTimes(1);
    });

    it("does NOT fire the confirm when the context-menu target is a bucket", () => {
      const h = makeHarness({ getContextMenuNibId: () => "__no_milestone__" });
      mount(h);

      const button = focusEl(document.createElement("button"));
      press(key, button);

      expect(h.showConfirm).not.toHaveBeenCalled();
    });

    it("DOES fire the confirm when the context-menu target is a real nib (control)", () => {
      const h = makeHarness({ getContextMenuNibId: () => "tnib-abcd" });
      mount(h);

      const button = focusEl(document.createElement("button"));
      press(key, button);

      expect(h.showConfirm).toHaveBeenCalledTimes(1);
    });
  });
});

describe("useKeyboardShortcuts · Escape survives a focused input", () => {
  // Escape is the one global shortcut deliberately NOT gated by
  // `isInputFocused()`: closing an open view while the caret is still sitting in
  // its editor is the entire point of the Escape hierarchy. tinykeys' own target
  // filter would drop a keypress made from an input before any handler ran, so
  // `keyboard.ts` turns that filter off — and this test is what holds that
  // decision in place. Its negative counterpart is the readonly-input gate
  // above, where suppression IS the wanted behavior; together they pin that the
  // choice is made per shortcut rather than for all of them at once.

  it("closes an open view when Escape comes from a focused text input", () => {
    const h = makeHarness({ isOpen: true });
    mount(h);

    const input = document.createElement("input");
    input.type = "text";
    press("Escape", focusEl(input));

    expect(h.requestClose).toHaveBeenCalledTimes(1);
  });
});

describe("useKeyboardShortcuts · post-delete cleanup", () => {
  // The Delete handler clears the selection after the mutation resolves. What it
  // may clear is narrower than "everything": `selectedNibId` (the detail panel)
  // and the action target can be different rows — the "open on double-click"
  // preference moves selection and focus without moving the panel, and arrow-key
  // nav moves focus alone — so a nib that was not deleted must keep its panel and
  // its `?nib=` URL.

  /** Press Delete and run the confirm dialog's action, i.e. confirm the delete. */
  async function deleteAndConfirm(h: ReturnType<typeof makeHarness>): Promise<void> {
    press("Delete", focusEl(document.createElement("button")));
    const opts = h.showConfirm.mock.calls[0]?.[0] as { action: () => Promise<void> } | undefined;
    expect(opts).toBeDefined();
    await opts!.action();
  }

  it("deleting the focused row leaves a panel showing a different nib open", async () => {
    const h = makeHarness();
    h.selection.select("tnib-open");     // panel on tnib-open
    h.selection.selectOnly("tnib-abcd"); // selection/focus move; panel does not
    mount(h);

    await deleteAndConfirm(h);

    expect(h.execute).toHaveBeenCalledTimes(1);
    expect(h.selection.selectedNibId).toBe("tnib-open");
    expect(h.selection.selectedIds.size).toBe(0);
    expect(h.selection.focusedNibId).toBeNull();
    expect(h.nav.replaceClosed).not.toHaveBeenCalled();
  });

  it("single mode: deleting the arrow-focused row leaves an unrelated panel open", async () => {
    // The same divergence without the "open on double-click" preference: plain
    // ArrowDown calls focus() alone, so the panel keeps showing the row the user
    // opened while the delete targets the focused one.
    const h = makeHarness();
    h.selection.select("tnib-open"); // panel + selection on tnib-open
    h.selection.focus("tnib-abcd");  // plain ArrowDown — no selectOnly
    mount(h);

    await deleteAndConfirm(h);

    expect(h.execute).toHaveBeenCalledTimes(1);
    expect(h.selection.selectedNibId).toBe("tnib-open");
    expect(h.nav.replaceClosed).not.toHaveBeenCalled();
  });

  it("deleting the nib the panel is showing closes it and heals the URL", async () => {
    const h = makeHarness();
    h.selection.select("tnib-abcd"); // panel and target are the same row
    mount(h);

    await deleteAndConfirm(h);

    expect(h.selection.selectedNibId).toBeNull();
    expect(h.nav.replaceClosed).toHaveBeenCalledTimes(1);
  });

  it("a failed delete clears nothing", async () => {
    const h = makeHarness();
    h.execute.mockResolvedValue({ ok: false } as never);
    h.selection.select("tnib-abcd");
    mount(h);

    await deleteAndConfirm(h);

    expect(h.selection.selectedNibId).toBe("tnib-abcd");
    expect(h.selection.selectedIds.has("tnib-abcd")).toBe(true);
    expect(h.nav.replaceClosed).not.toHaveBeenCalled();
  });
});
