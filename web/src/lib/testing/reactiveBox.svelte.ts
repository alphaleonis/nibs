/**
 * A mutable reactive cell, for a test that must push a value through a context
 * GETTER after mount. Context is written once at mount, so `rerender` cannot
 * reach it; in the app the getter closes over a `$derived` that a query fills in
 * later, and this is the smallest stand-in for that.
 */
export function reactiveBox<T>(initial: T) {
  let current = $state(initial);
  return {
    get value(): T {
      return current;
    },
    set value(next: T) {
      current = next;
    },
  };
}
