import { describe, it, expect } from "vitest";
import {
  FILTER_COMPLETION_CONTRACT,
  FILTER_SPAN_CONTRACT,
} from "../generated/filterInputContract";
import { getCompletion, relTokenValueContext, tokenizeSpans } from "./index";
import { createAreaVocabulary } from "../areas";

/**
 * The web half of the highlighting and completion parity tables; the Go half is
 * internal/filterlangcontract. It is the only consumer of the generated cases.
 */
describe("Go↔TS parity for filter highlighting", () => {
  it.each(FILTER_SPAN_CONTRACT.map((c) => [c.name, c] as const))("%s", (_name, c) => {
    const areas = c.areas === null ? undefined : createAreaVocabulary(c.areas);
    expect(tokenizeSpans(c.text, areas)).toEqual(c.spans);
  });
});

describe("Go↔TS parity for filter completion", () => {
  it.each(FILTER_COMPLETION_CONTRACT.map((c) => [c.name, c] as const))("%s", (_name, c) => {
    const areas = c.areas === null ? undefined : createAreaVocabulary(c.areas);
    expect(relTokenValueContext(c.text, c.cursor)).toEqual(c.rel);
    const completion = getCompletion(c.text, c.cursor, c.tags, { explicit: c.explicit, areas });
    expect(
      completion && {
        kind: completion.kind,
        items: completion.items,
        applied: completion.items.map((item) => completion.apply(item)),
      },
    ).toEqual(c.completion);
  });
});
