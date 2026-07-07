/**
 * Type hierarchy constraints for nib parent-child relationships.
 *
 * milestone -> [epic]
 * epic -> [feature, task, bug]
 * feature -> [task, bug]
 * task -> [] (leaf)
 * bug -> [] (leaf)
 */

export const VALID_CHILD_TYPES: Record<string, string[]> = {
  milestone: ["epic"],
  epic: ["feature", "task", "bug"],
  feature: ["task", "bug"],
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
