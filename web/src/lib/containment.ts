import type { TreeNib, TreeNode } from "./types";

/**
 * Who draws whom in one view tree — the relation reveal, ArrowLeft, subtree
 * expand/collapse and the drag's destination check all ask about, named once
 * instead of walked by hand at each of them.
 *
 * Read off `buildShapedViewTree`'s output rather than off the flattened rows, so
 * the collapse set never reaches it: a nib inside a collapsed section still has
 * a chain here. That is what lets it answer for reveal, whose subject has no row
 * by definition — `flatten` pushes a collapsed node's own row and none of its
 * children's.
 *
 * Distinct from `RowData.displayParentId`, which is this relation with the
 * display containers ELIDED so the value is always a backend-acceptable
 * `parentId`. A row inside a section answers `null` there and its section here.
 */
export interface ContainmentIndex {
  /**
   * The node whose `children` array holds `id`, or null at the display root —
   * and null too for an id this tree has no node for; `has` separates them.
   */
  containerOf(id: string): string | null;
  /** Whether this view tree has a node for `id` at all, rendered or not. */
  has(id: string): boolean;
  /** The containers enclosing `id`, innermost first, excluding `id`. Empty for a
   *  root and for an id this tree has no node for. */
  chainOf(id: string): string[];
  /** Whether `id` is anywhere inside `containerId`'s subtree, excluding
   *  `containerId` itself. O(depth). */
  contains(containerId: string, id: string): boolean;
  /** Every id under `id`, excluding it. Memoized; empty when absent. */
  descendantsOf(id: string): ReadonlySet<string>;
}

/** Index one view tree. */
export function buildContainmentIndex<T extends TreeNib>(tree: TreeNode<T>[]): ContainmentIndex {
  const container = new Map<string, string | null>();
  const children = new Map<string, string[]>();

  // First occurrence wins, and skipping the rest is also what bounds every walk
  // below: an id is recorded once, against a container recorded strictly before
  // it, so `container` is a forest however malformed the node graph is.
  // `children` gets no such property — it is written before the descent — so the
  // downward walk carries a visited set of its own.
  (function collect(nodes: readonly TreeNode<T>[], holder: string | null): void {
    for (const node of nodes) {
      const id = node.nib.id;
      if (container.has(id)) continue;
      container.set(id, holder);
      children.set(id, node.children.map((child) => child.nib.id));
      collect(node.children, id);
    }
  })(tree, null);

  /** Walks out of `id`, innermost container first, until `visit` says stop. */
  function climb(id: string, visit: (containerId: string) => boolean): void {
    let current = container.get(id) ?? null;
    while (current !== null) {
      if (visit(current)) return;
      current = container.get(current) ?? null;
    }
  }

  function contains(containerId: string, id: string): boolean {
    let found = false;
    climb(id, (current) => {
      if (current !== containerId) return false;
      found = true;
      return true;
    });
    return found;
  }

  const descendants = new Map<string, ReadonlySet<string>>();

  return {
    containerOf: (id) => container.get(id) ?? null,
    has: (id) => container.has(id),
    chainOf(id) {
      const chain: string[] = [];
      climb(id, (current) => {
        chain.push(current);
        return false;
      });
      return chain;
    },
    contains,
    descendantsOf(id) {
      const memo = descendants.get(id);
      if (memo !== undefined) return memo;
      const result = new Set<string>();
      const seen = new Set<string>([id]);
      const stack = [...(children.get(id) ?? [])];
      while (stack.length > 0) {
        const current = stack.pop()!;
        if (seen.has(current)) continue;
        seen.add(current);
        result.add(current);
        for (const child of children.get(current) ?? []) stack.push(child);
      }
      descendants.set(id, result);
      return result;
    },
  };
}
