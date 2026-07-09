<script lang="ts">
  interface Props {
    initialValue?: string;
    onchange: (value: string) => void;
    onsave?: () => void;
  }

  let { initialValue = "", onchange, onsave }: Props = $props();

  let container: HTMLDivElement | undefined = $state(undefined);

  $effect(() => {
    if (!container) return;
    let view: any;
    let aborted = false;
    (async () => {
      try {
        const [
          { basicSetup },
          { EditorView, keymap },
          { EditorState },
          { markdown },
          { HighlightStyle, syntaxHighlighting },
          { tags: t },
        ] = await Promise.all([
          import("codemirror"),
          import("@codemirror/view"),
          import("@codemirror/state"),
          import("@codemirror/lang-markdown"),
          import("@codemirror/language"),
          import("@lezer/highlight"),
        ]);

        if (!container || aborted) return;

        container.innerHTML = "";

        // Token-driven chrome: every colour is a CSS var, so the editor tracks
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

        view = new EditorView({
          state: EditorState.create({
            doc: initialValue,
            extensions: [
              basicSetup,
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
      }
    };
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
