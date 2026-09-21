import { render, screen, waitFor } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import DelegatedTooltip from "./DelegatedTooltip.svelte";

const CONTENT = '[data-slot="tooltip-content"]';

let container: HTMLElement;

function cell(text: string, tooltip: string | null): HTMLElement {
  const el = document.createElement("div");
  el.textContent = text;
  if (tooltip !== null) el.setAttribute("data-tooltip", tooltip);
  container.appendChild(el);
  return el;
}

describe("DelegatedTooltip", () => {
  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
  });

  afterEach(() => {
    container.remove();
  });

  it("reveals the hovered element's data-tooltip in the app's styled tooltip", async () => {
    const user = userEvent.setup();
    const target = cell("User Authentica…", "User Authentication (tnib-e002)");
    render(DelegatedTooltip, { props: { container } });

    await user.hover(target);

    expect(
      await screen.findByText("User Authentication (tnib-e002)", { selector: CONTENT }),
    ).toBeInTheDocument();
  });

  it("serves every delegating element from one tooltip root", async () => {
    const user = userEvent.setup();
    const first = cell("one", "First (tnib-0001)");
    const second = cell("two", "Second (tnib-0002)");
    render(DelegatedTooltip, { props: { container } });

    await user.hover(first);
    await screen.findByText("First (tnib-0001)", { selector: CONTENT });
    await user.hover(second);
    await screen.findByText("Second (tnib-0002)", { selector: CONTENT });

    // One root, so the first element's text is gone rather than left behind.
    expect(screen.queryByText("First (tnib-0001)", { selector: CONTENT })).not.toBeInTheDocument();
    expect(document.querySelectorAll(CONTENT)).toHaveLength(1);
  });

  it("stays shut for an element that carries no tooltip", async () => {
    const user = userEvent.setup();
    const bare = cell("plain", null);
    const target = cell("has one", "Something (tnib-0003)");
    render(DelegatedTooltip, { props: { container } });

    await user.hover(target);
    await screen.findByText("Something (tnib-0003)", { selector: CONTENT });

    await user.hover(bare);
    await waitFor(() => expect(document.querySelector(CONTENT)).toBeNull());
  });

  it("closes when the container scrolls, which moves the anchored element", async () => {
    const user = userEvent.setup();
    const target = cell("scrolls away", "Scrolled (tnib-0004)");
    render(DelegatedTooltip, { props: { container } });

    await user.hover(target);
    await screen.findByText("Scrolled (tnib-0004)", { selector: CONTENT });

    container.dispatchEvent(new Event("scroll"));
    await waitFor(() => expect(document.querySelector(CONTENT)).toBeNull());
  });

  // `scroll` does not bubble, and the container is not always the scroller:
  // ActiveNibView delegates from `.anv`, which is `overflow: hidden`, while
  // `.anv-body` and `.anv-rail` scroll under it and hold tooltip bearers.
  it("closes when a scroller nested inside the container scrolls", async () => {
    const user = userEvent.setup();
    const scroller = document.createElement("div");
    container.appendChild(scroller);
    const target = document.createElement("div");
    target.textContent = "in a nested scroller";
    target.setAttribute("data-tooltip", "Nested (tnib-0005)");
    scroller.appendChild(target);
    render(DelegatedTooltip, { props: { container } });

    await user.hover(target);
    await screen.findByText("Nested (tnib-0005)", { selector: CONTENT });

    scroller.dispatchEvent(new Event("scroll"));
    await waitFor(() => expect(document.querySelector(CONTENT)).toBeNull());
  });

  // A re-render — a filter keystroke, a subscription push — removes the hovered
  // row with the pointer still on it, and no pointer event reports that.
  it("closes when the element it is anchored to is removed", async () => {
    const user = userEvent.setup();
    const target = cell("about to go", "Doomed (tnib-0006)");
    render(DelegatedTooltip, { props: { container } });

    await user.hover(target);
    await screen.findByText("Doomed (tnib-0006)", { selector: CONTENT });

    target.remove();
    await waitFor(() => expect(document.querySelector(CONTENT)).toBeNull());
  });

  it("does not open for an element removed during the dwell", async () => {
    const user = userEvent.setup();
    const target = cell("about to go", "Doomed mid-dwell (tnib-0007)");
    render(DelegatedTooltip, { props: { container, delay: 20 } });

    await user.hover(target);
    target.remove();

    // Well past the dwell: a detached node's rect is all zeros, so opening would
    // put the popover in the viewport's corner.
    await new Promise((resolve) => setTimeout(resolve, 80));
    expect(document.querySelector(CONTENT)).toBeNull();
  });

  it("reopens on the same element after Escape dismissed it", async () => {
    const user = userEvent.setup();
    const target = cell("press escape", "Escaped (tnib-0008)");
    render(DelegatedTooltip, { props: { container } });

    await user.hover(target);
    await screen.findByText("Escaped (tnib-0008)", { selector: CONTENT });

    await user.keyboard("{Escape}");
    await waitFor(() => expect(document.querySelector(CONTENT)).toBeNull());

    // Dispatched rather than driven by the pointer: the point is a fresh
    // `pointerover` on the same element with no intervening leave, which is what
    // moving into a child span produces — and what the identity short-circuit
    // would swallow while `hovered` is stale.
    target.dispatchEvent(new Event("pointerover", { bubbles: true }));
    expect(await screen.findByText("Escaped (tnib-0008)", { selector: CONTENT })).toBeInTheDocument();
  });

  it("stays shut while the caller reports a drag in flight", async () => {
    const user = userEvent.setup();
    const target = cell("mid-drag", "Dragged over (tnib-0009)");
    let dragging = true;
    render(DelegatedTooltip, { props: { container, delay: 20, suppressed: () => dragging } });

    await user.hover(target);
    await new Promise((resolve) => setTimeout(resolve, 80));
    expect(document.querySelector(CONTENT)).toBeNull();

    dragging = false;
    target.dispatchEvent(new Event("pointerover", { bubbles: true }));
    expect(
      await screen.findByText("Dragged over (tnib-0009)", { selector: CONTENT }),
    ).toBeInTheDocument();
  });

  // bits-ui's trigger defaults `type="button"`, which is not valid on the <div>
  // the anchor renders as.
  it("renders the anchor without bits-ui's button type", () => {
    render(DelegatedTooltip, { props: { container } });

    expect(screen.getByTestId("delegated-tooltip-anchor")).not.toHaveAttribute("type");
  });
});
