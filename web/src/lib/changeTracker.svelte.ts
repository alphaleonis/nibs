export class NibChangeTracker {
  #highlightDurationMs: number;
  #fadeDurationMs: number;
  #highlightedIds: Set<string> = $state(new Set());
  #fadingIds: Set<string> = $state(new Set());
  #timers = new Map<string, ReturnType<typeof setTimeout>>();

  constructor(opts?: { highlightDurationMs?: number; fadeDurationMs?: number }) {
    this.#highlightDurationMs = opts?.highlightDurationMs ?? 1000;
    this.#fadeDurationMs = opts?.fadeDurationMs ?? 500;
  }

  handleEvent(event: { type: string; nibId: string }): void {
    if (event.type === "updated" || event.type === "created" || event.type === "unarchived") {
      // An unarchive moves the nib back to its main path and changes its row, so
      // it reads as an update (highlight), not a removal (fade).
      this.#setHighlighted(event.nibId);
    } else if (event.type === "deleted") {
      this.#setFading(event.nibId);
    } else if (event.type === "archived") {
      // Deliberately no fade: archiving does not take the nib out of the tree.
      // It stays in the store at its new path and keeps being returned by the
      // list query, so fading the row would play an exit the row never makes —
      // it would drop to invisible and then pop back.
    } else if (import.meta.env.DEV) {
      console.warn(`NibChangeTracker: unhandled event type "${event.type}"`);
    }
  }

  isHighlighted(id: string): boolean {
    return this.#highlightedIds.has(id);
  }

  isFading(id: string): boolean {
    return this.#fadingIds.has(id);
  }

  destroy(): void {
    for (const timer of this.#timers.values()) {
      clearTimeout(timer);
    }
    this.#timers.clear();
    this.#highlightedIds = new Set();
    this.#fadingIds = new Set();
  }

  #setHighlighted(id: string): void {
    const timerKey = `highlight:${id}`;
    const existing = this.#timers.get(timerKey);
    if (existing) clearTimeout(existing);

    this.#highlightedIds = new Set([...this.#highlightedIds, id]);

    const timer = setTimeout(() => {
      const next = new Set(this.#highlightedIds);
      next.delete(id);
      this.#highlightedIds = next;
      this.#timers.delete(timerKey);
    }, this.#highlightDurationMs);

    this.#timers.set(timerKey, timer);
  }

  #setFading(id: string): void {
    const timerKey = `fade:${id}`;
    const existing = this.#timers.get(timerKey);
    if (existing) clearTimeout(existing);

    this.#fadingIds = new Set([...this.#fadingIds, id]);

    const timer = setTimeout(() => {
      const next = new Set(this.#fadingIds);
      next.delete(id);
      this.#fadingIds = next;
      this.#timers.delete(timerKey);
    }, this.#fadeDurationMs);

    this.#timers.set(timerKey, timer);
  }

  get fadeDurationMs(): number {
    return this.#fadeDurationMs;
  }
}
