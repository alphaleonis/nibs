<script lang="ts">
  import * as Tooltip from "$lib/components/ui/tooltip/index.js";
  import type { Snippet } from "svelte";

  let {
    tooltip,
    side = "bottom",
    ariaHidden = false,
    triggerElement = "button",
    trigger,
  }: {
    /** Tooltip text ONLY (not the accessible name — unlike TooltipButton.label).
     *  The caller MUST set the trigger's own aria-label inside the `trigger`
     *  snippet (usually to the same string) — EXCEPT under `ariaHidden`, where the
     *  trigger sits outside the accessibility tree and has no accessible name to
     *  give. Named `tooltip`, not `label`, to make that contract explicit. */
    tooltip: string;
    side?: "top" | "right" | "bottom" | "left";
    /**
     * Hide the tooltip content from assistive technology.
     *
     * Needed only when the trigger sits inside an `aria-hidden` subtree. The
     * content is PORTALED to `<body>`, so it escapes that subtree and inherits
     * none of its hiding — left alone it would be announced while the thing it
     * describes is not. Set it to keep the content on the same AT layer as its
     * trigger. Default false: a normally-reachable trigger wants a reachable hint.
     */
    ariaHidden?: boolean;
    /**
     * What the `trigger` snippet renders. `"button"` passes bits-ui's props
     * through untouched; `"other"` strips `type`, which is invalid anywhere but a
     * `<button>` and cannot be dropped at the call site (see below).
     */
    triggerElement?: "button" | "other";
    /**
     * Renders whatever the tooltip attaches to. Receives the `Tooltip.Trigger`'s
     * `props` (its hover/focus attachment + a11y attrs); the caller spreads them
     * onto its own trigger element or component.
     *
     * Two attachment modes, decided by what the snippet renders:
     *
     * - CHAIN — the snippet renders a bits-ui component that internally runs
     *   `mergeProps(restProps, triggerState.props)` (`DropdownMenu.Trigger`,
     *   `Popover.Trigger`). The tooltip's spread handlers CHAIN with the
     *   component's own, so both fire: hover/focus opens the tooltip, click/keydown
     *   opens the menu or panel. Nothing else is needed.
     * - OVERRIDE — the snippet renders a raw element and therefore controls spread
     *   order itself. Anything written AFTER `{...props}` wins over the tooltip's
     *   version of it, which is how a caller keeps its own `onclick` or pins an
     *   attribute bits-ui merged in for a `<button>` (`tabindex` defaults to 0). The
     *   tooltip still opens because its hover/focus handlers are not overridden.
     *   `TooltipButton` uses this mode too.
     *
     *   On a non-button element, `type` is the exception: it is merged in
     *   unconditionally and cannot be overridden, because it is not a valid
     *   attribute there to write at all. Pass `triggerElement="other"` and this
     *   component removes it before the snippet ever sees it.
     */
    trigger: Snippet<[{ props: Record<string, unknown> }]>;
  } = $props();

  // `type` is the one prop a non-button trigger cannot deal with itself: bits-ui
  // merges it in unconditionally for a <button>, and on a <span> or <div> there is
  // no valid value to write over it with. Removing it here keeps that knowledge in
  // the component that documents it, instead of in every caller.
  //
  // The copy MUST stay an object spread. bits-ui registers its trigger node through
  // an attachment stored under a SYMBOL key, which spread carries and any
  // Object.entries/keys/fromEntries rebuild silently drops. The hover handlers are
  // plain string keys, so the tooltip still opens without it — it just opens with no
  // registered trigger to anchor to, and jsdom lays nothing out, so no test in this
  // repo can see the difference. Correctness here is by construction, not by guard.
  function triggerProps(props: Record<string, unknown>): Record<string, unknown> {
    if (triggerElement === "button") return props;
    const rest = { ...props };
    delete rest.type;
    return rest;
  }
</script>

<!-- Mount this inside whatever context the `trigger` snippet needs to resolve — a
     bits-ui trigger reads its own root's context from its lexical scope, so this
     goes inside that <DropdownMenu.Root> / <Popover.Root> with the matching
     <*.Content> as its sibling. Only the Tooltip.* wrapping lives here.

     The markup below is deliberately jammed onto one line. This component splices
     the caller's trigger into the CALLER's flow, so every newline between these
     tags becomes a real text node there — invisible in a flex row, but corrupting
     in a `whitespace-pre` context such as the filter-token layer, which must
     reproduce the input's character stream glyph-for-glyph. Tooltip.Content is
     portaled, but the whitespace separating it from Tooltip.Trigger is not.

     That invariant is guarded from the caller's side: the Toolbar test asserting
     the exact `textContent` of the filter-token layer ("renders one hit-region per
     token in the interaction layer", Toolbar.test.ts) fails on any newline added
     between the tags below. -->
<Tooltip.Root><Tooltip.Trigger>{#snippet child({ props })}{@render trigger({ props: triggerProps(props) })}{/snippet}</Tooltip.Trigger><Tooltip.Content {side} aria-hidden={ariaHidden ? "true" : undefined}>{tooltip}</Tooltip.Content></Tooltip.Root>
