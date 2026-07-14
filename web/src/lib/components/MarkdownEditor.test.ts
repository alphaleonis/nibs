import { render, screen, waitFor, cleanup } from "@testing-library/svelte";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { Transaction } from "@codemirror/state";
import MarkdownEditor from "./MarkdownEditor.svelte";

// The annotation the sync effect stamps on its out-of-band doc-replace to keep it
// out of the undo stack. Built from the same real module the component imports, so
// it deep-equals what the component dispatches.
const historyExcluded = Transaction.addToHistory.of(false);

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
// @codemirror/state is deliberately NOT faked: the real EditorState normalizes
// line endings to LF at create time (CRLF/CR are split as line breaks and rejoined
// with "\n"). A hand-rolled `toString: () => config.doc` would echo the doc back
// verbatim and hide that, so any test covering line-ending handling would pass
// against a broken guard. The real module is the only honest oracle here; the
// mocked EditorView below never applies transactions, so no DOM is needed.
vi.mock("@codemirror/state", async () =>
  vi.importActual<typeof import("@codemirror/state")>("@codemirror/state"),
);
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
      annotations: historyExcluded,
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
      annotations: historyExcluded,
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

  // Regression: the Go backend does not normalize line endings, and `Parse` trims
  // one trailing "\n" off the body, so a Windows-authored nib arrives with interior
  // CRLFs and a dangling lone CR. EditorState.create normalizes all of that to LF,
  // so the doc and the echoed prop differ by line endings alone — that is still a
  // self-echo and must not dispatch.
  it.each([
    ["single CRLF", "a\r\nb"],
    ["blank line between CRLFs", "x\r\n\r\ny"],
    ["lone CR", "a\rb"],
    ["realistic CRLF body", "# T\r\n\r\n- [ ] one\r\n- [ ] two\r"],
  ])(
    "does NOT dispatch when the echoed value differs from the doc only by line endings (%s)",
    async (_label, crlfBody) => {
      const { rerender } = render(MarkdownEditor, {
        initialValue: crlfBody,
        onchange: vi.fn(),
      });

      await waitFor(() => {
        expect(mockViewInstances.length).toBe(1);
      });

      const view = mockViewInstances[0];
      // Precondition: the real EditorState really did normalize away the CRLF, so
      // an exact === guard cannot recognize the echo. If this ever stops holding,
      // the test below is vacuous rather than wrong — assert it explicitly.
      expect(view.state.doc.toString()).toBe(crlfBody.replace(/\r\n?/g, "\n"));
      expect(view.state.doc.toString()).not.toBe(crlfBody);

      // The parent stores onchange's value verbatim and feeds it straight back.
      // Nothing changed, so touching the doc here would rewrite the user's content
      // and mark a pristine form dirty.
      await rerender({ initialValue: crlfBody, onchange: vi.fn() });

      expect(view.dispatch).not.toHaveBeenCalled();
    },
  );

  // Regression: the line-ending-insensitive guard must not swallow real edits that
  // happen to arrive with CRLF endings.
  it("DOES dispatch when a CRLF value differs from the doc by more than line endings", async () => {
    const { rerender } = render(MarkdownEditor, {
      initialValue: "- [ ] a\r\n",
      onchange: vi.fn(),
    });

    await waitFor(() => {
      expect(mockViewInstances.length).toBe(1);
    });

    const view = mockViewInstances[0];
    // Doc is "- [ ] a\n"; an out-of-band checkbox flip arrives still CRLF-encoded.
    await rerender({ initialValue: "- [x] a\r\n", onchange: vi.fn() });

    expect(view.dispatch).toHaveBeenCalledTimes(1);
    expect(view.dispatch).toHaveBeenCalledWith({
      changes: { from: 3, to: 4, insert: "x" },
      annotations: historyExcluded,
    });
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
