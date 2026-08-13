import {
  createConnectionRecovery,
  type ConnectionRecovery,
  type ConnectionStatus,
} from "../connectionRecovery";

/**
 * Binds the browser events that mean "the page is back" to the pure recovery
 * policy in `connectionRecovery.ts`, and republishes its status as reactive
 * state so the UI can show a disconnected indicator.
 *
 * `pageshow` is the one that matters for the reported bug (nibs-1seo): it is the
 * only signal a back/forward-cache restore emits, and `event.persisted`
 * distinguishes that restore — where the socket was closed on freeze while the
 * client's own state was preserved — from an ordinary load.
 */
export function useConnectionRecovery(ports: { reconnect: () => void }): ConnectionRecovery {
  let status = $state<ConnectionStatus>("connecting");

  const core = createConnectionRecovery({
    reconnect: ports.reconnect,
    scheduleDeferred: (fn, ms) => setTimeout(fn, ms),
    cancelDeferred: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
  });

  /** Mirror the core's status into reactive state after any transition. */
  const sync = <T extends unknown[]>(fn: (...args: T) => void) => (...args: T) => {
    fn(...args);
    status = core.status;
  };

  const onConnected = sync(() => core.onConnected());
  const onClosed = sync(() => core.onClosed());
  const onResume = sync((reason: Parameters<ConnectionRecovery["onResume"]>[0]) =>
    core.onResume(reason),
  );

  $effect(() => {
    const onPageshow = (e: PageTransitionEvent) => {
      if (e.persisted) onResume("pageshow-restored");
    };
    const onOnline = () => onResume("online");
    const onVisibility = () => {
      if (document.visibilityState === "visible") onResume("visible");
    };

    window.addEventListener("pageshow", onPageshow);
    window.addEventListener("online", onOnline);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      window.removeEventListener("pageshow", onPageshow);
      window.removeEventListener("online", onOnline);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  });

  return {
    get status() {
      return status;
    },
    onConnected,
    onClosed,
    onResume,
    onRecovered: (listener) => core.onRecovered(listener),
  };
}
