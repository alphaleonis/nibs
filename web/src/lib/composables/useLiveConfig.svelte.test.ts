import { describe, it, expect, vi, afterEach } from "vitest";
import { flushSync } from "svelte";
import { useLiveConfig, CONFIG_RETRY_DELAYS } from "./useLiveConfig.svelte";
import type { LiveConfig } from "./useLiveConfig.svelte";

type Cfg = { areas: string[] };

let dispose: (() => void) | undefined;
afterEach(() => {
  dispose?.();
  dispose = undefined;
});

/** A controllable stand-in for the query store, the subscription and the clock. */
function mount(initial: { queried?: Cfg; pushed?: Cfg; error?: unknown; fetching?: boolean } = {}) {
  const state = $state({
    queried: initial.queried,
    pushed: initial.pushed,
    error: initial.error,
    fetching: initial.fetching ?? false,
  });
  const reask = vi.fn();
  const timers: { fn: () => void; ms: number; canceled: boolean }[] = [];

  let live!: LiveConfig<Cfg>;
  dispose = $effect.root(() => {
    live = useLiveConfig<Cfg>({
      queried: () => state.queried,
      pushed: () => state.pushed,
      error: () => state.error,
      fetching: () => state.fetching,
      reask,
      schedule: (fn, ms) => {
        timers.push({ fn, ms, canceled: false });
        return timers.length - 1;
      },
      cancel: (handle) => {
        const t = timers[handle as number];
        if (t) t.canceled = true;
      },
    });
  });
  flushSync();

  /** Run the newest live timer, as the clock would. */
  const fireTimer = () => {
    const t = [...timers].reverse().find((x) => !x.canceled);
    if (!t) throw new Error("no live timer scheduled");
    t.canceled = true;
    t.fn();
    flushSync();
  };
  const liveTimers = () => timers.filter((t) => !t.canceled);
  const set = (patch: Partial<typeof state>) => {
    Object.assign(state, patch);
    flushSync();
  };

  return { live, reask, fireTimer, liveTimers, timers, set };
}

describe("useLiveConfig", () => {
  it("prefers a pushed config over the queried one", () => {
    const { live } = mount({ queried: { areas: ["web"] }, pushed: { areas: ["frontend"] } });
    expect(live.config).toEqual({ areas: ["frontend"] });
  });

  it("answers from the query while nothing has been pushed", () => {
    const { live } = mount({ queried: { areas: ["web"] } });
    expect(live.config).toEqual({ areas: ["web"] });
  });

  // The latch. Without it an unconditional re-ask cannot be made safe: urql
  // drops `data` on a failed network-only re-execution (measured), so a re-ask
  // that failed would take away a vocabulary the session already had.
  it("keeps the last good config when a later re-ask fails", () => {
    const { live, set } = mount({ queried: { areas: ["web"] } });
    expect(live.config).toEqual({ areas: ["web"] });

    set({ queried: undefined, error: new Error("502") });

    expect(live.config).toEqual({ areas: ["web"] });
    expect(live.unavailable).toBe(false);
  });

  it("is unavailable only when nothing has ever answered and the query failed", () => {
    const { live, set } = mount({});
    expect(live.unavailable).toBe(false); // still in flight

    set({ error: new Error("502") });
    expect(live.unavailable).toBe(true);

    set({ queried: { areas: ["web"] }, error: undefined });
    expect(live.unavailable).toBe(false);
  });

  // The bug: a config query that failed over HTTP was never re-asked, because
  // the only re-ask was gated on the WEBSOCKET recovering (nibs-zwnm).
  it("re-asks on its own after a failure, backing off", () => {
    const { reask, fireTimer, liveTimers, set } = mount({ error: new Error("502") });

    expect(liveTimers()[0].ms).toBe(CONFIG_RETRY_DELAYS[0]);
    fireTimer();
    expect(reask).toHaveBeenCalledTimes(1);

    // The re-ask fails too, so the next attempt waits longer.
    set({ fetching: false, error: new Error("502 again") });
    expect(liveTimers()[0].ms).toBe(CONFIG_RETRY_DELAYS[1]);
  });

  it("stops re-asking after the last delay, rather than hammering forever", () => {
    const { reask, fireTimer, liveTimers, set } = mount({ error: new Error("502") });

    for (let i = 0; i < CONFIG_RETRY_DELAYS.length; i++) {
      fireTimer();
      set({ error: new Error(`502 #${i}`) });
    }

    expect(reask).toHaveBeenCalledTimes(CONFIG_RETRY_DELAYS.length);
    expect(liveTimers()).toHaveLength(0);
  });

  it("does not stack a re-ask on top of one already in flight", () => {
    const { liveTimers, set } = mount({ error: new Error("502") });
    expect(liveTimers()).toHaveLength(1);

    set({ fetching: true });
    expect(liveTimers()).toHaveLength(0);
  });

  it("gives a later failure a fresh budget once an answer arrives", () => {
    const { reask, fireTimer, liveTimers, set } = mount({ error: new Error("502") });
    for (let i = 0; i < CONFIG_RETRY_DELAYS.length; i++) {
      fireTimer();
      set({ error: new Error(`502 #${i}`) });
    }
    expect(liveTimers()).toHaveLength(0);

    set({ queried: { areas: ["web"] }, error: undefined });
    set({ queried: undefined, error: new Error("later 502") });

    expect(liveTimers()[0].ms).toBe(CONFIG_RETRY_DELAYS[0]);
    fireTimer();
    expect(reask).toHaveBeenCalledTimes(CONFIG_RETRY_DELAYS.length + 1);
  });

  // The manual escape, and the one App calls on socket recovery: it re-asks now
  // and restores the budget, so a session that exhausted it is not stuck.
  it("retry() re-asks immediately and restores the budget", () => {
    const { live, reask, fireTimer, liveTimers, set } = mount({ error: new Error("502") });
    for (let i = 0; i < CONFIG_RETRY_DELAYS.length; i++) {
      fireTimer();
      set({ error: new Error(`502 #${i}`) });
    }
    expect(liveTimers()).toHaveLength(0);
    reask.mockClear();

    live.retry();
    flushSync();

    expect(reask).toHaveBeenCalledTimes(1);
    expect(liveTimers()[0].ms).toBe(CONFIG_RETRY_DELAYS[0]);
  });
});
