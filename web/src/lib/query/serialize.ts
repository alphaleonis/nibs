import { FIELD_SPECS, collapseToTokens } from "./fields";
import type { FieldSpec, QueryFilter } from "./fields";
import { REL_TOKEN_ORDER } from "./relations";
import { AREA_FIELD } from "./area";

/**
 * Render the box-owned filter fields and invalid tokens as canonical query text:
 *  1. Metadata in `FIELD_SPECS` order, each `field:…` then `-field:…`, values
 *     via `collapseToTokens`.
 *  2. Relationship/existence tokens in `REL_TOKEN_ORDER`.
 *  3. `area:<path>`.
 *  4. Free-text `search`.
 *  5. `invalidTokens`.
 *
 * Accepts a full NibFilter. `serializeQuery(parseQuery(s)) === s` for canonical
 * `s`. A group spelled out in full is not canonical
 * (`status:draft,todo,in-progress` becomes `status:open`); a partial one
 * (`status:draft,todo`) is.
 */
export function serializeQuery(query: { filter: QueryFilter; invalidTokens?: string[] }): string {
  const { filter, invalidTokens = [] } = query;
  const parts: string[] = [];

  for (const spec of FIELD_SPECS) {
    const inc = filter[spec.filterKey];
    if (inc && inc.length > 0) {
      parts.push(`${spec.name}:${renderValues(spec, inc)}`);
    }
    const exc = filter[spec.excludeKey];
    if (exc && exc.length > 0) {
      parts.push(`-${spec.name}:${renderValues(spec, exc)}`);
    }
  }

  for (const t of REL_TOKEN_ORDER) {
    if (t.kind === "id") {
      const id = filter[t.field];
      if (id) parts.push(`${t.name}:${id}`);
    } else if (filter[t.field] === t.value) {
      // Compared by value: `false` is set, and emits the `no:` spelling.
      parts.push(t.token);
    }
  }

  // Verbatim: the parser reads the whole post-colon run back as the path.
  if (filter.area) {
    parts.push(`${AREA_FIELD}:${filter.area}`);
  }

  if (filter.search) {
    parts.push(filter.search);
  }

  for (const token of invalidTokens) {
    parts.push(token);
  }

  return parts.join(" ");
}

// One field's value list as a token value. Collapsing groups keeps a typed
// `status:open` from being rewritten to its members when the box canonicalizes.
function renderValues(spec: FieldSpec, values: readonly string[]): string {
  return collapseToTokens(spec, values).join(",");
}
