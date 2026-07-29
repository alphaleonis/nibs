import { FIELD_SPECS, orderValues } from "./fields";
import type { QueryFilter } from "./fields";
import { REL_TOKEN_ORDER } from "./relations";

/**
 * Render the box-owned filter fields plus the invalid-token sidecar to canonical
 * query text.
 *
 * Order (design 2.4):
 *  1. Metadata — type, priority, status, estimate, tags — each emitting its
 *     positive `field:v1,v2` token then its negative `-field:v1,v2` token. Values
 *     are ordered canonically (enum-declaration order for the four enums,
 *     alphabetical for tags) and comma-joined.
 *  2. Relationship/existence tokens in the fixed `REL_TOKEN_ORDER` — grouped by
 *     relationship dimension (parent, blocking, blocked-by + is:blocked, mentions,
 *     mentioned-by), id token before has/no within each dimension.
 *  3. Free-text `search`.
 *  4. Preserved `invalidTokens`, flagged, at the very end.
 *
 * A full NibFilter is accepted. The identity `serializeQuery(parseQuery(s)) === s`
 * holds for any `s` already in canonical form, including relationship + existence
 * tokens and preserved invalid tokens.
 */
export function serializeQuery(query: { filter: QueryFilter; invalidTokens?: string[] }): string {
  const { filter, invalidTokens = [] } = query;
  const parts: string[] = [];

  for (const spec of FIELD_SPECS) {
    const inc = filter[spec.filterKey];
    if (inc && inc.length > 0) {
      parts.push(`${spec.name}:${orderValues(spec, inc).join(",")}`);
    }
    const exc = filter[spec.excludeKey];
    if (exc && exc.length > 0) {
      parts.push(`-${spec.name}:${orderValues(spec, exc).join(",")}`);
    }
  }

  for (const t of REL_TOKEN_ORDER) {
    if (t.kind === "id") {
      const id = filter[t.field];
      if (id) parts.push(`${t.name}:${id}`);
    } else if (filter[t.field] === t.value) {
      // Compared by value, not truthiness: the field is tri-state, and `false` is
      // a set value that emits the `no:` spelling rather than nothing. Exactly one
      // of a dimension's two entries can match, so the roundtrip stays canonical.
      parts.push(t.token);
    }
  }

  if (filter.search) {
    parts.push(filter.search);
  }

  for (const token of invalidTokens) {
    parts.push(token);
  }

  return parts.join(" ");
}
