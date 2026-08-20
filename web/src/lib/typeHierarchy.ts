/**
 * Type hierarchy constraints for nib parent-child relationships.
 *
 * The tables are GENERATED from the Go rules (internal/nibtypes ValidChildTypes
 * and the rank derivation in internal/webvocab) into ./generated/vocabulary.ts,
 * so the web UI, TUI, and backend follow the one shared rule set — no curated
 * divergence, and a Go-side hierarchy change reaches here through
 * `task codegen` instead of a hand edit.
 */

export { TYPE_RANK, VALID_CHILD_TYPES } from "./generated/vocabulary";
import { TYPE_RANK, VALID_CHILD_TYPES } from "./generated/vocabulary";

/** Returns the list of valid child types for the given parent type. */
export function getValidChildTypes(parentType: string): readonly string[] {
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

/** Returns the container→leaf rank for a type (unknown/"" fall to leaf tier 0). */
export function typeRank(type: string): number {
  return TYPE_RANK[type] ?? 0;
}
