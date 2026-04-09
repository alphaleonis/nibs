import type { TreeNib, TreeNode, ViewLevel } from "./types";

export function buildTree<T extends TreeNib>(nibs: T[]): TreeNode<T>[] {
  const nodeMap = new Map<string, TreeNode<T>>();
  const roots: TreeNode<T>[] = [];

  // First pass: create all nodes
  for (const nib of nibs) {
    nodeMap.set(nib.id, { nib, children: [], depth: 0 });
  }

  // Second pass: link children to parents
  for (const nib of nibs) {
    const node = nodeMap.get(nib.id)!;
    if (nib.parentId !== null && nodeMap.has(nib.parentId)) {
      const parent = nodeMap.get(nib.parentId)!;
      parent.children.push(node);
    } else {
      roots.push(node);
    }
  }

  // Third pass: compute depths via recursive traversal
  setDepths(roots, 0);

  return roots;
}

function setDepths<T extends TreeNib>(nodes: TreeNode<T>[], depth: number): void {
  for (const node of nodes) {
    node.depth = depth;
    setDepths(node.children, depth + 1);
  }
}

const VIEW_LEVEL_ROOT_TYPES: Record<ViewLevel, string[]> = {
  milestones: ["milestone"],
  epics: ["epic"],
  backlog: ["feature", "bug"],
};

export function buildViewTree<T extends TreeNib>(nibs: T[], viewLevel: ViewLevel): TreeNode<T>[] {
  const fullTree = buildTree(nibs);
  const rootTypes = VIEW_LEVEL_ROOT_TYPES[viewLevel];
  const roots: TreeNode<T>[] = [];

  if (viewLevel === "backlog") {
    // Backlog view: features/bugs become roots with only task children.
    // Non-task children (nested features/bugs) become separate roots.
    // Tasks without a feature/bug parent go under a virtual "Unparented" node.
    const orphanTasks: TreeNode<T>[] = [];

    function collectBacklogRoots(nodes: TreeNode<T>[]): void {
      for (const node of nodes) {
        if (rootTypes.includes(node.nib.type)) {
          // Filter children to only tasks
          const taskChildren = node.children.filter(c => c.nib.type === "task");
          const nonTaskChildren = node.children.filter(c => c.nib.type !== "task");
          // Create a new node instead of mutating shared tree references
          roots.push({ nib: node.nib, children: taskChildren, depth: 0 });
          // Recurse into non-task children to find more features/bugs
          collectBacklogRoots(nonTaskChildren);
        } else if (node.nib.type === "task") {
          // Task not under a feature/bug — orphaned
          orphanTasks.push({ nib: node.nib, children: [], depth: 0 });
        } else {
          collectBacklogRoots(node.children);
        }
      }
    }

    collectBacklogRoots(fullTree);

    if (orphanTasks.length > 0) {
      // NOTE: This virtual nib must include all fields that T may require.
      // When T is TreeTableNib, blockingIds/blockedByIds are needed by TreeTableRow.
      // If T gains new required fields, they must be added here too.
      const virtualNib = {
        id: "__unparented__",
        title: "Unparented",
        status: "",
        type: "",
        priority: "",
        estimate: "",
        tags: [],
        updatedAt: "",
        parentId: null,
        blockingIds: [],
        blockedByIds: [],
      } as T;

      roots.push({
        nib: virtualNib,
        children: orphanTasks,
        depth: 0,
      });
    }
  } else {
    // Milestones/epics view: root types become roots with full subtrees
    function collectRoots(nodes: TreeNode<T>[]): void {
      for (const node of nodes) {
        if (rootTypes.includes(node.nib.type)) {
          roots.push(node);
        } else {
          collectRoots(node.children);
        }
      }
    }

    collectRoots(fullTree);
  }

  // Reset depths relative to new roots
  setDepths(roots, 0);

  return roots;
}
