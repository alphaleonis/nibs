<script lang="ts">
  import type { EditorView } from "@codemirror/view";

  interface Props {
    /**
     * The editor's body text — this prop is the component's SINGLE SOURCE OF
     * TRUTH and is continuously reconciled: an out-of-band change (e.g. a rendered
     * task-checkbox flip) syncs into the live editor via a minimal-diff
     * transaction WITHOUT a remount (see the external-value-sync $effect below),
     * preserving undo / cursor / scroll.
     *
     * ECHO-LOOP CONTRACT — do not break: the sync guard (skip when the incoming
     * value already equals the editor's doc) only holds because the parent stores
     * exactly what `onchange` emits and feeds it straight back here. The consumer
     * MUST:
     *   1. feed `onchange`'s value back as `initialValue` UNMODIFIED — no trim, no
     *      CRLF/whitespace normalization. A transform makes every self-originated
     *      keystroke look out-of-band, firing a doc-replace on each keypress that
     *      clobbers the live selection (the exact defect nibs-fva8 removed); and
     *   2. NOT route that value back through a setter that can remount this
     *      component (e.g. a `bumpBodyVersion`-ing setBody) — that remounts the
     *      editor on every keystroke.
     */
    initialValue?: string;
    /** Fires with the editor's EXACT current doc string on every doc change. */
    onchange: (value: string) => void;
    onsave?: () => void;
  }

  let { initialValue = "", onchange, onsave }: Props = $props();

  let container: HTMLDivElement | undefined = $state(undefined);

  // The live CodeMirror view, hoisted out of the async init IIFE so the
  // external-value sync $effect (below) can reach it. `$state.raw` because we
  // only ever swap the whole reference (never mutate it) and CodeMirror's view
  // must not be wrapped in a reactive proxy. Reassigning it (init / teardown)
  // re-runs the sync effect.
  let view: EditorView | undefined = $state.raw(undefined);

  // The Transaction class from the dynamically-imported @codemirror/state,
  // captured during init so the sync $effect can mark its doc-replace as
  // history-excluded (addToHistory.of(false)) — without a second import. Assigned
  // BEFORE `view`, so it is always present when the sync effect fires.
  let cmTransaction: typeof import("@codemirror/state").Transaction | undefined =
    $state.raw(undefined);

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

        // Token-driven chrome: every color is a CSS var, so the editor tracks
        // the app's light/dark theme engine automatically (no fixed oneDark).
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

        // basicSetup reproduced without the gutter extensions
        // (lineNumbers, highlightActiveLineGutter, foldGutter) so the
        // prose editor has no left gutter. Everything else basicSetup
        // provides is retained.
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
              EditorView.updateListener.of((update: any) => {
                if (update.docChanged) {
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

  // External-value sync: when the incoming `initialValue` changes
  // out-of-band (e.g. a rendered task-checkbox flip in the parent, which no
  // longer remounts us) reconcile the editor doc via a transaction rather than a
  // remount — this preserves undo history and selection.
  //
  // ECHO-LOOP GUARD: the editor's own updateListener fires onchange on user
  // typing → the parent sets form.body → initialValue updates → this effect
  // re-runs. We compare the incoming value against the editor's CURRENT doc and
  // skip when equal, so a self-echo neither loops nor clobbers the live selection.
  //
  // MINIMAL DIFF: replacing the whole doc (from:0..len) would collapse any
  // interior caret to position 0. Instead we dispatch only the changed sub-range
  // (common-prefix / common-suffix diff), so selections outside the change map
  // through unchanged — a 1-2 char checkbox flip leaves the caret put.
  //
  // addToHistory.of(false): the sync is an out-of-band edit, not a user action —
  // keep it OUT of the undo stack so Ctrl-Z undoes the user's own typing, not a
  // checkbox flip made in the preview pane.
  $effect(() => {
    const v = view;
    const next = initialValue;
    if (!v) return;
    const cur = v.state.doc.toString();
    if (cur === next) return; // self-echo → nothing to do

    let start = 0;
    const maxStart = Math.min(cur.length, next.length);
    while (start < maxStart && cur[start] === next[start]) start++;
    let curEnd = cur.length;
    let nextEnd = next.length;
    while (curEnd > start && nextEnd > start && cur[curEnd - 1] === next[nextEnd - 1]) {
      curEnd--;
      nextEnd--;
    }

    v.dispatch({
      changes: { from: start, to: curEnd, insert: next.slice(start, nextEnd) },
      ...(cmTransaction
        ? { annotations: cmTransaction.addToHistory.of(false) }
        : {}),
    });
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
