import { describe, it, expect } from "vitest";
import { FILTER_PARSE_CONTRACT } from "../generated/filterParseContract";
import { parseQuery, serializeQuery } from "./index";
import { createAreaVocabulary } from "../areas";

/**
 * The web half of the filter-parsing parity table; the Go half is
 * internal/filterlangcontract. It is the only consumer of the generated cases.
 */
describe("Go↔TS parity for filter parsing", () => {
  it.each(FILTER_PARSE_CONTRACT.map((c) => [c.name, c] as const))("%s", (_name, c) => {
    const areas = c.areas === null ? undefined : createAreaVocabulary(c.areas);
    const parsed = parseQuery(c.text, areas);
    expect(parsed).toEqual({ filter: c.filter, invalidTokens: c.invalidTokens });
    expect(serializeQuery(parsed)).toBe(c.canonical);
  });
});
