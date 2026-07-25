import type { NibFilter } from "../types";

// The box-owned slice of a NibFilter. Both ParsedQuery and a full NibFilter
// satisfy this shape, so `serializeQuery(parseQuery(text))` type-checks.
type QueryFields = Pick<NibFilter, "status" | "search">;

/**
 * Render the box-owned filter fields to canonical query text: one `status:<v>`
 * token per status value (in order), then the free-text `search` last. Non-box
 * fields (type/priority/estimate/tags/relationships) are ignored — the text box
 * only represents status + search in phase 1.
 *
 * Canonical form is idempotent: `serializeQuery(parseQuery(s)) === s` for any `s`
 * already in this shape (status tokens first, single-spaced, lowercased).
 */
export function serializeQuery(filter: QueryFields): string {
  const parts: string[] = [];
  for (const value of filter.status ?? []) {
    parts.push(`status:${value}`);
  }
  if (filter.search) {
    parts.push(filter.search);
  }
  return parts.join(" ");
}
