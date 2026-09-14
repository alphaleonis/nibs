import { FIELD_SPECS, expandValue, fieldSpec, isValidValue } from "./fields";
import type { QueryFilter } from "./fields";
import { recognizeRelationship } from "./relations";
import type { RelIdKey, ExistenceKey } from "./relations";
import { AREA_FIELD, isRefusedArea, recognizeArea } from "./area";
import type { AreaVocabulary } from "../areas";

// Parsed box text. `invalidTokens` holds rejected values (`status:banana`,
// `area:retired`) and negated rel/existence/area tokens (`-ancestor:x`); they set
// nothing but are kept, lowercased except an area path, so the box can flag them
// and serialize them back.
export interface ParsedQuery {
  filter: QueryFilter;
  invalidTokens: string[];
}

// `[-]field:value`. Groups: 1 negation, 2 field name (letters only), 3 value list.
// `field:` with no value does not match. Shared with `spans.ts`.
export const FIELD_TOKEN = /^(-?)([A-Za-z]+):(.+)$/;

/**
 * Parse filter-box text into the box-owned filter and `invalidTokens`.
 *
 * - known field + valid value → its include-list, or its `exclude*` list when
 *   negated. A group name expands to its members.
 * - known field + invalid value → `invalidTokens`.
 * - comma-separated values and repeated tokens for one field union, deduplicated.
 * - relationship-id token → scalar field; existence token → boolean field. Last
 *   wins, including across a `has:`/`no:` pair. Negated → `invalidTokens`.
 * - `area:<path>` → `area`, last wins, unless `areas` answers "undeclared"
 *   (→ `invalidTokens`). Without `areas` the path is kept. Negated → `invalidTokens`.
 * - anything else, `title:foo` and `type:` included → free-text `search`.
 *
 * Names and values are lowercased, except area paths. Unset keys are omitted.
 */
export function parseQuery(text: string, areas?: AreaVocabulary): ParsedQuery {
  const includes = new Map<string, string[]>();
  const excludes = new Map<string, string[]>();
  const invalidTokens: string[] = [];
  const words: string[] = [];
  const relIds = new Map<RelIdKey, string>();
  const existence = new Map<ExistenceKey, boolean>();
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
      // Not a metadata token: try relationship, then area, then free text.
      const rel = recognizeRelationship(token);
      if (rel) {
        if (rel.kind === "id") relIds.set(rel.field, rel.value);
        else if (rel.kind === "bool") existence.set(rel.field, rel.value);
        // Negated: park it, never free text (see recognizeRelationship).
        else invalidTokens.push(rel.token);
        continue;
      }
      const areaToken = recognizeArea(token);
      if (areaToken) {
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
      // Only empty/comma segments (`type:,`): keep it as free text, as `type:` is.
      words.push(token);
      continue;
    }

    for (const value of values) {
      if (isValidValue(spec, value)) {
        for (const member of expandValue(spec, value)) {
          push(negated ? excludes : includes, spec.name, member);
        }
      } else {
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
