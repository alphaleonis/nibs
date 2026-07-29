import { FIELD_SPECS, fieldSpec, isValidValue } from "./fields";
import type { QueryFilter } from "./fields";
import { recognizeRelationship } from "./relations";
import type { RelIdKey, ExistenceKey } from "./relations";

// The structured result of parsing filter-box text: the box-owned `filter` slice
// plus an `invalidTokens` sidecar carrying known-field tokens whose value failed
// validation (e.g. `status:banana`). Invalid tokens contribute nothing to the
// filter but are preserved verbatim (lowercased, minus kept) so the box can flag
// them and round-trip them through canonicalization and dropdown edits.
export interface ParsedQuery {
  filter: QueryFilter;
  invalidTokens: string[];
}

// A `[-]field:value` token. Group 1 = optional negation, 2 = field name (letters
// only, matched case-insensitively), 3 = the value list (`.+`, so `field:` with
// no value never matches — it falls through to free text, as in phase 1).
// Exported so the highlight tokenizer (`spans.ts`) classifies exactly the tokens
// `parseQuery` recognizes — one grammar, no drift.
export const FIELD_TOKEN = /^(-?)([A-Za-z]+):(.+)$/;

/**
 * Parse filter-box text into the structured fields the box owns plus the
 * invalid-token sidecar.
 *
 * Routing (design 2.2 + 2.4):
 * - known field + valid value → the positive include-list, or the `exclude*`
 *   list when the token is negated (`-field:value`).
 * - known field + invalid value → excluded from the filter, preserved (lowercased)
 *   in `invalidTokens`.
 * - comma splits a token into OR values; repeated same-field tokens union. Both
 *   are deduplicated (lenient-in).
 * - relationship-id token (`blocking:<id>`, `parent:<id>`, …) → a scalar id field,
 *   last-wins on repeat; existence token (`has:parent`, `no:parent`, `is:blocked`,
 *   …) → a tri-state boolean field. `has:` writes true and `no:` writes false on
 *   the SAME field, so the pair is last-wins too. Neither is negatable — a
 *   leading `-` routes to free text.
 * - unknown `field:value` (including Bleve `title:`/`body:`) and bare words →
 *   free-text `search`.
 *
 * Field names and values are lowercased. Absent keys are omitted from `filter`.
 */
export function parseQuery(text: string): ParsedQuery {
  // Accumulate include/exclude value lists per field name (encounter order).
  const includes = new Map<string, string[]>();
  const excludes = new Map<string, string[]>();
  const invalidTokens: string[] = [];
  const words: string[] = [];
  // Relationship-id scalars (last write wins) + existence booleans.
  const relIds = new Map<RelIdKey, string>();
  // Map, not Set: an existence token can write false (`no:parent`).
  const existence = new Map<ExistenceKey, boolean>();

  const push = (map: Map<string, string[]>, key: string, value: string) => {
    const list = map.get(key);
    if (list) list.push(value);
    else map.set(key, [value]);
  };

  for (const token of text.split(/\s+/)) {
    if (token === "") continue;
    const match = FIELD_TOKEN.exec(token);
    const spec = match ? fieldSpec(match[2]) : undefined;
    if (!match || !spec) {
      // Not a metadata token. Try a relationship-id / existence token (these may
      // use hyphenated field-names the metadata FIELD_TOKEN regex can't match),
      // else fall back to free text, preserved verbatim.
      const rel = recognizeRelationship(token);
      if (rel) {
        if (rel.kind === "id") relIds.set(rel.field, rel.value);
        else existence.set(rel.field, rel.value);
      } else {
        words.push(token);
      }
      continue;
    }

    const negated = match[1] === "-";
    const values = match[3]
      .split(",")
      .map((v) => v.toLowerCase())
      .filter((v) => v !== "");

    if (values.length === 0) {
      // A known-field token whose value is only empty/comma segments (e.g.
      // `type:,`) yields no values. Preserve it verbatim as free text rather than
      // dropping it silently — same treatment as `field:` with no value and
      // unknown fields, so nothing the user typed is lost and it round-trips.
      words.push(token);
      continue;
    }

    for (const value of values) {
      if (isValidValue(spec, value)) {
        push(negated ? excludes : includes, spec.name, value);
      } else {
        // Preserve the exact (normalized) token so it survives round-trips.
        invalidTokens.push(`${negated ? "-" : ""}${spec.name}:${value}`);
      }
    }
  }

  const filter: QueryFilter = {};
  for (const spec of FIELD_SPECS) {
    const inc = includes.get(spec.name);
    if (inc) filter[spec.filterKey] = dedupe(inc);
    const exc = excludes.get(spec.name);
    if (exc) filter[spec.excludeKey] = dedupe(exc);
  }
  for (const [field, value] of relIds) {
    filter[field] = value;
  }
  for (const [field, value] of existence) {
    filter[field] = value;
  }
  const search = words.join(" ");
  if (search !== "") filter.search = search;

  return { filter, invalidTokens: dedupe(invalidTokens) };
}

function dedupe(values: string[]): string[] {
  return [...new Set(values)];
}
