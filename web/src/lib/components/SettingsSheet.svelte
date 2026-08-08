<script lang="ts" module>
  // Per-instance counter for unique aria-labelledby/aria-describedby ids.
  let idCounter = 0;
</script>

<script lang="ts">
  import type { RowDensity, FontSize, Theme, DetailPanelPosition, OpenDetailGesture, BlockedEmphasis } from "../types";
  import SegmentedControl from "./SegmentedControl.svelte";
  import ThemeSelect from "./ThemeSelect.svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import TooltipButton from "./TooltipButton.svelte";
  import { Settings, X } from "@lucide/svelte";
  import { Portal } from "bits-ui";
  import { fly } from "svelte/transition";
  import { untrack } from "svelte";
  import { clickOutside } from "$lib/clickOutside";

  let {
    open = $bindable(false),
    rowDensity,
    ondensitychange,
    fontSize,
    onfontsizechange,
    blockedEmphasis,
    onemphasischange,
    theme,
    onthemechange,
    detailPanelPosition,
    onpositionchange,
    openDetailOn,
    onopendetailchange,
  }: {
    open?: boolean;
    rowDensity: RowDensity;
    ondensitychange: (d: RowDensity) => void;
    fontSize: FontSize;
    onfontsizechange: (f: FontSize) => void;
    blockedEmphasis: BlockedEmphasis;
    onemphasischange: (e: BlockedEmphasis) => void;
    theme: Theme;
    onthemechange: (t: Theme) => void;
    detailPanelPosition: DetailPanelPosition;
    onpositionchange: (p: DetailPanelPosition) => void;
    openDetailOn: OpenDetailGesture;
    onopendetailchange: (g: OpenDetailGesture) => void;
  } = $props();

  const densityOptions: { value: RowDensity; label: string }[] = [
    { value: "compact", label: "Compact" },
    { value: "comfortable", label: "Comfortable" },
  ];

  const fontSizeOptions: { value: FontSize; label: string }[] = [
    { value: "small", label: "Small" },
    { value: "medium", label: "Medium" },
    { value: "large", label: "Large" },
  ];

  const emphasisOptions: { value: BlockedEmphasis; label: string }[] = [
    { value: "subtle", label: "Subtle" },
    { value: "pill", label: "Pill" },
    { value: "pill-dim", label: "Pill+dim" },
  ];

  const positionOptions: { value: DetailPanelPosition; label: string }[] = [
    { value: "right", label: "Right" },
    { value: "bottom", label: "Bottom" },
  ];

  const openDetailOptions: { value: OpenDetailGesture; label: string }[] = [
    { value: "single", label: "Single click" },
    { value: "double", label: "Double click" },
  ];

  // Static label shared by the gear trigger's aria-label and its Tooltip.Content,
  // defined once so the accessible name and the visible tooltip can't drift apart.
  const settingsLabel = "Settings";

  const uid = idCounter++;
  const titleId = `settings-title-${uid}`;
  const descId = `settings-desc-${uid}`;
  const appearanceId = `settings-appearance-${uid}`;
  const behaviorId = `settings-behavior-${uid}`;
  const panelId = `settings-panel-${uid}`;

  let triggerEl = $state<HTMLButtonElement | null>(null);
  let panelEl = $state<HTMLElement | null>(null);

  function close() {
    open = false;
  }

  // clickOutside "inside" predicate. The gear trigger toggles the panel, so a
  // pointerdown on it must not also dismiss (double-fire). The Theme control is a
  // shadcn Select whose dropdown content portals to document.body — i.e. a
  // body-level sibling of this <aside>, which clickOutside would otherwise read
  // as "outside" and dismiss the whole panel on the very click that picks an
  // option. Treat any pointerdown within an OPEN portaled select popover as
  // inside. The `[data-state='open']` filter is load-bearing: bits-ui keeps
  // closed content mounted (data-state="closed") for its ~100ms exit animation,
  // so an unfiltered match would defer clicks to a dropdown that is already gone.
  function isInsideOrTrigger(target: Node): boolean {
    if (triggerEl?.contains(target)) return true;
    return (
      target instanceof Element &&
      target.closest("[data-slot='select-content'][data-state='open']") !== null
    );
  }

  // Escape-to-dismiss listens on `document`, not on the <aside>, because this is
  // a non-modal panel that does NOT trap focus: the user can Tab into the
  // (portaled-sibling) background, and a keydown handler bound to the <aside>
  // would never see the event. Mirrors the clickOutside document-listener
  // rationale. The listener is only attached while open, so Escape is a no-op
  // when the panel is closed.
  $effect(() => {
    if (!open) return;
    function onKeydown(e: KeyboardEvent) {
      if (e.key !== "Escape") return;
      // A portaled select popover (e.g. the Theme dropdown) is OPEN — defer to it.
      // bits-ui's escape-layer only calls e.preventDefault(), never
      // stopPropagation(), and both listeners sit on `document` in the bubble
      // phase, so without this guard the sheet's Escape handler would ALSO fire and
      // close the whole panel on the Escape meant to dismiss just the dropdown. The
      // select tears its content down; a second Escape (none open) then closes the
      // panel. The `[data-state='open']` filter matters: bits-ui flips content to
      // data-state="closed" synchronously on close but keeps it mounted ~100ms for
      // the exit animation — matching that lingering closed content would drop a
      // legitimate Escape (needing a third press). Mirrors the isInsideOrTrigger
      // select-content guard on clickOutside.
      if (document.querySelector("[data-slot='select-content'][data-state='open']")) return;
      e.preventDefault();
      close();
    }
    document.addEventListener("keydown", onKeydown);
    return () => document.removeEventListener("keydown", onKeydown);
  });

  // Focus management. On open, move focus into the panel; on close, return it to
  // the gear trigger. This is a non-modal panel: focus is NOT trapped, so nothing
  // pulls focus back if the user tabs out. `open` is the only tracked dependency
  // (element reads are untracked) and `wasOpen` guards against refocusing on every
  // render — we only act on the closed<->open transition.
  let wasOpen = false;
  $effect(() => {
    const isOpen = open;
    untrack(() => {
      if (isOpen && !wasOpen) {
        wasOpen = true;
        // Defer a microtask: the bits-ui Portal mounts this <aside> (and assigns
        // panelEl via bind:this) a microtask AFTER this effect runs, so a
        // synchronous panelEl?.focus() would no-op. The close path below targets
        // the always-mounted trigger, so it needs no such deferral.
        // preventScroll: focusing a fixed, off-canvas panel/trigger must not
        // scroll the page — notably the close-path refocus on outside-click,
        // which would otherwise jump to the gear if the toolbar is scrolled away.
        queueMicrotask(() => panelEl?.focus({ preventScroll: true }));
      } else if (!isOpen && wasOpen) {
        wasOpen = false;
        triggerEl?.focus({ preventScroll: true });
      }
    });
  });
</script>

<!-- Genuinely non-modal settings panel: role="dialog" with
     aria-modal="false", no overlay, the page stays scrollable, and focus is not
     trapped — so the table behind stays visible/interactive while the user
     previews settings (row density now).
     Hand-wired off bits-ui Dialog because Dialog.Content hardcodes
     aria-modal="true" (not overridable via props); here we portal a plain <aside>
     and hand-wire Esc + click-outside dismissal. Do NOT reintroduce an overlay,
     body scroll lock, or focus trap — that would break the non-modal contract. -->
<!-- TooltipButton owns the tooltip + the single <button>. bind:ref exposes that
     button as triggerEl (needed for focus return + clickOutside `contains`), and
     aria-expanded / aria-controls forward through. The wrapper spreads the
     tooltip's props first and applies our onclick last, so the panel-toggle
     OVERRIDES and behavior is unchanged. -->
<TooltipButton
  label={settingsLabel}
  variant="ghost"
  size="icon"
  bind:ref={triggerEl}
  aria-expanded={open}
  aria-controls={panelId}
  onclick={() => (open = !open)}
>
  <Settings size={16} />
</TooltipButton>

{#if open}
  <Portal>
    <!-- role="dialog" on <aside> is intentional: this IS a (non-modal) dialog.
         The implicit "complementary" landmark is replaced on purpose. -->
    <!-- svelte-ignore a11y_no_noninteractive_element_to_interactive_role -->
    <aside
      bind:this={panelEl}
      id={panelId}
      role="dialog"
      aria-modal="false"
      aria-labelledby={titleId}
      aria-describedby={descId}
      tabindex="-1"
      transition:fly={{ x: 320, duration: 200 }}
      use:clickOutside={{ enabled: open, onOutside: close, ignore: isInsideOrTrigger }}
      class="bg-background border-border ring-foreground/10 fixed inset-y-0 right-0 z-50 flex h-full w-3/4 flex-col gap-4 border-l shadow-lg ring-1 outline-none sm:max-w-sm"
    >
      <div class="flex flex-col gap-1.5 p-4">
        <h2 id={titleId} class="text-base leading-none font-medium">Settings</h2>
        <p id={descId} class="sr-only">Application preferences</p>
      </div>

      <div class="flex flex-col gap-6 px-4 pb-4">
        <section aria-labelledby={appearanceId} class="flex flex-col gap-3">
          <h3
            id={appearanceId}
            class="text-caption font-medium text-muted-foreground"
          >
            Appearance
          </h3>

          <div class="flex items-center justify-between gap-3">
            <span class="text-sm text-foreground">Theme</span>
            <ThemeSelect value={theme} onchange={onthemechange} />
          </div>

          <div class="flex items-center justify-between gap-3">
            <span class="text-sm text-foreground">Row density</span>
            <SegmentedControl
              value={rowDensity}
              options={densityOptions}
              ariaLabel="Row density"
              onchange={(v) => ondensitychange(v as RowDensity)}
            />
          </div>

          <div class="flex items-center justify-between gap-3">
            <span class="text-sm text-foreground">Font size</span>
            <SegmentedControl
              value={fontSize}
              options={fontSizeOptions}
              ariaLabel="Font size"
              onchange={(v) => onfontsizechange(v as FontSize)}
            />
          </div>

          <div class="flex items-center justify-between gap-3">
            <span class="text-sm text-foreground">Blocked emphasis</span>
            <SegmentedControl
              value={blockedEmphasis}
              options={emphasisOptions}
              ariaLabel="Blocked emphasis"
              onchange={(v) => onemphasischange(v as BlockedEmphasis)}
            />
          </div>

          <div class="flex items-center justify-between gap-3">
            <span class="text-sm text-foreground">Detail panel position</span>
            <SegmentedControl
              value={detailPanelPosition}
              options={positionOptions}
              ariaLabel="Detail panel position"
              onchange={(v) => onpositionchange(v as DetailPanelPosition)}
            />
          </div>
        </section>

        <section aria-labelledby={behaviorId} class="flex flex-col gap-3">
          <h3
            id={behaviorId}
            class="text-caption font-medium text-muted-foreground"
          >
            Behavior
          </h3>

          <div class="flex items-center justify-between gap-3">
            <span class="text-sm text-foreground">Open detail on</span>
            <SegmentedControl
              value={openDetailOn}
              options={openDetailOptions}
              ariaLabel="Open detail on"
              onchange={(v) => onopendetailchange(v as OpenDetailGesture)}
            />
          </div>
        </section>
      </div>

      <Button
        variant="ghost"
        size="icon-sm"
        class="absolute top-3 right-3"
        onclick={() => close()}
      >
        <X />
        <span class="sr-only">Close</span>
      </Button>
    </aside>
  </Portal>
{/if}
