import { FIELD_SPECS, orderValues } from "./fields";
import type { QueryFilter } from "./fields";

/**
 * Render the box-owned filter fields plus the invalid-token sidecar to canonical
 * query text.
 *
 * Field order mirrors the dropdown row: type, priority, status, estimate, tags —
 * each emitting its positive `field:v1,v2` token then its negative `-field:v1,v2`
 * token. Values are ordered canonically (enum-declaration order for the four
 * enums, alphabetical for tags) and comma-joined. Free-text `search` comes next,
 * and any `invalidTokens` are appended, flagged, at the very end.
 *
 * A full NibFilter is accepted; non-box fields (relationships/existence) are
 * ignored. The identity `serializeQuery(parseQuery(s)) === s` holds for any `s`
 * already in canonical form, including preserved invalid tokens.
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

  if (filter.search) {
    parts.push(filter.search);
  }

  for (const token of invalidTokens) {
    parts.push(token);
  }

  return parts.join(" ");
}
