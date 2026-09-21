import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect } from "vitest";
import ConnectionStatus from "./ConnectionStatus.svelte";

describe("ConnectionStatus", () => {
  // A healthy socket is the overwhelmingly common case; a permanent "connected"
  // badge would be noise, and reserving layout for it would shift the header.
  it("renders nothing while the socket is up", () => {
    const { container } = render(ConnectionStatus, { props: { status: "connected" } });
    expect(container.textContent?.trim()).toBe("");
  });

  // Start-up is not a lost connection. Showing the chip here would flash it on
  // every cold load, training the user to ignore it.
  it("renders nothing during the initial connect", () => {
    const { container } = render(ConnectionStatus, { props: { status: "connecting" } });
    expect(container.textContent?.trim()).toBe("");
  });

  it("announces a dropped connection", () => {
    render(ConnectionStatus, { props: { status: "disconnected" } });
    expect(screen.getByTestId("connection-status")).toHaveTextContent(/reconnecting/i);
  });

  // The whole point is telling the user their view may be behind, so it must
  // reach assistive tech rather than being a purely visual cue.
  it("exposes the drop to assistive technology", () => {
    render(ConnectionStatus, { props: { status: "disconnected" } });
    const chip = screen.getByTestId("connection-status");
    expect(chip).toHaveAttribute("role", "status");
    expect(chip).toHaveAttribute("aria-live", "polite");
  });

  // A lone chip outside the table and the detail panel, so it carries its own
  // tooltip root rather than delegating to a container's.
  it("explains the drop through the app's styled tooltip, not a native title", async () => {
    const user = userEvent.setup();
    render(ConnectionStatus, { props: { status: "disconnected" } });
    const chip = screen.getByTestId("connection-status");

    await user.hover(chip);

    expect(
      await screen.findByText(/Live updates are disconnected/, {
        selector: '[data-slot="tooltip-content"]',
      })
    ).toBeInTheDocument();
    expect(chip).not.toHaveAttribute("title");
  });
});
