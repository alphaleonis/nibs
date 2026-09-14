import { REL_ID_FIELDS, type RelIdKey } from "./relations";

// The synchronous half of the relationship-id typeahead: whether the caret sits
// in the value of a token named in `REL_ID_FIELDS`. The Toolbar runs the search.
// Values are scalar, so a chosen candidate replaces the whole post-colon run.

/** A candidate nib row for the relationship typeahead. */
export interface NibSuggestion {
  id: string;
  title: string;
  type: string;
  status: string;
}

/** The rel-id token value the caret sits in. */
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
 * The rel-id value context at `caret`, or `null`. The caret must be after the
 * colon. A negated token yields `null`, since the parser parks it.
 */
export function relTokenValueContext(text: string, caret: number): RelValueContext | null {
  // The whitespace-delimited token around the caret.
  let start = caret;
  while (start > 0 && !/\s/.test(text[start - 1])) start--;
  let end = caret;
  while (end < text.length && !/\s/.test(text[end])) end++;

  const token = text.slice(start, end);
  if (token.startsWith("-")) return null;

  const colon = token.indexOf(":");
  if (colon <= 0) return null;

  const name = token.slice(0, colon).toLowerCase();
  const field = REL_ID_FIELDS.get(name);
  if (!field) return null;

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
