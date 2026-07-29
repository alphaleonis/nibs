import { FIELD_SPECS, matchingGroup, orderValues } from "./fields";
import type { FieldSpec, QueryFilter } from "./fields";
import { REL_TOKEN_ORDER } from "./relations";

/**
 * Render the box-owned filter fields plus the invalid-token sidecar to canonical
 * query text.
 *
 * Order:
 *  1. Metadata — type, priority, status, estimate, tags — each emitting its
 *     positive `field:v1,v2` token then its negative `-field:v1,v2` token. Values
 *     are ordered canonically (enum-declaration order for the four enums,
 *     alphabetical for tags) and comma-joined — unless they are exactly a group's
 *     members, which collapse to the group name (`status:open`).
 *  2. Relationship/existence tokens in the fixed `REL_TOKEN_ORDER` — grouped by
 *     relationship dimension (hierarchy: parent + ancestor/descendant/sibling, then
 *     blocking, blocked-by + is:blocked, mentions, mentioned-by), id token before
 *     has/no within each dimension.
 *  3. Free-text `search`.
 *  4. Preserved `invalidTokens`, flagged, at the very end.
 *
 * A full NibFilter is accepted. The identity `serializeQuery(parseQuery(s)) === s`
 * holds for any `s` already in canonical form, including relationship + existence
 * tokens and preserved invalid tokens. Group collapse is what puts the spelled-out
 * member list OUTSIDE canonical form: `status:draft,todo,in-progress` canonicalizes
 * to `status:open`, and it is `status:open` that round-trips.
 *
 * Collapse is exact single-group set equality, so a token naming TWO groups is
 * also non-canonical — and loses both names rather than keeping one:
 * `status:open,closed` covers every status and serializes back spelled out.
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

  if (filter.search) {
    parts.push(filter.search);
  }

  for (const token of invalidTokens) {
    parts.push(token);
  }

  return parts.join(" ");
}

// Render one field's value list as the value part of its token: the group name
// when the list is exactly a group's members, else the canonically ordered
// values comma-joined.
//
// Collapsing is what makes a group name usable rather than a one-shot
// expansion. The include-list is the single source of truth for the box's text,
// so without it a typed `status:open` would come back as its three members the
// moment the box lost focus, and no shared link would ever carry the shorthand.
// It also gives the Status facet's Open preset the group spelling for free.
function renderValues(spec: FieldSpec, values: readonly string[]): string {
  return matchingGroup(spec, values) ?? orderValues(spec, values).join(",");
}
