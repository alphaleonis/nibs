import { describe, expect, it } from "vitest";
import { cn } from "./utils.js";

describe("cn() semantic type-scale merging", () => {
  // --- Real config guards ---------------------------------------------------
  // Each assertion below flips (fails) if the named member of `text-scale`'s
  // conflictingClassGroups list is removed from utils.ts: without the entry the
  // later semantic bundle stops dropping the earlier raw utility and BOTH
  // survive in the merged string.

  it("drops an earlier raw font-size under a later semantic bundle (guards 'font-size')", () => {
    const result = cn("text-xs", "text-caption");
    const classes = result.split(/\s+/);
    expect(classes).toContain("text-caption");
    expect(classes).not.toContain("text-xs");
  });

  it("drops an earlier raw font-weight under a later semantic bundle (guards 'font-weight')", () => {
    const result = cn("font-medium", "text-caption");
    const classes = result.split(/\s+/);
    expect(classes).toContain("text-caption");
    expect(classes).not.toContain("font-medium");
  });

  it("drops an earlier raw line-height under a later semantic bundle (guards 'leading')", () => {
    const result = cn("leading-6", "text-body");
    const classes = result.split(/\s+/);
    expect(classes).toContain("text-body");
    expect(classes).not.toContain("leading-6");
  });

  it("resolves the dropdown-menu-label + Toolbar density case (guards 'font-size' + 'font-weight')", () => {
    // Reproduces cn(base, override) as rendered for the "Row density"
    // DropdownMenu.Label (Toolbar.svelte). The label's base is `text-xs
    // font-medium` (== 12px/500); the consumer layers `text-caption`
    // (== 12px/400). Both raw utilities must drop so only the bundle remains.
    const result = cn(
      "text-muted-foreground px-1.5 py-1 text-xs font-medium data-inset:pl-7 data-[inset]:pl-8",
      "text-caption text-muted-foreground px-2 py-1",
    );
    const classes = result.split(/\s+/);
    expect(classes).toContain("text-caption");
    expect(classes).not.toContain("text-xs");
    expect(classes).not.toContain("font-medium");
  });

  // --- Documentation tests --------------------------------------------------
  // These pass with OR without the custom config: the classes belong to
  // unrelated tailwind-merge groups, so they are default twMerge behavior, NOT
  // regression guards for our conflictingClassGroups entries. They document the
  // intended one-directionality and real call-site shapes. Note: which class
  // actually *wins* at render time depends on Tailwind's @utility CSS emission
  // order, not on tailwind-merge output — a class-string test cannot observe it.

  it("documents: a later raw font-weight is NOT dropped by an earlier bundle (one-directional)", () => {
    // The conflict is deliberately one-directional: a semantic bundle drops
    // earlier raw font utilities, but a raw `font-bold` placed AFTER `text-body`
    // is left intact so a weight-only override stays possible. This survival is
    // default twMerge behavior for unrelated groups, not enforced by our config.
    const result = cn("text-body", "font-bold");
    const classes = result.split(/\s+/);
    expect(classes).toContain("text-body");
    expect(classes).toContain("font-bold");
  });

  it("documents: a later raw font-size is NOT dropped by an earlier bundle (one-directional)", () => {
    // Reverse of the font-size guard: with the bundle FIRST, the later raw
    // `text-sm` survives. Again default twMerge behavior for unrelated groups;
    // the rendered winner is decided by CSS emission order, not this assertion.
    const result = cn("text-caption", "text-sm");
    const classes = result.split(/\s+/);
    expect(classes).toContain("text-caption");
    expect(classes).toContain("text-sm");
  });

  it("documents: the ConfirmDialog warning-button path keeps its arbitrary color value", () => {
    // Real shipping shape: Button base `text-sm font-medium` + the ConfirmDialog
    // override `text-[var(--warning-foreground,white)]`. tailwind-merge classifies
    // that bare arbitrary color value as a text-COLOR (not font-size), so it coexists
    // with the base size/weight rather than being dropped. Documents that the
    // warning color survives; guards nothing in our config (all three groups are
    // unrelated), but flags a future tailwind-merge arbitrary-value reclassification.
    const result = cn(
      "text-sm font-medium",
      "text-[var(--warning-foreground,white)]",
    );
    const classes = result.split(/\s+/);
    expect(classes).toContain("text-[var(--warning-foreground,white)]");
    expect(classes).toContain("text-sm");
    expect(classes).toContain("font-medium");
  });
});
