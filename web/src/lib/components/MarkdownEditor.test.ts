import { render, screen, waitFor, cleanup } from "@testing-library/svelte";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import MarkdownEditor from "./MarkdownEditor.svelte";

// Track created EditorView instances and their config
const { mockViewInstances, mockUpdateListenerCallback } = vi.hoisted(() => {
  return {
    mockViewInstances: [] as any[],
    mockUpdateListenerCallback: { current: null as any },
  };
});

// Mock CodeMirror modules
vi.mock("@codemirror/lang-markdown", () => ({
  markdown: () => [],
}));
vi.mock("@codemirror/language", () => ({
  HighlightStyle: { define: () => [] },
  syntaxHighlighting: () => [],
  defaultHighlightStyle: [],
  indentOnInput: () => [],
  bracketMatching: () => [],
  foldKeymap: [],
}));
vi.mock("@codemirror/commands", () => ({
  history: () => [],
  defaultKeymap: [],
  historyKeymap: [],
}));
vi.mock("@codemirror/search", () => ({
  highlightSelectionMatches: () => [],
  searchKeymap: [],
}));
vi.mock("@codemirror/autocomplete", () => ({
  closeBrackets: () => [],
  autocompletion: () => [],
  closeBracketsKeymap: [],
  completionKeymap: [],
}));
vi.mock("@codemirror/lint", () => ({
  lintKeymap: [],
}));
vi.mock("@lezer/highlight", () => ({
  tags: new Proxy({}, { get: () => ({}) }),
}));
vi.mock("@codemirror/state", () => ({
  EditorState: {
    allowMultipleSelections: { of: () => [] },
    create: (config: any) => ({
      doc: {
        length: config?.doc?.length ?? 0,
        toString: () => config?.doc ?? "",
      },
    }),
  },
  // The sync effect marks its doc-replace transaction as history-excluded.
  Transaction: {
    addToHistory: { of: (value: boolean) => ({ __annotation: "addToHistory", value }) },
  },
}));
vi.mock("@codemirror/view", () => {
  return {
    EditorView: class MockEditorView {
      static lineWrapping = [];
      static theme = (_spec: any, _opts?: any) => [];
      static updateListener = {
        of: (cb: any) => {
          mockUpdateListenerCallback.current = cb;
          return [];
        },
      };
      state: any;
      destroy = vi.fn();
      dispatch = vi.fn();
      constructor(config: any) {
        this.state = config.state;
        if (config.parent) {
          config.parent.innerHTML = "<div data-testid='mock-cm'>CodeMirror</div>";
        }
        mockViewInstances.push(this);
      }
    },
    keymap: {
      of: (bindings: any[]) => {
        // Store the keymap bindings for onsave testing
        (globalThis as any).__cmKeymapBindings = bindings;
        return [];
      },
    },
    highlightSpecialChars: () => [],
    drawSelection: () => [],
    dropCursor: () => [],
    rectangularSelection: () => [],
    crosshairCursor: () => [],
    highlightActiveLine: () => [],
  };
});

describe("MarkdownEditor", () => {
  beforeEach(() => {
    mockViewInstances.length = 0;
    mockUpdateListenerCallback.current = null;
    (globalThis as any).__cmKeymapBindings = [];
  });

  afterEach(() => {
    cleanup();
  });

  it("initializes EditorView with initialValue as doc", async () => {
    render(MarkdownEditor, {
      initialValue: "# Hello World",
      onchange: vi.fn(),
    });

    await waitFor(() => {
      expect(mockViewInstances.length).toBe(1);
    });

    const view = mockViewInstances[0];
    expect(view.state.doc.toString()).toBe("# Hello World");
  });

  it("uses empty string when initialValue is not provided", async () => {
    render(MarkdownEditor, {
      onchange: vi.fn(),
    });

    await waitFor(() => {
      expect(mockViewInstances.length).toBe(1);
    });

    const view = mockViewInstances[0];
    expect(view.state.doc.toString()).toBe("");
  });

  it("calls onchange when document changes", async () => {
    const onchange = vi.fn();
    render(MarkdownEditor, {
      initialValue: "initial",
      onchange,
    });

    await waitFor(() => {
      expect(mockUpdateListenerCallback.current).not.toBeNull();
    });

    // Simulate a document change through the update listener
    mockUpdateListenerCallback.current({
      docChanged: true,
      state: { doc: { toString: () => "updated content" } },
    });

    expect(onchange).toHaveBeenCalledWith("updated content");
  });

  it("does not call onchange when document has not changed", async () => {
    const onchange = vi.fn();
    render(MarkdownEditor, {
      initialValue: "initial",
      onchange,
    });

    await waitFor(() => {
      expect(mockUpdateListenerCallback.current).not.toBeNull();
    });

    // Simulate an update that is NOT a doc change (e.g., selection change)
    mockUpdateListenerCallback.current({
      docChanged: false,
      state: { doc: { toString: () => "initial" } },
    });

    expect(onchange).not.toHaveBeenCalled();
  });

  it("syncs an out-of-band checkbox flip via a MINIMAL-DIFF, history-excluded transaction (no remount, cursor preserved)", async () => {
    const { rerender } = render(MarkdownEditor, {
      initialValue: "- [ ] a",
      onchange: vi.fn(),
    });

    await waitFor(() => {
      expect(mockViewInstances.length).toBe(1);
    });

    const view = mockViewInstances[0];
    expect(view.dispatch).not.toHaveBeenCalled();

    // The parent flips a checkbox out-of-band ("- [ ] a" -> "- [x] a") WITHOUT
    // remounting — the editor syncs via a transaction that changes ONLY the single
    // differing char (from:3,to:4,insert:"x"), so a caret elsewhere maps through
    // unchanged. A full-doc replace (from:0) would collapse the caret to 0.
    await rerender({ initialValue: "- [x] a", onchange: vi.fn() });

    expect(view.dispatch).toHaveBeenCalledTimes(1);
    expect(view.dispatch).toHaveBeenCalledWith({
      changes: { from: 3, to: 4, insert: "x" },
      // Out-of-band sync stays OUT of the undo stack (Ctrl-Z undoes the user's
      // own typing, not the preview-pane checkbox flip).
      annotations: { __annotation: "addToHistory", value: false },
    });
    // Still the SAME editor instance — no remount happened.
    expect(mockViewInstances.length).toBe(1);
  });

  it("minimal-diff sync pins `to` to the OLD doc length, not the new value's length (different-length change)", async () => {
    const { rerender } = render(MarkdownEditor, {
      initialValue: "ab",
      onchange: vi.fn(),
    });

    await waitFor(() => {
      expect(mockViewInstances.length).toBe(1);
    });

    const view = mockViewInstances[0];

    // Appending "cd": the only change is an insertion at the end. `to` must be the
    // OLD doc's end (2), not next.length (4) — a `to: next.length` regression fails.
    await rerender({ initialValue: "abcd", onchange: vi.fn() });

    expect(view.dispatch).toHaveBeenCalledTimes(1);
    expect(view.dispatch).toHaveBeenCalledWith({
      changes: { from: 2, to: 2, insert: "cd" },
      annotations: { __annotation: "addToHistory", value: false },
    });
  });

  it("does NOT dispatch when the incoming value equals the current doc (echo-loop guard)", async () => {
    const { rerender } = render(MarkdownEditor, {
      initialValue: "same",
      onchange: vi.fn(),
    });

    await waitFor(() => {
      expect(mockViewInstances.length).toBe(1);
    });

    const view = mockViewInstances[0];

    // The parent echoes our own onchange back as the incoming value (user typing
    // → onchange → parent sets body → prop updates). The value equals the current
    // doc, so a dispatch here would fight the user's selection / loop endlessly.
    await rerender({ initialValue: "same", onchange: vi.fn() });

    expect(view.dispatch).not.toHaveBeenCalled();
  });

  it("calls onsave on Mod-S keymap", async () => {
    const onsave = vi.fn();
    render(MarkdownEditor, {
      initialValue: "",
      onchange: vi.fn(),
      onsave,
    });

    await waitFor(() => {
      expect((globalThis as any).__cmKeymapBindings.length).toBeGreaterThan(0);
    });

    const binding = (globalThis as any).__cmKeymapBindings.find(
      (b: any) => b.key === "Mod-s",
    );
    expect(binding).toBeDefined();

    const result = binding.run();
    expect(result).toBe(true);
    expect(onsave).toHaveBeenCalled();
  });

  it("destroys the view on unmount", async () => {
    const { unmount } = render(MarkdownEditor, {
      initialValue: "test",
      onchange: vi.fn(),
    });

    await waitFor(() => {
      expect(mockViewInstances.length).toBe(1);
    });

    const view = mockViewInstances[0];
    unmount();

    expect(view.destroy).toHaveBeenCalled();
  });

  it("renders the container div with data-testid", () => {
    render(MarkdownEditor, {
      onchange: vi.fn(),
    });

    expect(screen.getByTestId("markdown-editor")).toBeInTheDocument();
  });
});
