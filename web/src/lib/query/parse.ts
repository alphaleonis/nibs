// The structured subset of NibFilter that the query text box owns: the `status`
// include-list and the free-text `search`. Both keys are omitted when the parsed
// text contributes nothing to them, so a caller can tell "field absent" from
// "field set to empty" and delete/keep accordingly.
export interface ParsedQuery {
  status?: string[];
  search?: string;
}

// A `field:value` token. Phase 1 recognizes only `status:`; every other token is
// treated as free text (forward-compatible: later phases add fields without
// changing already-typed queries). The field name is matched case-insensitively
// and the value is lowercased by the caller.
const FIELD_TOKEN = /^([A-Za-z]+):(.+)$/;

/**
 * Parse filter-box text into the structured fields the box owns.
 *
 * - Each `status:<value>` token contributes its (lowercased) value to `status`;
 *   repeated tokens union in order (lenient-in — a forward-compatible superset of
 *   the canonical single-value form).
 * - Everything else — bare words and unrecognized `field:value` tokens — joins,
 *   whitespace-collapsed, into `search` (the existing Bleve full-text field).
 *
 * Absent fields are omitted from the result rather than set to empty.
 */
export function parseQuery(text: string): ParsedQuery {
  const status: string[] = [];
  const words: string[] = [];

  for (const token of text.split(/\s+/)) {
    if (token === "") continue;
    const match = FIELD_TOKEN.exec(token);
    if (match && match[1].toLowerCase() === "status") {
      status.push(match[2].toLowerCase());
      continue;
    }
    words.push(token);
  }

  const result: ParsedQuery = {};
  if (status.length > 0) result.status = status;
  const search = words.join(" ");
  if (search !== "") result.search = search;
  return result;
}
