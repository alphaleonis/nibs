import { FIELD_SPECS, completionValues, fieldSpec } from "./fields";
import { REL_TOKEN_ORDER } from "./relations";
import { AREA_FIELD } from "./area";
import type { AreaVocabulary } from "../areas";

export type CompletionKind = "field" | "value" | "tag";

// --- The completable vocabulary ------------------------------------------------
//
// Relationship and existence names derive from `REL_TOKEN_ORDER`, the array
// recognition is built from. Do not add them to `FIELD_SPECS`: parse, serialize
// and spans treat its entries as metadata fields. Area paths come from the caller.

const METADATA_FIELD_NAMES: readonly string[] = FIELD_SPECS.map((s) => s.name);

/** Relationship-id field names in canonical order. */
const REL_ID_NAMES: readonly string[] = REL_TOKEN_ORDER.flatMap((t) =>
  t.kind === "id" ? [t.name] : [],
);

/** Existence word (`has`/`no`/`is`) → the values it accepts, in first-appearance
 *  order. */
const EXISTENCE_VALUES: ReadonlyMap<string, readonly string[]> = (() => {
  const byWord = new Map<string, string[]>();
  for (const spec of REL_TOKEN_ORDER) {
    if (spec.kind !== "bool") continue;
    const colon = spec.token.indexOf(":");
    const word = spec.token.slice(0, colon);
    const value = spec.token.slice(colon + 1);
    const values = byWord.get(word);
    if (!values) byWord.set(word, [value]);
    else if (!values.includes(value)) values.push(value);
  }
  return byWord;
})();

/** Every completable field name, in `serializeQuery`'s block order. */
const ALL_FIELD_NAMES: readonly string[] = [
  ...METADATA_FIELD_NAMES,
  ...REL_ID_NAMES,
  ...EXISTENCE_VALUES.keys(),
  AREA_FIELD,
];

export interface CompletionOptions {
  /** Completion was requested explicitly (Ctrl+Space); an empty token then yields
   *  the field list instead of `null`. */
  explicit?: boolean;
  /** The areas vocabulary for `area:<partial>`. Offers nothing while loading or
   *  unavailable. */
  areas?: AreaVocabulary;
}

/**
 * Suggestions for the token left of the caret. `apply(item)` returns the new text
 * and caret, rewriting only the token-so-far.
 */
export interface Completion {
  kind: CompletionKind;
  items: string[];
  apply: (item: string) => { text: string; caret: number };
}

/**
 * Synchronous autocomplete for the query input: suggestions for the run of
 * non-space characters ending at `caret`, or `null`:
 *
 * - a partial field name (`ty`, `blo`, `-ty`) → matching field names (prefix);
 * - `field:partial` for a known enum → its group names, then values (substring);
 * - `tags:partial` → matching `availableTags` (substring);
 * - `has:` / `no:` / `is:` → the values that word accepts (substring);
 * - `area:partial` → declared paths from `options.areas` that contain no
 *   whitespace (case-insensitive substring);
 * - any other field (`title:`, `parent:`) → `null`; relationship ids come from the
 *   caller's async typeahead;
 * - an empty token → `null`, unless `options.explicit`, which yields every field.
 *
 * A multi-value token completes the segment after its last comma and does not
 * offer values it already holds.
 */
export function getCompletion(
  text: string,
  caret: number,
  availableTags: readonly string[] = [],
  options: CompletionOptions = {},
): Completion | null {
  // The token-so-far: from the previous whitespace up to the caret.
  let start = caret;
  while (start > 0 && !/\s/.test(text[start - 1])) start--;
  const prefix = text.slice(start, caret);
  if (prefix === "" && !options.explicit) return null;

  const negated = prefix.startsWith("-");
  const body = negated ? prefix.slice(1) : prefix;
  const colon = body.indexOf(":");

  if (colon === -1) {
    // Field-name completion (prefix match); negation narrows the pool to metadata.
    const partial = body.toLowerCase();
    const pool = negated ? METADATA_FIELD_NAMES : ALL_FIELD_NAMES;
    const items = pool.filter((n) => n.startsWith(partial));
    if (items.length === 0) return null;
    const before = text.slice(0, start);
    const after = text.slice(caret);
    // An explicit trigger can complete with the caret against the next token; a
    // space keeps the two from merging. The caret still lands after the colon.
    const separator = after !== "" && !/\s/.test(after[0]) ? " " : "";
    return {
      kind: "field",
      items,
      apply: (item) => {
        const insert = `${negated ? "-" : ""}${item}:`;
        return { text: before + insert + separator + after, caret: (before + insert).length };
      },
    };
  }

  const name = body.slice(0, colon);

  // Area paths: the whole post-colon run is the segment, not lowercased because
  // paths are case-sensitive.
  if (!negated && name.toLowerCase() === AREA_FIELD) {
    const segment = body.slice(colon + 1);
    // `area.validateNodes` allows interior whitespace in
    // a declared name, but the grammar splits on whitespace and has no quoting:
    // inserting `Web UI` would write `area:Web` plus free text `UI`.
    const items = (options.areas?.completions(segment) ?? []).filter((p) => !/\s/.test(p));
    if (items.length === 0) return null;
    const before = text.slice(0, caret - segment.length);
    const after = text.slice(caret);
    return {
      kind: "value",
      items,
      apply: (item) => ({ text: before + item + after, caret: (before + item).length }),
    };
  }

  // Existence values: scalar, so the whole post-colon run is the segment.
  const existence = negated ? undefined : EXISTENCE_VALUES.get(name.toLowerCase());
  if (existence) {
    const segment = body.slice(colon + 1).toLowerCase();
    const items = existence.filter((v) => v.includes(segment));
    if (items.length === 0) return null;
    const before = text.slice(0, caret - segment.length);
    const after = text.slice(caret);
    return {
      kind: "value",
      items,
      apply: (item) => ({ text: before + item + after, caret: (before + item).length }),
    };
  }

  const spec = fieldSpec(name);
  if (!spec) return null;

  const valuePart = body.slice(colon + 1);
  const lastComma = valuePart.lastIndexOf(",");
  const segment = valuePart.slice(lastComma + 1).toLowerCase();
  // Values already listed earlier in this same token — don't offer them again.
  const chosen = new Set(
    valuePart
      .slice(0, lastComma + 1)
      .split(",")
      .map((v) => v.toLowerCase())
      .filter((v) => v !== ""),
  );

  const pool = spec.values === null ? availableTags : completionValues(spec);
  const items = pool.filter((v) => !chosen.has(v) && v.includes(segment));
  if (items.length === 0) return null;

  const replaceStart = caret - segment.length;
  const before = text.slice(0, replaceStart);
  const after = text.slice(caret);
  return {
    kind: spec.values === null ? "tag" : "value",
    items,
    apply: (item) => ({ text: before + item + after, caret: (before + item).length }),
  };
}
