import type { ViewLevel } from "./types";

// How a per-view slice persists. "auto" instances are tracked by the Preferences
// auto-save $effect (a change writes localStorage immediately); "flush" instances
// are deliberately NOT tracked and persist only when flush() is called (the
// pointerup pattern used by column-width drags).
export type SaveMode = "auto" | "flush";

export interface PerViewColumnMapOpts<T, R = T> {
  // Serialized field name in the `nibs-filter-preferences` blob. Documents which
  // slice this instance owns; Preferences reads/writes the field by that name.
  storageKey: string;
  // The default returned by resolve() when a view level has no stored value.
  defaultValue: R;
  // Combine the stored value for a level with the default. MERGE (widths overlay
  // the full default) or REPLACE (visibility/order use the stored value whole).
  // Must return a fresh value so callers never mutate shared state.
  resolve: (stored: T | undefined, dflt: R) => R;
  saveMode: SaveMode;
  // Closure into Preferences.save() — passed in rather than imported so the
  // primitive has no dependency on the persistence layer.
  requestSave: () => void;
}

// One reusable per-ViewLevel map primitive. The web table's per-view column
// state (visibility, widths, and — via nibs-46c1 — order) are separate reactive
// slices sharing this single implementation of the map plumbing. The concern's
// only genuine differences (default, resolve combinator, per-level validator,
// save timing) are injected per instance; column knowledge is never imported
// here — this module depends only on ViewLevel.
export class PerViewColumnMap<T, R = T> {
  #map: Partial<Record<ViewLevel, T>> = $state({});
  #defaultValue: R;
  #resolve: (stored: T | undefined, dflt: R) => R;
  #requestSave: () => void;
  readonly storageKey: string;
  readonly saveMode: SaveMode;

  constructor(opts: PerViewColumnMapOpts<T, R>) {
    this.storageKey = opts.storageKey;
    this.#defaultValue = opts.defaultValue;
    this.#resolve = opts.resolve;
    this.saveMode = opts.saveMode;
    this.#requestSave = opts.requestSave;
  }

  // Seed the map from loadPreferences (undefined ⇒ empty).
  hydrate(parsed: Partial<Record<ViewLevel, T>> | undefined): void {
    this.#map = parsed ?? {};
  }

  // The old $derived body: the stored value for a level combined with the
  // default via the injected combinator. Reactive when called inside $derived.
  resolve(level: ViewLevel): R {
    return this.#resolve(this.#map[level], this.#defaultValue);
  }

  // The raw stored value for a level (no default), for imperative reads.
  peek(level: ViewLevel): T | undefined {
    return this.#map[level];
  }

  // Immutable outer-map rebuild, in one place. The updater receives the current
  // stored value for the level (possibly undefined) and returns the next one.
  updateLevel(level: ViewLevel, updater: (current: T | undefined) => T): void {
    this.#map = { ...this.#map, [level]: updater(this.#map[level]) };
  }

  setLevel(level: ViewLevel, value: T): void {
    this.#map = { ...this.#map, [level]: value };
  }

  // Persist now (flush-mode instances, on pointerup).
  flush(): void {
    this.#requestSave();
  }

  // Void-read the map so a caller running inside the auto-save $effect subscribes
  // to it. Only "auto" instances are tracked this way; "flush" instances are
  // never subscribed, so their mutations don't trigger auto-save.
  track(): void {
    void this.#map;
  }

  serialize(): Partial<Record<ViewLevel, T>> {
    return this.#map;
  }
}
