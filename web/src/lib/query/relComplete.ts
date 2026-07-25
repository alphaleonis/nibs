import { REL_ID_FIELDS, type RelIdKey } from "./relations";

// Async ID/title typeahead for relationship tokens (design 2.4, phase 6).
//
// This is the PURE, synchronous half: given the box text + caret, decide whether
// the caret sits inside a relationship-id token's VALUE (`parent:<here>`,
// `blocking:<here>`, `blocked-by:<here>`, `mentions:<here>`, `mentioned-by:<here>`).
// The Toolbar uses the result to fire a debounced search and offer candidate nibs.
//
// Kept SEPARATE from `getCompletion` (which stays metadata-only) because the data
// source here is asynchronous (a search query), not a static value list. Rel-id
// values are SCALAR — there is no comma multi-value — so the whole post-colon run
// of the token is the fragment, and selecting a candidate replaces that whole run.

/** A candidate nib row for the relationship typeahead. */
export interface NibSuggestion {
  id: string;
  title: string;
  type: string;
  status: string;
}

/** The caret sits in a rel-id token's value. Carries the field, the field-name as
 *  recognized, the partial value typed so far, and the [start, end) range of the
 *  value run to replace when a candidate is chosen. */
export interface RelValueContext {
  /** The scalar-id NibFilter key this token assigns (e.g. `parentId`). */
  field: RelIdKey;
  /** The recognized field-name (lowercased, e.g. `parent`, `blocked-by`). */
  name: string;
  /** The partial value typed after the colon (the whole post-colon run). */
  fragment: string;
  /** Replace-range start: the offset just after the colon. */
  start: number;
  /** Replace-range end: the token's end (next whitespace or end of text). */
  end: number;
}

/**
 * Detect whether `caret` is inside a relationship-id token's value, returning the
 * field + partial fragment + replace range, or `null` otherwise.
 *
 * The caret must be strictly AFTER the colon (in the value region); a caret in the
 * field-name portion, in a metadata token, or in free text yields `null`. A leading
 * `-` (negation) disqualifies the token — negation is a metadata-only feature —
 * matching `recognizeRelationship`.
 */
export function relTokenValueContext(text: string, caret: number): RelValueContext | null {
  // The whitespace-delimited token containing the caret (it may extend past the
  // caret in either direction).
  let start = caret;
  while (start > 0 && !/\s/.test(text[start - 1])) start--;
  let end = caret;
  while (end < text.length && !/\s/.test(text[end])) end++;

  const token = text.slice(start, end);
  if (token.startsWith("-")) return null;

  // Split on the FIRST colon so hyphenated field-names (`blocked-by`) are handled.
  const colon = token.indexOf(":");
  if (colon <= 0) return null;

  const name = token.slice(0, colon).toLowerCase();
  const field = REL_ID_FIELDS[name];
  if (!field) return null;

  // The colon's absolute offset; the value region begins one past it. The caret
  // must be in that region (>= just after the colon) for this to be a value context.
  const colonAbs = start + colon;
  if (caret <= colonAbs) return null;

  const valueStart = colonAbs + 1;
  return {
    field,
    name,
    fragment: text.slice(valueStart, end),
    start: valueStart,
    end,
  };
}
