import { FIELD_SPECS, collapseToTokens } from "./fields";
import type { FieldSpec, QueryFilter } from "./fields";
import { REL_TOKEN_ORDER } from "./relations";
import { AREA_FIELD } from "./area";

/**
 * Render the box-owned filter fields plus the invalid-token sidecar to canonical
 * query text.
 *
 * Order:
 *  1. Metadata — type, priority, status, estimate, tags — each emitting its
 *     positive `field:v1,v2` token then its negative `-field:v1,v2` token. Values
 *     are ordered canonically (enum-declaration order for the four enums,
 *     alphabetical for tags) and comma-joined, with any group whose members are
 *     all present collapsed to the group name (`status:open`).
 *  2. Relationship/existence tokens in the fixed `REL_TOKEN_ORDER` — grouped by
 *     dimension (hierarchy: parent + ancestor/descendant/sibling, then blocking,
 *     blocked-by + is:blocked, mentions, mentioned-by, and last the assignment
 *     axis: milestone + is:backlog), id token before has/no within each dimension.
 *  3. The ownership axis, `area:<path>` — the third token block, emitted after
 *     the second rather than inside it. It lands beside the assignment axis that
 *     closes REL_TOKEN_ORDER, which is the other token naming where work belongs
 *     rather than how nibs relate.
 *  4. Free-text `search`.
 *  5. Preserved `invalidTokens`, flagged, at the very end.
 *
 * A full NibFilter is accepted. The identity `serializeQuery(parseQuery(s)) === s`
 * holds for any `s` already in canonical form, including relationship + existence
 * tokens and preserved invalid tokens. Group collapse is what puts the spelled-out
 * member list OUTSIDE canonical form: `status:draft,todo,in-progress` canonicalizes
 * to `status:open`, and it is `status:open` that round-trips.
 *
 * Collapse applies to a group's members wherever they appear, not only when they
 * are the whole list, so a group name survives beside other values:
 * `status:open,deferred` and `status:open,closed` are both canonical. A group only
 * disappears when its members are not all present — `status:draft,todo` stays
 * spelled out, because two thirds of `open` is not `open`.
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
      // Compared by value, not truthiness: the field is tri-state, and `false` is
      // a set value that emits the `no:` spelling rather than nothing. Exactly one
      // of a dimension's two entries can match, so the roundtrip stays canonical.
      parts.push(t.token);
    }
  }

  // Emitted verbatim: an area path is case-sensitive and carries `/`, and the
  // parser reads the whole post-colon run back as one scalar value.
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

// Render one field's value list as the value part of its token: each group whose
// members are all present as the group name, the rest spelled out, comma-joined.
//
// Collapsing is what makes a group name usable rather than a one-shot
// expansion. The include-list is the single source of truth for the box's text,
// so without it a typed `status:open` would come back as its three members the
// moment the box lost focus, and no shared link would ever carry the shorthand.
// It also gives the Status facet's Open preset the group spelling for free.
function renderValues(spec: FieldSpec, values: readonly string[]): string {
  return collapseToTokens(spec, values).join(",");
}
