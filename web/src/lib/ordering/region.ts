import type { OrderScope } from "../gql/graphql";

/**
 * The set within which a single `reorderNib` can position rows against each
 * other — one arm per ordering key the server keeps: `order` among the siblings
 * under one resolved parent, `milestoneOrder` among the members of one
 * milestone's queue.
 *
 * `parentId` is the resolved parent the `parentId` resolver returns, which is
 * the server's PARENT group (`resolvedParentID`), with the root group `""`
 * encoded as `null`. `milestoneId` is the resolved milestone id the MILESTONE
 * group is keyed by (`resolvedMilestoneID`), not the raw `milestone:` text; see
 * `meaning`'s invariant in tree.ts.
 *
 * Not `reorderNib`'s own `parentId` argument, which is a container change under
 * the opposite convention (`nil` = no reparent, `""` = the root).
 */
export type Region =
  | { readonly axis: "parent"; readonly parentId: string | null }
  | { readonly axis: "milestone"; readonly milestoneId: string };

// Compile-time guards binding Region's arms to the schema's OrderScope in both
// directions: an arm no scope backs, and a scope no arm models.
type _ClientArmsExist = Region["axis"] extends Lowercase<OrderScope> ? true : never;
const _clientArmsCheck: _ClientArmsExist = true;
void _clientArmsCheck;

type _SchemaArmsModeled = Lowercase<OrderScope> extends Region["axis"] ? true : never;
const _schemaArmsCheck: _SchemaArmsModeled = true;
void _schemaArmsCheck;

/**
 * The wire scope a move in this region runs under. No default arm, so a new
 * axis fails to compile here until it names its scope.
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
 * One-directional: a nib can be in several server groups at once, so `false`
 * does not mean one `reorderNib` cannot position them. `null` matches nothing,
 * including another `null`.
 */
export function sameRegion(a: Region | null, b: Region | null): boolean {
  return a !== null && b !== null && regionKey(a) === regionKey(b);
}

/**
 * A region's identity as one comparable string, decided by an exhaustive switch.
 * The axis prefix keeps the two arms apart whatever their ids.
 */
function regionKey(r: Region): string {
  switch (r.axis) {
    case "parent":
      // The root has its own key, so a hand-built `parentId: ""` does not equal it.
      return r.parentId === null ? "p:root" : `p:nib:${r.parentId}`;
    case "milestone":
      return `m:${r.milestoneId}`;
  }
}

/**
 * The one region every input shares, or null when they share none — an empty
 * input, a null among them, or two that differ.
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
 * Spells a nib id as something a reader recognizes, or returns undefined where
 * the id itself is the best answer. A function rather than a table, because the
 * rows that can answer are replaced mid-gesture.
 */
export type RegionNamer = (id: string) => string | undefined;

/**
 * The namer for a caller that can place no id: every id is spelled as itself.
 * Pass it explicitly — `nameOf` is required so a forgotten namer is a compile
 * error rather than a sentence of raw ids.
 */
export const BY_ID: RegionNamer = () => undefined;

/**
 * Names the list a region is, as a noun phrase a caller can put after a verb
 * ("Reorder in " + describeRegion(r, nameOf)). Ids inside it are spelled
 * through `nameOf`; this module imports no row type to read a title from.
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
 * One id, spelled the way `describeRegion` spells its ids. `||` rather than
 * `??`, so an empty title falls back to the id.
 */
export function spellId(id: string, nameOf: RegionNamer): string {
  return nameOf(id) || id;
}
