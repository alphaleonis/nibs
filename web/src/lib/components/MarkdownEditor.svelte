<script lang="ts">
  import type { EditorView } from "@codemirror/view";

  interface Props {
    /**
     * The body text. A change that did not come from this editor (e.g. a rendered
     * task-checkbox flip) syncs in as a minimal-diff transaction, keeping undo,
     * cursor and scroll.
     *
     * Echo-loop contract: feed `onchange`'s value back unmodified (no trim or
     * whitespace normalization), and not through a remounting setter such as a
     * `bumpBodyVersion`-ing setBody — otherwise every keystroke replaces the doc
     * or remounts the editor. Line endings are handled here: a CRLF value stays
     * CRLF until the user edits.
     */
    initialValue?: string;
    /**
     * Fires with the editor's exact doc string on every doc change except the
     * external-value sync's own.
     */
    onchange: (value: string) => void;
    onsave?: () => void;
  }

  let { initialValue = "", onchange, onsave }: Props = $props();

  let container: HTMLDivElement | undefined = $state(undefined);

  // `$state.raw`: CodeMirror's view must not be wrapped in a reactive proxy.
  // Reassigning it re-runs the sync effect.
  let view: EditorView | undefined = $state.raw(undefined);

  // Captured at init so the sync effect can mark its change history-excluded.
  // Assigned before `view`.
  let cmTransaction: typeof import("@codemirror/state").Transaction | undefined =
    $state.raw(undefined);

  // True only during the sync effect's own dispatch.
  let syncing = false;

  $effect(() => {
    if (!container) return;
    let aborted = false;
    (async () => {
      try {
        const [
          {
            EditorView,
            keymap,
            highlightSpecialChars,
            drawSelection,
            dropCursor,
            rectangularSelection,
            crosshairCursor,
            highlightActiveLine,
          },
          { EditorState, Transaction },
          { markdown },
          {
            HighlightStyle,
            syntaxHighlighting,
            defaultHighlightStyle,
            indentOnInput,
            bracketMatching,
            foldKeymap,
          },
          { history, defaultKeymap, historyKeymap },
          { highlightSelectionMatches, searchKeymap },
          {
            closeBrackets,
            autocompletion,
            closeBracketsKeymap,
            completionKeymap,
          },
          { lintKeymap },
          { tags: t },
        ] = await Promise.all([
          import("@codemirror/view"),
          import("@codemirror/state"),
          import("@codemirror/lang-markdown"),
          import("@codemirror/language"),
          import("@codemirror/commands"),
          import("@codemirror/search"),
          import("@codemirror/autocomplete"),
          import("@codemirror/lint"),
          import("@lezer/highlight"),
        ]);

        if (!container || aborted) return;

        container.innerHTML = "";

        // Every color is a CSS var, so the chrome follows the active theme. The
        // `dark` flag is read once, at init.
        const isDark =
          typeof document !== "undefined" &&
          document.documentElement.classList.contains("dark");

        const appTheme = EditorView.theme(
          {
            "&": {
              backgroundColor: "var(--background)",
              color: "var(--foreground)",
              fontSize: "0.875rem",
              height: "100%",
            },
            "&.cm-focused": { outline: "none" },
            ".cm-content": {
              caretColor: "var(--ring)",
              fontFamily:
                "ui-monospace, SFMono-Regular, 'SF Mono', Menlo, Consolas, monospace",
            },
            ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--ring)" },
            "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection":
              { backgroundColor: "var(--accent)" },
            ".cm-gutters": {
              backgroundColor: "var(--muted)",
              color: "var(--muted-foreground)",
              border: "none",
              borderRight: "1px solid var(--border)",
            },
            ".cm-activeLine": { backgroundColor: "transparent" },
            ".cm-activeLineGutter": {
              backgroundColor: "var(--muted)",
              color: "var(--foreground)",
            },
          },
          { dark: isDark },
        );

        const appHighlight = syntaxHighlighting(
          HighlightStyle.define([
            { tag: t.heading, color: "var(--type-feature)", fontWeight: "600" },
            { tag: t.strong, fontWeight: "700", color: "var(--foreground)" },
            { tag: t.emphasis, fontStyle: "italic" },
            { tag: [t.link, t.url], color: "var(--link)" },
            { tag: t.monospace, color: "var(--tag-text)" },
            { tag: t.list, color: "var(--muted-foreground)" },
            { tag: t.quote, color: "var(--muted-foreground)", fontStyle: "italic" },
            { tag: t.contentSeparator, color: "var(--muted-foreground)" },
          ]),
        );

        // basicSetup minus its gutter extensions (lineNumbers,
        // highlightActiveLineGutter, foldGutter).
        const editorBasics = [
          highlightSpecialChars(),
          history(),
          drawSelection(),
          dropCursor(),
          EditorState.allowMultipleSelections.of(true),
          indentOnInput(),
          syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
          bracketMatching(),
          closeBrackets(),
          autocompletion(),
          rectangularSelection(),
          crosshairCursor(),
          highlightActiveLine(),
          highlightSelectionMatches(),
          keymap.of([
            ...closeBracketsKeymap,
            ...defaultKeymap,
            ...searchKeymap,
            ...historyKeymap,
            ...foldKeymap,
            ...completionKeymap,
            ...lintKeymap,
          ]),
        ];

        cmTransaction = Transaction;

        view = new EditorView({
          state: EditorState.create({
            doc: initialValue,
            extensions: [
              ...editorBasics,
              markdown(),
              appTheme,
              appHighlight,
              EditorView.lineWrapping,
              // `docChanged` is also true for the sync effect's own dispatch.
              EditorView.updateListener.of((update: any) => {
                if (update.docChanged && !syncing) {
                  onchange(update.state.doc.toString());
                }
              }),
              keymap.of([
                {
                  key: "Mod-s",
                  run: () => {
                    onsave?.();
                    return true;
                  },
                },
              ]),
            ],
          }),
          parent: container,
        });
      } catch (err) {
        console.error("Failed to load editor:", err);
      }
    })();

    return () => {
      aborted = true;
      if (view) {
        view.destroy();
        view = undefined;
      }
    };
  });

  // External-value sync: reconcile an out-of-band `initialValue` change as a
  // transaction. Compare and diff in the doc's encoding: the doc joins lines with
  // "\n", and `state.toText()` splits with the doc's own `lineSeparator`.
  $effect(() => {
    const raw = initialValue; // read before the early return so the effect tracks it
    const v = view;
    if (!v) return;
    const next = v.state.toText(raw).toString();
    const cur = v.state.doc.toString();
    if (cur === next) return; // self-echo

    // Dispatch only the changed sub-range, so a caret outside it stays put.
    let start = 0;
    const maxStart = Math.min(cur.length, next.length);
    while (start < maxStart && cur[start] === next[start]) start++;
    let curEnd = cur.length;
    let nextEnd = next.length;
    while (curEnd > start && nextEnd > start && cur[curEnd - 1] === next[nextEnd - 1]) {
      curEnd--;
      nextEnd--;
    }

    // `syncing` suppresses the onchange write-back, which would push the LF doc
    // over the consumer's CRLF value; dispatch runs the listener synchronously.
    // Do not key it on the addToHistory annotation, which other transactions can carry.
    syncing = true;
    try {
      v.dispatch({
        changes: { from: start, to: curEnd, insert: next.slice(start, nextEnd) },
        ...(cmTransaction
          ? { annotations: cmTransaction.addToHistory.of(false) }
          : {}),
      });
    } finally {
      syncing = false;
    }
  });
</script>

<div bind:this={container} data-testid="markdown-editor" class="markdown-editor"></div>

<style>
  .markdown-editor {
    height: 100%;
    width: 100%;
  }

  .markdown-editor :global(.cm-editor) {
    height: 100%;
  }

  .markdown-editor :global(.cm-scroller) {
    overflow: auto;
  }
</style>
