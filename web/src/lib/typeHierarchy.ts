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
