import type { ViewLevel } from "./types";

// How a per-view slice persists. "auto" instances are tracked by the Preferences
// auto-save $effect (a change writes localStorage immediately); "flush" instances
// are deliberately NOT tracked and persist only when flush() is called (the
// pointerup pattern used by column-width drags).
export type SaveMode = "auto" | "flush";

/**
 * What a PERSISTED slice declares. One type rather than two fields so the pair
 * travels together everywhere it is read: a storage key with no save mode saves
 * nothing, and a save mode with no key has nowhere to go.
 */
export interface PerViewPersistence {
  // Serialized field name in the `nibs-filter-preferences` blob. Documents which
  // slice this instance owns; Preferences reads/writes it by that name.
  storageKey: string;
  saveMode: SaveMode;
}

export interface PerViewMapOpts<T, R = T> {
  // The default returned by resolve() when a view level has no stored value.
  defaultValue: R;
  // Combine the stored value for a level with the default. MERGE (widths overlay
  // the full default) or REPLACE (visibility/order use the stored value whole).
  //
  // Whenever R is OBJECT- or ARRAY-valued this must return a FRESH value: one
  // `defaultValue` is shared by every unset level, so handing it back by identity
  // makes `resolve("flat") === resolve("milestones")` and a mutation through one
  // level's result silently reaches all five. A PRIMITIVE R has nothing to alias,
  // so `stored ?? dflt` is a legitimate combinator there and only there.
  resolve: (stored: T | undefined, dflt: R) => R;
  /**
   * Omit for an EPHEMERAL slice — one that lives only as long as the tab and is
   * never written to localStorage. Grouped rather than three independent
   * optionals so the incoherent combinations (a storage key with no way to
   * request a save, a save mode nothing ever saves) cannot be expressed.
   */
  persistence?: PerViewPersistence & {
    // Closure into Preferences.save() — passed in rather than imported so the
    // primitive has no dependency on the persistence layer.
    requestSave: () => void;
  };
}

/**
 * A slice that is KNOWN to persist. Every `PerViewMap` shares one declared type,
 * in which `persistence` is optional — so an auto-save list typed on the class
 * alone is structurally satisfied by an ephemeral instance that can never save,
 * and enrolling one is a silent no-op rather than a compile error. Naming the
 * narrowed shape is what lets such a list refuse it.
 */
export type PersistedPerViewMap<T, R = T> = PerViewMap<T, R> & {
  readonly persistence: PerViewPersistence;
};

/**
 * Construct a slice that persists. The parameter type REQUIRES the persistence
 * group, which is what makes the narrowed return type true — the assertion below
 * asserts nothing the caller was not forced to supply.
 */
export function persistedPerViewMap<T, R = T>(
  opts: PerViewMapOpts<T, R> & { persistence: NonNullable<PerViewMapOpts<T, R>["persistence"]> },
): PersistedPerViewMap<T, R> {
  return new PerViewMap(opts) as PersistedPerViewMap<T, R>;
}

// One reusable per-ViewLevel map primitive: a level-keyed store whose only
// genuine per-instance differences (default, resolve combinator, save timing)
// are injected, so every slice keyed by view level shares one implementation of
// the map plumbing. Slices may be persisted (the table's column visibility,
// widths and order) or ephemeral (the tree table's per-view scroll offsets); the
// persisted ones are the reason `persistence` exists at all. No column knowledge
// is imported here — this module depends only on ViewLevel.
export class PerViewMap<T, R = T> {
  #map: Partial<Record<ViewLevel, T>> = $state({});
  #defaultValue: R;
  #resolve: (stored: T | undefined, dflt: R) => R;
  #requestSave: (() => void) | undefined;
  // The pair or nothing, mirroring the opts group: exposing the two fields
  // independently would let the surface a consumer reads describe a storage key
  // with no save mode, which construction has never been able to produce.
  // `requestSave` is deliberately NOT re-exposed — it is this primitive's own
  // wire back into Preferences.save(), reachable only through flush().
  readonly persistence: PerViewPersistence | undefined;

  constructor(opts: PerViewMapOpts<T, R>) {
    this.#defaultValue = opts.defaultValue;
    this.#resolve = opts.resolve;
    this.persistence = opts.persistence
      ? { storageKey: opts.persistence.storageKey, saveMode: opts.persistence.saveMode }
      : undefined;
    this.#requestSave = opts.persistence?.requestSave;
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

  // Persist now (flush-mode instances, on pointerup). Inert on an ephemeral
  // instance: there is nothing to persist to.
  flush(): void {
    this.#requestSave?.();
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
