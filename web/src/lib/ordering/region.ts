import type { OrderScope } from "../gql/graphql";

/**
 * The set within which a single `reorderNib` can position rows against each
 * other — one arm per ordering key the server keeps: `order` among the siblings
 * under one resolved parent, `milestoneOrder` among the members of one
 * milestone's queue.
 *
 * The parent arm holds the RESOLVED parent, not the stored link, because that is
 * already what arrives: the `parentId` resolver returns `resolvedParentID(obj,
 * r.Reader)` (schema.resolvers.go) and `scopeTable[ScopeParent].group` is that
 * same `resolvedParentID` (orderer.go), so nothing re-derives it here. It is the
 * client ENCODING of that group rather than the value: the resolver maps the
 * root group `""` to `null`, so `parentId: null` and group `""` are one group
 * under two spellings.
 *
 * Not to be confused with `reorderNib`'s own `parentId` argument, which is a
 * CONTAINER CHANGE under the opposite convention (`nil` = reparent nothing, `""`
 * = clear to the root) — a move that stays in one region omits it entirely.
 */
export type Region =
  | { readonly axis: "parent"; readonly parentId: string | null }
  | { readonly axis: "milestone"; readonly milestoneId: string };

// Compile-time guard binding Region's arms to the schema's OrderScope, so a
// closed union of two cannot quietly stop matching the axes the server orders
// on.
//
// BOTH directions are required, and each catches drift the other cannot see.
// The first catches an arm invented here that no scope backs. The second catches
// a scope the schema declares that no arm models — the direction with no other
// backstop at all, since every other use of the enum in this module either
// consumes an arm (`scopeOf`'s switch) or produces a value the wider union still
// accepts, so an unmodeled scope is silently absent rather than a type error.
//
// `import type` keeps both erased at compile time.
type _ClientArmsExist = Region["axis"] extends Lowercase<OrderScope> ? true : never;
const _clientArmsCheck: _ClientArmsExist = true;
void _clientArmsCheck;

type _SchemaArmsModeled = Lowercase<OrderScope> extends Region["axis"] ? true : never;
const _schemaArmsCheck: _SchemaArmsModeled = true;
void _schemaArmsCheck;

/**
 * The wire scope a move in this region runs under.
 *
 * Exhaustive switch, no default arm: a third arm is a compile error here until
 * it names its scope, rather than falling through to one.
 */
export function scopeOf(region: Region): OrderScope {
  switch (region.axis) {
    case "parent":
      return "PARENT";
    case "milestone":
      return "MILESTONE";
  }
}

/**
 * Whether two rows are in the same ordering group.
 *
 * One-directional: same region means one `reorderNib` can position them against
 * each other, but a false answer does NOT mean it cannot. A region names the
 * single group a row's display position sits in, while a nib can be in several
 * server groups at once — two nibs sharing a milestone queue under different
 * parents get different parent-axis regions in every view shipped today, and a
 * MILESTONE-scope reorder still positions them against each other.
 *
 * `null` is the absence of a region, so it matches nothing, INCLUDING another
 * null: two rows that are in no orderable list are not thereby in one together.
 */
export function sameRegion(a: Region | null, b: Region | null): boolean {
  return a !== null && b !== null && regionKey(a) === regionKey(b);
}

/**
 * A region's identity as one comparable string, so equality is decided by an
 * exhaustive switch like `scopeOf`'s rather than by an if-chain that answers
 * `false` for any arm it was never taught. The axis prefix makes the two arms
 * incomparable whatever their ids.
 */
function regionKey(r: Region): string {
  switch (r.axis) {
    case "parent":
      // The root arm gets its own key rather than folding onto `""`, so a region
      // someone hand-built with `parentId: ""` still compares unequal to it —
      // this function decides equality, not what the encoding accepts.
      return r.parentId === null ? "p:root" : `p:nib:${r.parentId}`;
    case "milestone":
      return `m:${r.milestoneId}`;
  }
}

/**
 * The one region every input shares, or null when they share none — an empty
 * input, a null among them, or two that differ.
 *
 * Takes regions rather than rows, so this module stays independent of the
 * table's row shape. A `null` among the inputs is an input with no region and
 * makes the whole answer null, so a caller holding an id it could not resolve to
 * a row needs no special case for it.
 */
export function commonRegion(regions: readonly (Region | null)[]): Region | null {
  if (regions.length === 0) return null;
  const first = regions[0];
  for (const region of regions) {
    if (!sameRegion(first, region)) return null;
  }
  return first;
}

/**
 * Names the LIST a region is, as a noun phrase a caller can put after a verb
 * ("Reorder in " + describeRegion(r)).
 *
 * By ID, and not title-capable at all: the phrase comes back finished, with the
 * id already in it. That is downstream of the module importing one generated
 * type and nothing else, so it has no nib to read a title off — an isolation
 * region.test.ts asserts, since nothing in `web/` enforces an import boundary.
 */
export function describeRegion(region: Region): string {
  switch (region.axis) {
    case "parent":
      return region.parentId === null ? "the top level" : `the children of ${region.parentId}`;
    case "milestone":
      return `the ${region.milestoneId} queue`;
  }
}
