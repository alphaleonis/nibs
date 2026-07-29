import { FIELD_SPECS, completionValues, fieldSpec } from "./fields";

export type CompletionKind = "field" | "value" | "tag";

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
 * Static autocomplete for the query input. Given the full text and caret offset,
 * return suggestions for the token being typed (the run of non-space characters
 * ending at the caret), or `null` when there is nothing to suggest:
 *
 * - a partial field name (`ty`, `-ty`) → matching field names (prefix match);
 * - `field:` / `field:partial` for a known enum → that field's group names, then
 *   its values (substring);
 * - `tags:partial` → matching entries from `availableTags` (substring);
 * - an unknown field (`title:`) or an empty token → `null`.
 *
 * Multi-value tokens complete the segment after the last comma and never
 * re-suggest a value already chosen earlier in the same token.
 */
export function getCompletion(
  text: string,
  caret: number,
  availableTags: readonly string[] = [],
): Completion | null {
  // The token-so-far: from the previous whitespace up to the caret.
  let start = caret;
  while (start > 0 && !/\s/.test(text[start - 1])) start--;
  const prefix = text.slice(start, caret);
  if (prefix === "") return null;

  const negated = prefix.startsWith("-");
  const body = negated ? prefix.slice(1) : prefix;
  const colon = body.indexOf(":");

  if (colon === -1) {
    // Field-name completion (prefix match on the five field names).
    const partial = body.toLowerCase();
    const items = FIELD_SPECS.map((s) => s.name).filter((n) => n.startsWith(partial));
    if (items.length === 0) return null;
    const before = text.slice(0, start);
    const after = text.slice(caret);
    return {
      kind: "field",
      items,
      apply: (item) => {
        const insert = `${negated ? "-" : ""}${item}:`;
        return { text: before + insert + after, caret: (before + insert).length };
      },
    };
  }

  // Value completion for a known field; unknown fields get no static suggestions.
  const spec = fieldSpec(body.slice(0, colon));
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
