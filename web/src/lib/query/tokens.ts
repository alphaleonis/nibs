import { tokenizeSpans } from "./spans";
import type { Span } from "./spans";
import type { AreaVocabulary } from "../areas";

// Token/gap segments of the query text: the `tokenizeSpans` spans of each
// non-whitespace run form one `token`, each whitespace run is a `gap`. Segments
// tile the text as the spans do.
export interface TokenSegment {
  kind: "token" | "gap";
  /** Inclusive start offset into the source text. */
  start: number;
  /** Exclusive end offset into the source text. */
  end: number;
}

/**
 * `tokenGroups` without the spans, for the click layer. `areas` changes span kinds,
 * not boundaries; pass the same value as to `tokenGroups`.
 */
export function tokenSegments(text: string, areas?: AreaVocabulary): TokenSegment[] {
  return tokenGroups(text, areas).map(({ kind, start, end }) => ({ kind, start, end }));
}

/** A segment with the spans it covers, for the highlight backdrop. */
export interface TokenGroup extends TokenSegment {
  /**
   * Whether the run contains a `field` span, which earns it a chip. Bare words and
   * parked whole-token invalids (`-ancestor:x`) have none; `status:banana` does.
   */
  structured: boolean;
  /** The spans in this segment. All groups' spans concatenated equal
   *  `tokenizeSpans(text)`. */
  spans: Span[];
  /**
   * Index into `spans` where the value run starts: everything after the field's
   * colon, commas included, so a multi-value token fills as one well. `-1` for
   * gaps, bare words and whole-token invalids.
   */
  valueRunStart: number;
}

/** Group `tokenizeSpans(text)` into token/gap groups, one per backdrop wrapper. */
export function tokenGroups(text: string, areas?: AreaVocabulary): TokenGroup[] {
  const groups: TokenGroup[] = [];
  for (const span of tokenizeSpans(text, areas)) {
    if (span.kind === "whitespace") {
      groups.push({
        kind: "gap",
        start: span.start,
        end: span.end,
        structured: false,
        spans: [span],
        valueRunStart: -1,
      });
      continue;
    }
    const last = groups[groups.length - 1];
    if (last && last.kind === "token" && last.end === span.start) {
      last.end = span.end;
      last.spans.push(span);
    } else {
      groups.push({
        kind: "token",
        start: span.start,
        end: span.end,
        structured: false,
        spans: [span],
        valueRunStart: -1,
      });
    }
    if (span.kind === "field") groups[groups.length - 1].structured = true;
  }

  // The run opens after the first operator, the field's colon; later operators
  // are commas inside the run.
  for (const g of groups) {
    if (!g.structured) continue;
    const colon = g.spans.findIndex((s) => s.kind === "operator");
    if (colon >= 0 && colon + 1 < g.spans.length) g.valueRunStart = colon + 1;
  }

  return groups;
}
