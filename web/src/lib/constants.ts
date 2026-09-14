// The vocabulary is generated from Go into ./generated/vocabulary.ts (`task
// codegen`). This module derives presentation orders and role groupings from it;
// do not restate a generated name here.
import {
  ESTIMATE_DEFS,
  STATUS_DEFS,
  STATUS_GROUP_CLOSED,
  STATUS_GROUP_OPEN,
  STATUS_WORKFLOW_ORDER,
  type StatusRole,
} from "./generated/vocabulary";

export { PRIORITIES, STATUS_WORKFLOW_ORDER, TYPES } from "./generated/vocabulary";
export type { StatusRole } from "./generated/vocabulary";

// Exhaustive with a `never` default: a new role from regeneration fails
// type-checking here until it is classified.
export function roleIsClosed(role: StatusRole): boolean {
  switch (role) {
    case "open":
    case "startable":
      return false;
    case "parked":
    case "done":
    case "dropped":
      return true;
    default: {
      const unclassified: never = role;
      throw new Error(`unclassified status role: ${String(unclassified)}`);
    }
  }
}

// Whether closing work in this role releases what waited on it. Not derived
// from roleIsClosed: `parked` is closed but still blocks.
export function roleReleasesDependents(role: StatusRole): boolean {
  switch (role) {
    case "open":
    case "startable":
    case "parked":
      return false;
    case "done":
    case "dropped":
      return true;
    default: {
      const unclassified: never = role;
      throw new Error(`unclassified status role: ${String(unclassified)}`);
    }
  }
}

// Statuses in rank order, as the Go side sorts them.
const RANK_ORDER: readonly string[] = STATUS_DEFS.map((s) => s.name);

// Statuses off the board, in rank order. Includes `deferred`, so the Open preset
// hides it.
export const CLOSED_STATUSES: readonly string[] = STATUS_DEFS.filter((s) =>
  roleIsClosed(s.role),
).map((s) => s.name);

// Mirrors Go's config.ReleasingStatusNames. A strict subset of CLOSED_STATUSES:
// `deferred` still blocks.
export const RELEASING_STATUSES: readonly string[] = STATUS_DEFS.filter((s) =>
  roleReleasesDependents(s.role),
).map((s) => s.name);

// Open statuses in workflow order, then closed ones in rank order. Used by the
// status-column sort, the facet checkboxes and the query value lists; reordering
// the closed statuses re-sorts the status column.
export const STATUSES: readonly string[] = [
  ...STATUS_WORKFLOW_ORDER.filter((s) => !CLOSED_STATUSES.includes(s)),
  ...RANK_ORDER.filter((s) => CLOSED_STATUSES.includes(s)),
];

// The chooser order. A status missing from STATUS_WORKFLOW_ORDER is appended
// rather than dropped, as config.orderStatusesBy does in Go.
export const STATUS_WORKFLOW: readonly string[] = [
  ...STATUS_WORKFLOW_ORDER.filter((s) => STATUSES.includes(s)),
  ...STATUSES.filter((s) => !STATUS_WORKFLOW_ORDER.includes(s)),
];

// The Open status preset: every status that is not closed.
export const OPEN_STATUSES: readonly string[] = STATUSES.filter(
  (s) => !CLOSED_STATUSES.includes(s),
);

// Group names accepted wherever a status is, as on the CLI (`-s open`). Names and
// membership derive from the generated vocabulary. A Map so that
// `status:constructor` cannot resolve an inherited key. A group has more than one
// member; a one-status group would only be a second spelling of that status.
export const STATUS_GROUPS: ReadonlyMap<string, readonly string[]> = new Map([
  [STATUS_GROUP_OPEN, OPEN_STATUSES],
  [STATUS_GROUP_CLOSED, CLOSED_STATUSES],
]);

export const ESTIMATES: readonly string[] = ESTIMATE_DEFS.map((e) => e.name);

export const ESTIMATE_LABELS: Record<string, string> = Object.fromEntries(
  ESTIMATE_DEFS.map((e) => [e.name, e.label]),
);
