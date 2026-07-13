/**
 * Type hierarchy constraints for nib parent-child relationships.
 *
 * milestone -> [epic]
 * epic -> [feature, task, bug]
 * feature -> [task]
 * task -> [] (leaf)
 * bug -> [] (leaf)
 *
 * This is a curated (opinionated) subset of what the backend permits — e.g. the
 * backend allows epic/feature/bug/task directly under a milestone, but the UI
 * steers toward milestone -> epic -> feature -> task. Every entry here must still
 * be a SUBSET of the backend's rules (internal/nibtypes/hierarchy.go): a bug's
 * only valid parents are milestone and epic, so a bug must NOT appear under a
 * feature (the backend rejects it with a HIERARCHY error).
 */

export const VALID_CHILD_TYPES: Record<string, string[]> = {
  milestone: ["epic"],
  epic: ["feature", "task", "bug"],
  feature: ["task"],
  task: [],
  bug: [],
};

/** Returns the list of valid child types for the given parent type. */
export function getValidChildTypes(parentType: string): string[] {
  return VALID_CHILD_TYPES[parentType] ?? [];
}

/** Returns true if the type is a leaf type (cannot have children). */
export function isLeafType(type: string): boolean {
  const children = VALID_CHILD_TYPES[type];
  return children !== undefined && children.length === 0;
}

/** Returns true if the type can have children. */
export function canHaveChildren(type: string): boolean {
  const children = VALID_CHILD_TYPES[type];
  return children !== undefined && children.length > 0;
}

/** Container→leaf rank. Higher = more container-like. Unknown/"" = leaf tier (0). */
export const TYPE_RANK: Record<string, number> = {
  milestone: 3,
  epic: 2,
  feature: 1,
  bug: 1,
  task: 0,
  research: 0,
};

/** Returns the container→leaf rank for a type (unknown/"" fall to leaf tier 0). */
export function typeRank(type: string): number {
  return TYPE_RANK[type] ?? 0;
}
