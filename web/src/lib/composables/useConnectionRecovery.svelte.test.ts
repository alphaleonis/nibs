import { describe, it, expect, vi, afterEach } from "vitest";
import { flushSync } from "svelte";
import { useConnectionRecovery } from "./useConnectionRecovery.svelte";
import type { ConnectionRecovery } from "../connectionRecovery";

let dispose: (() => void) | undefined;
afterEach(() => {
  dispose?.();
  dispose = undefined;
  setVisibility("visible");
});

function setVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, "visibilityState", { value: state, configurable: true });
}

function mount() {
  const reconnect = vi.fn();
  let recovery!: ConnectionRecovery;
  dispose = $effect.root(() => {
    recovery = useConnectionRecovery({ reconnect });
  });
  flushSync();
  return { recovery, reconnect };
}

/** A pageshow whose `persisted` flag marks a back/forward-cache restore. */
function firePageshow(persisted: boolean) {
  window.dispatchEvent(new PageTransitionEvent("pageshow", { persisted }));
}

describe("useConnectionRecovery", () => {
  it("reconnects when the page is restored from the back/forward cache", () => {
    const { recovery, reconnect } = mount();
    recovery.onConnected();
    firePageshow(true);
    expect(reconnect).toHaveBeenCalledTimes(1);
  });

  // A normal load also fires pageshow; only the persisted flag marks the restore
  // that froze the socket. Reconnecting on every load would drop a socket that
  // has just been established.
  it("ignores a pageshow from an ordinary load", () => {
    const { recovery, reconnect } = mount();
    recovery.onConnected();
    firePageshow(false);
    expect(reconnect).not.toHaveBeenCalled();
  });

  it("reconnects when the browser regains connectivity", () => {
    const { recovery, reconnect } = mount();
    recovery.onConnected();
    window.dispatchEvent(new Event("online"));
    expect(reconnect).toHaveBeenCalledTimes(1);
  });

  it("reconnects when the tab becomes visible while the socket is down", () => {
    const { recovery, reconnect } = mount();
    recovery.onConnected();
    recovery.onClosed();
    setVisibility("visible");
    document.dispatchEvent(new Event("visibilitychange"));
    expect(reconnect).toHaveBeenCalledTimes(1);
  });

  it("ignores the tab being hidden", () => {
    const { recovery, reconnect } = mount();
    recovery.onConnected();
    recovery.onClosed();
    setVisibility("hidden");
    document.dispatchEvent(new Event("visibilitychange"));
    expect(reconnect).not.toHaveBeenCalled();
  });

  it("exposes the socket status for the UI", () => {
    const { recovery } = mount();
    expect(recovery.status).toBe("connecting");
    recovery.onConnected();
    expect(recovery.status).toBe("connected");
    recovery.onClosed();
    expect(recovery.status).toBe("disconnected");
  });

  it("detaches its listeners when the scope is torn down", () => {
    const { recovery, reconnect } = mount();
    recovery.onConnected();
    dispose?.();
    dispose = undefined;
    firePageshow(true);
    window.dispatchEvent(new Event("online"));
    expect(reconnect).not.toHaveBeenCalled();
  });
});
