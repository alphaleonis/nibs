import { FIELD_SPECS, expandValue, fieldSpec, isValidValue } from "./fields";
import type { QueryFilter } from "./fields";
import { recognizeRelationship } from "./relations";
import type { RelIdKey, ExistenceKey } from "./relations";
import { AREA_FIELD, isRefusedArea, recognizeArea } from "./area";
import type { AreaVocabulary } from "../areas";

// The structured result of parsing filter-box text: the box-owned `filter` slice
// plus an `invalidTokens` sidecar carrying known-field tokens whose value failed
// validation (e.g. `status:banana`, `area:retired`) and negated rel/existence/area
// tokens, which the grammar recognizes but cannot express (e.g. `-ancestor:x`).
// Invalid tokens contribute nothing to the filter but are preserved with their
// field-name normalized and the minus kept — an area PATH keeps its case, as it
// does everywhere — so the box can flag them and round-trip them through
// canonicalization and dropdown edits.
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
 *   list when the token is negated (`-field:value`). A group name is a valid
 *   value and expands to its members (`status:open` → draft, todo, in-progress),
 *   so the filter only ever carries concrete values.
 * - known field + invalid value → excluded from the filter, preserved (lowercased)
 *   in `invalidTokens`.
 * - comma splits a token into OR values; repeated same-field tokens union. Both
 *   are deduplicated (lenient-in).
 * - relationship-id token (`blocking:<id>`, `parent:<id>`, …) → a scalar id field,
 *   last-wins on repeat; existence token (`has:parent`, `no:parent`, `is:blocked`,
 *   …) → a tri-state boolean field. `has:` writes true and `no:` writes false on
 *   the SAME field, so the pair is last-wins too. Neither is negatable — a
 *   leading `-` on an otherwise-recognized rel/existence token parks it in
 *   `invalidTokens` (free text would reach Bleve and match everything).
 * - `area:<path>` → the scalar `area` field, last-wins on repeat, when the
 *   supplied `areas` vocabulary does not answer "undeclared" for it; an
 *   undeclared path is parked in `invalidTokens` like any other rejected value,
 *   and a negated `-area:` is parked whole (there is no `excludeArea`). With no
 *   vocabulary the answer is "unknown", which keeps the value — see
 *   `isRefusedArea`.
 * - unknown `field:value` (including Bleve `title:`/`body:`) and bare words →
 *   free-text `search`.
 *
 * Field names and values are lowercased, except an area PATH, which is
 * case-sensitive on the server (query/area.ts). Absent keys are omitted from
 * `filter`.
 */
export function parseQuery(text: string, areas?: AreaVocabulary): ParsedQuery {
  // Accumulate include/exclude value lists per field name (encounter order).
  const includes = new Map<string, string[]>();
  const excludes = new Map<string, string[]>();
  const invalidTokens: string[] = [];
  const words: string[] = [];
  // Relationship-id scalars (last write wins) + existence booleans.
  const relIds = new Map<RelIdKey, string>();
  // Map, not Set: an existence token can write false (`no:parent`).
  const existence = new Map<ExistenceKey, boolean>();
  // The ownership axis: one scalar path, last write wins like a rel id.
  let area: string | undefined;

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
      // then the area token, then fall back to free text, preserved verbatim.
      // The three blocks are tried in the order `serializeQuery` emits them.
      const rel = recognizeRelationship(token);
      if (rel) {
        if (rel.kind === "id") relIds.set(rel.field, rel.value);
        else if (rel.kind === "bool") existence.set(rel.field, rel.value);
        // A negated rel/existence token. It contributes nothing to the filter and
        // must NOT become free text (that would reach Bleve and silently match
        // everything — see recognizeRelationship); park it so the box flags it.
        else invalidTokens.push(rel.token);
        continue;
      }
      const areaToken = recognizeArea(token);
      if (areaToken) {
        // A negated `-area:` is parked for the same reason a negated rel token
        // is; an undeclared path is parked the way `status:banana` is, so the
        // box flags it and it survives canonicalization instead of vanishing.
        if (areaToken.kind === "invalid") invalidTokens.push(areaToken.token);
        else if (isRefusedArea(areaToken.value, areas)) {
          invalidTokens.push(`${AREA_FIELD}:${areaToken.value}`);
        } else area = areaToken.value;
        continue;
      }
      words.push(token);
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
        // A group name stands for its members (`status:open` → draft, todo,
        // in-progress); every other legal value stands for itself.
        for (const member of expandValue(spec, value)) {
          push(negated ? excludes : includes, spec.name, member);
        }
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
  if (area !== undefined) filter.area = area;
  const search = words.join(" ");
  if (search !== "") filter.search = search;

  return { filter, invalidTokens: dedupe(invalidTokens) };
}

function dedupe(values: string[]): string[] {
  return [...new Set(values)];
}
