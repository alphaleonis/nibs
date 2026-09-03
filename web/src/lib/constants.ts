// The vocabulary itself — status, type, priority and estimate names, roles,
// hierarchy rules and both status orders — is GENERATED from the Go
// definitions into ./generated/vocabulary.ts (`task codegen`; the committed
// output is byte-pinned by Go's TestGeneratedVocabularyIsFresh). This module
// derives the presentation-order lists and role-based groupings the app
// consumes; nothing here restates a name the generator owns.
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

// roleIsClosed classifies a role as on or off the board. The switch is
// exhaustive with a `never` default, so a NEW role arriving through
// regeneration fails svelte-check right here until someone classifies it —
// same philosophy as the NibFilter key-set guard in types.ts.
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

// roleReleasesDependents is the sibling classification: whether closing work in
// this role satisfies what waited on it. Same exhaustive switch and `never`
// default as roleIsClosed, and deliberately not derived from it — `parked` is
// closed and still holds, which is the one pair on which the two answers differ.
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

// Statuses in rank order — the sequence the Go side sorts by.
const RANK_ORDER: readonly string[] = STATUS_DEFS.map((s) => s.name);

// Closed statuses — a nib with one of these is off the board. `deferred` is one
// of them: setting work aside is a way of closing it, not a state of being
// open, so it is hidden by the Open preset alongside completed and scrapped.
// Derived from the roles, in rank order.
export const CLOSED_STATUSES: readonly string[] = STATUS_DEFS.filter((s) =>
  roleIsClosed(s.role),
).map((s) => s.name);

// Statuses that release their dependents — the mirror of Go's
// config.ReleasingStatusNames, and a strict subset of CLOSED_STATUSES since
// `deferred` is closed and goes on blocking. A milestone in one of these has
// let its queue go, which is what closes the assignment door in
// `milestoneAcceptsAssignment`.
export const RELEASING_STATUSES: readonly string[] = STATUS_DEFS.filter((s) =>
  roleReleasesDependents(s.role),
).map((s) => s.name);

// The ordering everything but the choosers uses: the status-column sort in
// tableSort.ts, the facet checkboxes and the query-language value lists. The
// open statuses read best in workflow order (draft before the work it becomes),
// the closed ones in the same sequence the Go side ranks them — which is why
// this is a derivation rather than one of the generated orders verbatim:
// swapping the closed pair into workflow order would quietly re-sort the
// status column.
export const STATUSES: readonly string[] = [
  ...STATUS_WORKFLOW_ORDER.filter((s) => !CLOSED_STATUSES.includes(s)),
  ...RANK_ORDER.filter((s) => CLOSED_STATUSES.includes(s)),
];

// STATUS_WORKFLOW is what components read, and it is built from STATUSES rather
// than STATUS_WORKFLOW_ORDER verbatim: a status the sequence forgets is
// appended instead of dropped, so an ordering mistake makes a picker read oddly
// and never hides a status a nib can be set to. Same fail-safe as
// config.orderStatusesBy on the Go side.
export const STATUS_WORKFLOW: readonly string[] = [
  ...STATUS_WORKFLOW_ORDER.filter((s) => STATUSES.includes(s)),
  ...STATUSES.filter((s) => !STATUS_WORKFLOW_ORDER.includes(s)),
];

// Quick State-facet preset that sets the `status` include-list in one click.
// The include-list is the single source of truth for status visibility, so this
// OVERWRITES the current selection.
//   Open → everything that is not closed
export const OPEN_STATUSES: readonly string[] = STATUSES.filter(
  (s) => !CLOSED_STATUSES.includes(s),
);

// Status groups: names accepted anywhere a concrete status is, standing for the
// set of statuses they contain. This mirrors the CLI, where `-s open` is legal
// wherever `-s todo` is (cmd/statusfilter.go), so one *value* vocabulary spans
// both surfaces: the word after `-s` on the CLI is the word after `status:` in
// the box. The token syntax is not shared — the box's `-` prefix is negation
// (`-status:closed`), so CLI flags do not parse here.
//
// Both the group NAMES and the membership derive from the generated
// vocabulary, so a status added on the Go side lands in exactly one group on
// regeneration.
//
// A Map, not an object literal: object lookup would resolve inherited keys, so
// `status:constructor` would read as a legal group.
//
// Only two groups, and every group has more than one member. A group over a
// single status would just be a second spelling of that status — which is why
// there is no `parked` group for {deferred}.
export const STATUS_GROUPS: ReadonlyMap<string, readonly string[]> = new Map([
  [STATUS_GROUP_OPEN, OPEN_STATUSES],
  [STATUS_GROUP_CLOSED, CLOSED_STATUSES],
]);

export const ESTIMATES: readonly string[] = ESTIMATE_DEFS.map((e) => e.name);

export const ESTIMATE_LABELS: Record<string, string> = Object.fromEntries(
  ESTIMATE_DEFS.map((e) => [e.name, e.label]),
);
