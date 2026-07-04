<script lang="ts">
  import type { RowDensity } from "../types";
  import * as Sheet from "$lib/components/ui/sheet/index.js";
  import SegmentedControl from "./SegmentedControl.svelte";
  import { buttonVariants } from "$lib/components/ui/button/index.js";
  import { Settings2 } from "@lucide/svelte";

  let {
    open = $bindable(false),
    rowDensity,
    ondensitychange,
  }: {
    open?: boolean;
    rowDensity: RowDensity;
    ondensitychange: (d: RowDensity) => void;
  } = $props();

  const densityOptions: { value: RowDensity; label: string }[] = [
    { value: "compact", label: "Compact" },
    { value: "comfortable", label: "Comfortable" },
  ];
</script>

<Sheet.Root bind:open>
  <Sheet.Trigger
    title="Settings"
    class={buttonVariants({ variant: "ghost", size: "icon" })}
  >
    <Settings2 size={16} />
  </Sheet.Trigger>

  <!-- Intentionally non-modal (nibs-8fj2): no overlay, page stays scrollable,
       focus not trapped, so the table behind stays visible/interactive while the
       user previews settings (row density now; live theme preview later, nibs-vmaq).
       Do NOT restore showOverlay/preventScroll/trapFocus to their modal defaults.
       Known caveat: bits-ui Dialog still hardcodes aria-modal="true" and it cannot
       be overridden via props — tracked in nibs-p07b. -->
  <Sheet.Content
    side="right"
    showOverlay={false}
    preventScroll={false}
    trapFocus={false}
  >
    <Sheet.Header>
      <Sheet.Title>Settings</Sheet.Title>
      <Sheet.Description class="sr-only">Application preferences</Sheet.Description>
    </Sheet.Header>

    <div class="flex flex-col gap-6 px-4 pb-4">
      <section aria-labelledby="settings-appearance-heading" class="flex flex-col gap-3">
        <h3
          id="settings-appearance-heading"
          class="text-caption font-medium text-muted-foreground"
        >
          Appearance
        </h3>

        <div class="flex items-center justify-between gap-3">
          <span class="text-sm text-foreground">Row density</span>
          <SegmentedControl
            value={rowDensity}
            options={densityOptions}
            ariaLabel="Row density"
            onchange={(v) => ondensitychange(v as RowDensity)}
          />
        </div>
      </section>
    </div>
  </Sheet.Content>
</Sheet.Root>
