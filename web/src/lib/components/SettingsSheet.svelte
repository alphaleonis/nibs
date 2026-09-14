<script lang="ts" module>
  // Per-instance counter for unique aria-labelledby/aria-describedby ids.
  let idCounter = 0;
</script>

<script lang="ts">
  import type { RowDensity, FontSize, Theme, DetailPanelPosition, OpenDetailGesture, BlockedEmphasis, RegionBandMode } from "../types";
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
    regionBands,
    onemphasischange,
    onregionbandschange,
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
    regionBands: RegionBandMode;
    onemphasischange: (e: BlockedEmphasis) => void;
    onregionbandschange: (m: RegionBandMode) => void;
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

  const regionBandOptions: { value: RegionBandMode; label: string }[] = [
    { value: "on-drag", label: "While dragging" },
    { value: "never", label: "Never" },
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

  // Not "outside": the gear trigger, which toggles the panel itself, and the Theme
  // select's content, which is portaled to <body>. Match only open content: closed
  // content stays mounted through its exit animation.
  function isInsideOrTrigger(target: Node): boolean {
    if (triggerEl?.contains(target)) return true;
    return (
      target instanceof Element &&
      target.closest("[data-slot='select-content'][data-state='open']") !== null
    );
  }

  // Listens on `document`: focus is not trapped, so a handler on the <aside> would
  // miss Escape pressed after tabbing out.
  $effect(() => {
    if (!open) return;
    function onKeydown(e: KeyboardEvent) {
      if (e.key !== "Escape") return;
      // Defer to an open select: bits-ui's escape layer, also on `document`, only
      // calls preventDefault, so this would otherwise close the panel too.
      if (document.querySelector("[data-slot='select-content'][data-state='open']")) return;
      e.preventDefault();
      close();
    }
    document.addEventListener("keydown", onKeydown);
    return () => document.removeEventListener("keydown", onKeydown);
  });

  // Move focus into the panel on open and back to the trigger on close, on the
  // transition only.
  let wasOpen = false;
  $effect(() => {
    const isOpen = open;
    untrack(() => {
      if (isOpen && !wasOpen) {
        wasOpen = true;
        // Deferred: the Portal has not mounted the <aside> when this runs.
        queueMicrotask(() => panelEl?.focus({ preventScroll: true }));
      } else if (!isOpen && wasOpen) {
        wasOpen = false;
        triggerEl?.focus({ preventScroll: true });
      }
    });
  });
</script>

<!-- Non-modal panel: no overlay, scroll lock or focus trap, so the table stays
     usable while settings are previewed. Not bits-ui Dialog, whose content
     hardcodes aria-modal="true". -->
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
    <!-- role="dialog" replaces the <aside>'s complementary landmark. -->
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
            <span class="text-sm text-foreground">Ordering rules</span>
            <SegmentedControl
              value={regionBands}
              options={regionBandOptions}
              ariaLabel="Ordering rules"
              onchange={(v) => onregionbandschange(v as RegionBandMode)}
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
