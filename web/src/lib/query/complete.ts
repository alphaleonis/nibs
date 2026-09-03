import { FIELD_SPECS, completionValues, fieldSpec } from "./fields";
import { REL_TOKEN_ORDER } from "./relations";
import { AREA_FIELD } from "./area";
import type { AreaVocabulary } from "../areas";

export type CompletionKind = "field" | "value" | "tag";

// --- The completable vocabulary ------------------------------------------------
//
// Completion offers the whole query language, not just the five metadata facets.
// The relationship and existence halves are derived from `REL_TOKEN_ORDER`, which is
// also what recognition reads: `recognizeRelationship` goes through `REL_ID_FIELDS`
// and `EXISTENCE_TOKENS`, and both are built from that one array. Offering a
// spelling the parser rejects therefore requires breaking `relations.ts` itself.
// They are deliberately NOT added to `FIELD_SPECS`: `has`/`no`/`is` are not
// metadata fields, and `parse.ts`, `serialize.ts` and `spans.ts` all read that
// structure — a pseudo-field there would change parsing and highlighting too.
//
// The `area` field-name comes from `area.ts` for the same reason, and its VALUES
// are a per-store vocabulary the caller passes in.

const METADATA_FIELD_NAMES: readonly string[] = FIELD_SPECS.map((s) => s.name);

/** Relationship-id field names, in canonical token order (`parent`, `ancestor`, …). */
const REL_ID_NAMES: readonly string[] = REL_TOKEN_ORDER.flatMap((t) =>
  t.kind === "id" ? [t.name] : [],
);

/** Existence word (`has`/`no`/`is`) → the values it accepts, both in first-appearance
 *  order. Only dimensions the server has a predicate for appear, because only those
 *  are enumerated in `REL_TOKEN_ORDER`. */
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

/** Every completable field name: metadata, then relationship ids, then existence
 *  words, then the ownership axis — the three token blocks in the order
 *  `serializeQuery` emits them. A leading `-` narrows this to the metadata half —
 *  negation is a metadata-only feature, so offering the rest there would suggest
 *  tokens the parser parks as invalid. */
const ALL_FIELD_NAMES: readonly string[] = [
  ...METADATA_FIELD_NAMES,
  ...REL_ID_NAMES,
  ...EXISTENCE_VALUES.keys(),
  AREA_FIELD,
];

/** Caller-supplied completion behavior. */
export interface CompletionOptions {
  /** The user asked for completions explicitly (Ctrl+Space) rather than by typing.
   *  Only then does an empty token yield the field list instead of `null`. */
  explicit?: boolean;
  /** The runtime areas vocabulary, completing `area:<partial>`. Absent, and while
   *  it is loading or unavailable, it offers nothing: its own `completions` returns
   *  an empty list in both of those states, which becomes `null` here. */
  areas?: AreaVocabulary;
}

/**
 * A context-aware completion for the token immediately left of the caret.
 * `items` are the suggestions to display; `apply(item)` produces the new input
 * text and caret position after inserting the chosen item (only the token-so-far
 * is rewritten — text after the caret is untouched).
 */
export interface Completion {
  kind: CompletionKind;
  items: string[];
  apply: (item: string) => { text: string; caret: number };
}

/**
 * Synchronous autocomplete for the query input. Given the full text and caret
 * offset, return suggestions for the token being typed (the run of non-space
 * characters ending at the caret), or `null` when there is nothing to suggest:
 *
 * - a partial field name (`ty`, `blo`, `-ty`) → matching field names (prefix match);
 * - `field:` / `field:partial` for a known enum → that field's group names, then
 *   its values (substring);
 * - `tags:partial` → matching entries from `availableTags` (substring);
 * - `has:` / `no:` / `is:` → the existence dimensions that word accepts (substring);
 * - `area:` / `area:partial` → the declared area paths the grammar can carry, in
 *   declaration order (case-insensitive substring), from the vocabulary in
 *   `options.areas` — paths with interior whitespace are withheld, since there is
 *   no quoting to insert them with (see the `AREA_FIELD` branch below);
 * - an unknown field (`title:`) → `null`;
 * - an empty token → `null`, unless `options.explicit` (Ctrl+Space), which yields
 *   the full field list.
 *
 * Relationship-id VALUES (`parent:<id>`) are not completed here — they come from
 * the asynchronous nib typeahead in the caller.
 *
 * Area paths are a per-store vocabulary that arrives over the wire, and tags are
 * collected from the loaded nibs; both are supplied by the caller rather than
 * imported, so this stays a pure function of its arguments.
 *
 * Multi-value tokens complete the segment after the last comma and never
 * re-suggest a value already chosen earlier in the same token.
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
    // A caret jammed against the next token — reachable through the explicit trigger,
    // the only path on which an empty token completes at all — would otherwise glue
    // the insert onto that token, merging two into one and dropping a filter facet.
    // The caret still lands after the colon, ready for the value.
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

  // Area paths, from the runtime vocabulary. Scalar like the existence values —
  // the whole post-colon run is the segment — and NOT lowercased, because area
  // paths are case-sensitive (query/area.ts). Negation is excluded for the same
  // reason it is there: `-area:` has no filter key to write and the parser parks it.
  if (!negated && name.toLowerCase() === AREA_FIELD) {
    const segment = body.slice(colon + 1);
    // A declared path may carry interior whitespace — `validateAreaNodes`
    // (internal/config/areas.go) does not reject it — while this grammar splits
    // tokens on whitespace and has no quoting. Accepting `Web UI` writes
    // `area:Web` plus a bare `UI` in free text: the area filter lost, drag reorder
    // disabled (`isDragAllowed`, filter.ts), and the chip naming `area:Web`, a
    // token the user never typed. So the menu offers only what it can insert.
    // Whether the grammar should quote instead, or the config forbid the space, is
    // nibs-52z1.
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

  // Existence values (`has:parent`, `is:blocked`). Scalar — no comma multi-value —
  // so the whole post-colon run is the segment. Negated tokens are excluded for the
  // same reason their field names are.
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

  // Value completion for a known metadata field; unknown fields get no static
  // suggestions (relationship-id values are the caller's async typeahead).
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
