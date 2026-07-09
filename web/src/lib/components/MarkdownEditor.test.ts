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
vi.mock("codemirror", () => ({
  basicSetup: [],
}));
vi.mock("@codemirror/lang-markdown", () => ({
  markdown: () => [],
}));
vi.mock("@codemirror/language", () => ({
  HighlightStyle: { define: () => [] },
  syntaxHighlighting: () => [],
}));
vi.mock("@lezer/highlight", () => ({
  tags: new Proxy({}, { get: () => ({}) }),
}));
vi.mock("@codemirror/state", () => ({
  EditorState: {
    create: (config: any) => ({
      doc: {
        length: config?.doc?.length ?? 0,
        toString: () => config?.doc ?? "",
      },
    }),
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
