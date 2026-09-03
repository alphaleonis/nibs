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
 * The milestone arm holds the same group id the server keys that scope by:
 * `scopeTable[ScopeMilestone].group` is `resolvedMilestoneID` (orderer.go), the
 * nib id a nib's `milestone:` field resolves to — not the field's raw text and
 * not a title. The Milestones view's membership lens is what mints this arm, and
 * keying its sections on `milestoneOf`'s answer rather than on the raw
 * `milestone:` field is what holds it to the resolved id — see `meaning`'s
 * invariant in tree.ts, which is where that obligation is written down.
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
 * server groups at once.
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
 * Spells a nib id as something a reader recognizes, or returns undefined for an
 * id it cannot place — where the id itself is the best answer available.
 *
 * A function rather than a lookup table: the layer that can answer holds the
 * rendered rows, and that list is replaced mid-gesture by a refetch or an
 * expand.
 */
export type RegionNamer = (id: string) => string | undefined;

/**
 * The namer for a caller that can place no id — every phrase then spells its ids
 * as ids, which is what a caller with nothing loaded should say.
 *
 * It exists so `nameOf` can be REQUIRED at every site instead of optional. An
 * optional namer is threaded by habit: forgetting it at one of a dozen call
 * sites reverts that one sentence to raw ids with no compile error, which is a
 * defect the phrase itself has to be asserted to catch. Passing this constant is
 * a decision the reader can see.
 */
export const BY_ID: RegionNamer = () => undefined;

/**
 * Names the LIST a region is, as a noun phrase a caller can put after a verb
 * ("Reorder in " + describeRegion(r, nameOf)).
 *
 * The phrase comes back finished, so the single thing a caller can vary is how
 * an id inside it is spelled: `nameOf` is asked, and the raw id stands in
 * wherever it has no answer. That indirection is the whole title story here —
 * this module imports one generated type and nothing else, so it has no nib to
 * read a title off, an isolation region.test.ts asserts since nothing in `web/`
 * enforces an import boundary.
 */
export function describeRegion(region: Region, nameOf: RegionNamer): string {
  switch (region.axis) {
    case "parent":
      return region.parentId === null ? "the top level" : `the children of ${spellId(region.parentId, nameOf)}`;
    case "milestone":
      return `the ${spellId(region.milestoneId, nameOf)} queue`;
  }
}

/**
 * One id, spelled the way `describeRegion` spells the ones inside its phrases —
 * exported so a caller naming a nib BESIDE a region ("Move under X") reads the
 * namer the same way rather than growing a second fallback rule.
 *
 * `||` rather than `??`: an empty title names nothing, and a phrase built around
 * one reads as a missing word rather than as an untitled nib.
 */
export function spellId(id: string, nameOf: RegionNamer): string {
  return nameOf(id) || id;
}
