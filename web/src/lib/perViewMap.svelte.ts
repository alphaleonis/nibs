import type { ViewLevel } from "./types";

// "auto": tracked by Preferences' auto-save $effect, so a change saves at once.
// "flush": untracked; saves only when flush() is called (column-width drags).
export type SaveMode = "auto" | "flush";

/** What a persisted slice declares. */
export interface PerViewPersistence {
  // This slice's field name in the `nibs-filter-preferences` blob. Informational:
  // Preferences serializes each slice under a literal field name.
  storageKey: string;
  saveMode: SaveMode;
}

export interface PerViewMapOpts<T, R = T> {
  // Returned by resolve() when a level has no stored value.
  defaultValue: R;
  // Combine a level's stored value with the default: merge (widths) or replace
  // (visibility, order). An object or array R must come back FRESH — one
  // `defaultValue` serves every unset level, so returning it by identity lets a
  // mutation through one level reach the others. For a primitive R,
  // `stored ?? dflt` is fine.
  resolve: (stored: T | undefined, dflt: R) => R;
  /** Omit for an ephemeral slice, never written to localStorage. */
  persistence?: PerViewPersistence & {
    // Injected so this module does not depend on Preferences.
    requestSave: () => void;
  };
}

/**
 * A slice known to persist. `PerViewMap.persistence` is optional, so type a list
 * of saving slices with this to refuse an ephemeral instance.
 */
export type PersistedPerViewMap<T, R = T> = PerViewMap<T, R> & {
  readonly persistence: PerViewPersistence;
};

/** Construct a slice that persists; the required `persistence` makes the cast sound. */
export function persistedPerViewMap<T, R = T>(
  opts: PerViewMapOpts<T, R> & { persistence: NonNullable<PerViewMapOpts<T, R>["persistence"]> },
): PersistedPerViewMap<T, R> {
  return new PerViewMap(opts) as PersistedPerViewMap<T, R>;
}

// A store keyed by view level, with the default, resolve combinator and save
// timing injected.
export class PerViewMap<T, R = T> {
  #map: Partial<Record<ViewLevel, T>> = $state({});
  #defaultValue: R;
  #resolve: (stored: T | undefined, dflt: R) => R;
  #requestSave: (() => void) | undefined;
  // `requestSave` is not exposed; call it through flush().
  readonly persistence: PerViewPersistence | undefined;

  constructor(opts: PerViewMapOpts<T, R>) {
    this.#defaultValue = opts.defaultValue;
    this.#resolve = opts.resolve;
    this.persistence = opts.persistence
      ? { storageKey: opts.persistence.storageKey, saveMode: opts.persistence.saveMode }
      : undefined;
    this.#requestSave = opts.persistence?.requestSave;
  }

  // Seed from loaded preferences; undefined means empty.
  hydrate(parsed: Partial<Record<ViewLevel, T>> | undefined): void {
    this.#map = parsed ?? {};
  }

  // Reactive when called inside $derived.
  resolve(level: ViewLevel): R {
    return this.#resolve(this.#map[level], this.#defaultValue);
  }

  // The raw stored value, without the default.
  peek(level: ViewLevel): T | undefined {
    return this.#map[level];
  }

  updateLevel(level: ViewLevel, updater: (current: T | undefined) => T): void {
    this.#map = { ...this.#map, [level]: updater(this.#map[level]) };
  }

  setLevel(level: ViewLevel, value: T): void {
    this.#map = { ...this.#map, [level]: value };
  }

  // Persist now. No-op on an ephemeral slice.
  flush(): void {
    this.#requestSave?.();
  }

  // Subscribes the calling $effect to the map. Call only for "auto" slices.
  track(): void {
    void this.#map;
  }

  serialize(): Partial<Record<ViewLevel, T>> {
    return this.#map;
  }
}
