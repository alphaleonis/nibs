import { render, screen, waitFor, cleanup } from "@testing-library/svelte";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { Transaction } from "@codemirror/state";
import MarkdownEditor from "./MarkdownEditor.svelte";
import { toggleTaskLine } from "../markdown";

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
// against a broken guard. The real module is the only honest oracle here, and it
// is pure state — `state.update()` needs no DOM, so the mocked EditorView below
// can land a transaction for real without rendering anything.
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
      // Mirrors the real EditorView.dispatch -> update() closely enough to be an
      // honest oracle for what happens AFTER a transaction lands: it applies the
      // spec to `state` and re-invokes the updateListener SYNCHRONOUSLY, from
      // inside the dispatch call, exactly as the real one does (dispatch ->
      // dispatchTransactions -> update -> `for (listener of facet(updateListener))`,
      // no scheduling anywhere on that path). A bare vi.fn() stub leaves `state`
      // frozen and never reaches the listener, so any onchange write-back the
      // component performs in response to its own dispatch stays invisible and a
      // test asserting on it passes against a broken component.
      dispatch = vi.fn((spec: any) => {
        const tr = this.state.update(spec);
        this.state = tr.state;
        mockUpdateListenerCallback.current?.({
          docChanged: !tr.changes.empty,
          state: tr.state,
          transactions: [tr],
        });
      });
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

  // Regression: the sync effect's own dispatch must not write back through
  // onchange. `toggleTaskLine` preserves a body's original line endings, but the
  // doc holds LF only (CodeMirror line-splits the value it is handed and
  // `doc.toString()` rejoins with "\n"). Echoing the landed doc back to the parent
  // therefore rewrites every CRLF to LF — and since the write-back only happens
  // while the editor is mounted, the same checkbox flip would persist a different
  // body in preview-only mode than in side-by-side mode.
  it("does NOT write back through onchange when its OWN sync dispatch lands (the parent keeps its CRLF body)", async () => {
    const onchange = vi.fn();
    const crlfBody = "- [ ] a\r\n- [ ] b";
    const { rerender } = render(MarkdownEditor, {
      initialValue: crlfBody,
      onchange,
    });

    await waitFor(() => {
      expect(mockViewInstances.length).toBe(1);
    });

    const view = mockViewInstances[0];
    // Precondition: the doc really is LF-only, so a write-back here really would
    // cost the parent its CRLFs. Without this the test could pass vacuously.
    expect(view.state.doc.toString()).toBe("- [ ] a\n- [ ] b");

    // The parent flips the first checkbox in its own copy, which is still CRLF.
    const flipped = toggleTaskLine(crlfBody, 0);
    expect(flipped).toBe("- [x] a\r\n- [ ] b"); // toggleTaskLine kept the CRLF

    await rerender({ initialValue: flipped, onchange });

    // The sync lands — the doc has to track the flip ...
    expect(view.dispatch).toHaveBeenCalledTimes(1);
    expect(view.state.doc.toString()).toBe("- [x] a\n- [ ] b");
    // ... but it is the component's OWN sync, not a user edit. No write-back, so
    // the parent keeps the CRLF-encoded body it just computed.
    expect(onchange).not.toHaveBeenCalled();
  });

  // A dispatch the component did not initiate writes back. Note what this does NOT
  // pin: rendering fresh means `initialValue` already equals the doc, so the sync
  // effect hits its `cur === next` early return and never dispatches — `syncing` is
  // never set. This covers the flag's DEFAULT state only. That the suppression
  // window ever CLOSES is a separate invariant; the test below drives a real sync
  // first and covers it.
  it("DOES call onchange for a landed dispatch it did not initiate", async () => {
    const onchange = vi.fn();
    render(MarkdownEditor, {
      initialValue: "a",
      onchange,
    });

    await waitFor(() => {
      expect(mockViewInstances.length).toBe(1);
    });

    const view = mockViewInstances[0];
    view.dispatch({ changes: { from: 1, insert: "b" } });

    expect(onchange).toHaveBeenCalledWith("ab");
  });

  // Regression: the suppression window must CLOSE. `syncing` is set around the sync
  // effect's own dispatch and reset in a `finally`; if that reset is ever dropped or
  // refactored away, the flag latches `true` and the listener suppresses EVERY later
  // doc change — the user's typing never reaches onchange, the form never goes dirty,
  // and Save persists the pre-edit body. Silent and total, with no symptom until the
  // wrong body lands on disk. Unlike the test above, this one drives a real sync
  // first, so the flag genuinely goes `true` and must be observed going back to
  // `false`: a stuck-open window is only detectable through an edit that FOLLOWS a
  // sync on the same view instance.
  it("resumes the onchange write-back once its OWN sync dispatch completes (the suppression window CLOSES)", async () => {
    const onchange = vi.fn();
    const { rerender } = render(MarkdownEditor, {
      initialValue: "a",
      onchange,
    });

    await waitFor(() => {
      expect(mockViewInstances.length).toBe(1);
    });

    const view = mockViewInstances[0];

    // An out-of-band change drives a REAL sync dispatch — the only thing that opens
    // the window. The mock applies the spec and re-invokes the updateListener from
    // inside the dispatch call, exactly as CodeMirror does, so the flag is genuinely
    // observed set here and the suppression below is the shipped behavior.
    await rerender({ initialValue: "ab", onchange });
    expect(view.dispatch).toHaveBeenCalledTimes(1);
    expect(view.state.doc.toString()).toBe("ab");
    expect(onchange).not.toHaveBeenCalled(); // the sync's own write-back is suppressed

    // The user now edits the SAME view instance. This listener call happens OUTSIDE
    // any sync dispatch — which is what a keystroke looks like — so the window has to
    // have closed for it to reach the consumer.
    view.dispatch({ changes: { from: 2, insert: "c" } });

    expect(onchange).toHaveBeenCalledTimes(1);
    expect(onchange).toHaveBeenCalledWith("abc");
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
