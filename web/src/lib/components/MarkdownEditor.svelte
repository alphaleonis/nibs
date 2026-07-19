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
     * value already equals the editor's doc, compared in the doc's own line
     * encoding) only holds because the parent stores exactly what `onchange` emits
     * and feeds it straight back here. The consumer MUST:
     *   1. feed `onchange`'s value back as `initialValue` UNMODIFIED — no trim, no
     *      whitespace normalization. A transform makes every self-originated
     *      keystroke look out-of-band, firing a doc-replace on each keypress that
     *      clobbers the live selection; and
     *   2. NOT route that value back through a setter that can remount this
     *      component (e.g. a `bumpBodyVersion`-ing setBody) — that remounts the
     *      editor on every keystroke.
     *
     * Line endings are the ONE exception to rule 1, handled here rather than by the
     * consumer: CR / CRLF in an incoming value are normalized to the doc's own line
     * encoding internally (see the sync $effect), because CodeMirror line-splits the
     * doc and `doc.toString()` rejoins it with "\n", so a CRLF value is never `===`
     * its own echo.
     *
     * LINE-ENDING POLICY for the consumer's value: it keeps its CRLF endings until
     * the USER edits, at which point `onchange` emits the editor's LF doc. An
     * out-of-band sync does NOT flip it to LF — that is what the sync's write-back
     * suppression buys, and it is what lets a rendered checkbox flip (which
     * preserves the body's original endings, see `toggleTaskLine`) persist the same
     * body whether or not this editor happens to be mounted. Consequence: after a
     * sync, the consumer's value and the live doc are intentionally divergent
     * ENCODINGS of the same content. That is stable, not a bug in waiting: the sync
     * guard compares in the doc's encoding, so the next effect run finds them equal
     * and does not dispatch.
     *
     * Consumers must not normalize the value they feed BACK — rule 1 is easier to
     * honor as an absolute and the guard already handles it. Normalizing where a body
     * ENTERS the form (`fieldsFromSnapshot`) is a separate, open question: it would
     * keep `body` and `baseline.body` in the same encoding (the first keystroke flips
     * `body` to LF while the baseline stays CRLF; `nibForm.svelte.ts`'s `sameBody`
     * absorbs that at the comparison sites, so `dirty` and `EditForm.#matchesFields`
     * settle regardless), but it commits to a CRLF→LF-on-open policy whose etag /
     * round-trip blast radius is unverified. Deliberately not done.
     */
    initialValue?: string;
    /**
     * Fires with the editor's EXACT current doc string on every doc change EXCEPT
     * the external-value sync's own (see the sync $effect's WRITE-BACK
     * SUPPRESSION note) — a sync carries no information the consumer does not
     * already have, since it is reconciling toward the consumer's own value.
     */
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

  // True only for the duration of the sync $effect's own dispatch, so the
  // updateListener can tell that doc change apart from a user edit and skip the
  // onchange write-back (see the WRITE-BACK SUPPRESSION note on the effect).
  // Plain `let`: read and written within one synchronous call stack, never
  // rendered, so it needs no reactivity.
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
              // `docChanged` is `!changes.empty` — it does NOT distinguish a user
              // edit from the sync effect's own dispatch, so the `syncing` guard
              // is what keeps the sync from echoing the doc back to the consumer.
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

  // External-value sync: when the incoming `initialValue` changes
  // out-of-band (e.g. a rendered task-checkbox flip in the parent, which does
  // not remount us) reconcile the editor doc via a transaction rather than a
  // remount — this preserves undo history and selection.
  //
  // LINE-ENDING NORMALIZATION: an incoming value can carry CR / CRLF — the backend
  // does not normalize line endings, and `Parse` (`internal/nib/nib.go`) trims one
  // trailing "\n" off the body, so a Windows-authored file reaches us with interior
  // CRLFs and a DANGLING LONE CR at the end. The doc holds none of that: CodeMirror
  // line-splits the value it is handed, and `doc.toString()` rejoins with "\n".
  // Comparing the two encodings char-by-char is meaningless, so both the guard and
  // the diff below work in the doc's own encoding. `state.toText()` applies the SAME
  // split policy `EditorState.create` used to build the doc (both consult the
  // `EditorState.lineSeparator` facet, falling back to CodeMirror's CR / CRLF / LF
  // split) and stringifies through the same "\n" join. Delegating to it rather than
  // restating the policy as a local regex keeps the normalized value in the doc's
  // encoding under ANY line-separator configuration.
  //
  // ECHO-LOOP GUARD: the editor's own updateListener fires onchange on user
  // typing → the parent sets form.body → initialValue updates → this effect
  // re-runs. We compare the incoming value against the editor's CURRENT doc and
  // skip when equal, so a self-echo neither loops nor clobbers the live selection.
  // A CRLF body echoed back is likewise a self-echo: it differs from the doc only
  // by the normalization CodeMirror already applied, so it must not dispatch.
  //
  // MINIMAL DIFF: replacing the whole doc (from:0..len) would collapse any
  // interior caret to position 0. Instead we dispatch only the changed sub-range
  // (common-prefix / common-suffix diff), so selections outside the change map
  // through unchanged — a 1-2 char checkbox flip leaves the caret put. Diffing
  // against a non-normalized value would both inflate the changed range and insert
  // a lone CR that CodeMirror re-splits into an extra line break.
  //
  // addToHistory.of(false): the sync is an out-of-band edit, not a user action —
  // keep it OUT of the undo stack so Ctrl-Z undoes the user's own typing, not a
  // checkbox flip made in the preview pane.
  //
  // WRITE-BACK SUPPRESSION: the dispatch below changes the doc, so the
  // updateListener would fire onchange and push the doc's LF-only string back to
  // the consumer — overwriting the very value we are reconciling TOWARD, and
  // silently stripping the CRLFs an incoming body still carries. `syncing` marks
  // the window so the listener can skip it. The flag is exact rather than
  // best-effort: EditorView.dispatch runs dispatchTransactions -> update() and
  // fires updateListener from inside that call, with no scheduling on the path, so
  // the listener always observes the flag set. It is preferred over sniffing the
  // addToHistory annotation because that annotation means "keep out of undo", not
  // "this is a sync" — any future history-excluded transaction would inherit the
  // suppression by accident, and the guard would silently no-op if the annotation
  // were ever dropped.
  $effect(() => {
    const raw = initialValue; // read before the early return so the effect tracks it
    const v = view;
    if (!v) return;
    const next = v.state.toText(raw).toString();
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
