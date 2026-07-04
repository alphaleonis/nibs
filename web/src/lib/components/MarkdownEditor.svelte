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
          { oneDark },
        ] = await Promise.all([
          import("codemirror"),
          import("@codemirror/view"),
          import("@codemirror/state"),
          import("@codemirror/lang-markdown"),
          import("@codemirror/theme-one-dark"),
        ]);

        if (!container || aborted) return;

        container.innerHTML = "";

        view = new EditorView({
          state: EditorState.create({
            doc: initialValue,
            extensions: [
              basicSetup,
              markdown(),
              oneDark,
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
    border-radius: var(--radius-lg);
  }

  .markdown-editor :global(.cm-scroller) {
    overflow: auto;
  }
</style>
