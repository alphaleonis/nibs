import { tokenizeSpans } from "./spans";
import type { Span } from "./spans";

// A token/gap segmentation of the query text, derived from `tokenizeSpans`. Where
// `tokenizeSpans` splits a token into its field/operator/value parts, this coarser
// view groups each maximal run of non-whitespace back into a single `token`
// segment (one filter token like `type:bug`, `-tags:wip`, `blocking:tnib-1`, or a
// bare word) and keeps the whitespace runs as `gap` segments. Segments tile the
// whole string in order, gap-free and non-overlapping — the same contiguity
// `tokenizeSpans` guarantees — so a click-affordance layer can mirror the input's
// glyph flow exactly while giving each token one hit-region.
export interface TokenSegment {
  /** "token" = a non-whitespace run; "gap" = a whitespace run. */
  kind: "token" | "gap";
  /** Inclusive start offset into the source text. */
  start: number;
  /** Exclusive end offset into the source text. */
  end: number;
}

/**
 * Group `tokenizeSpans(text)` into alternating token / gap segments. Consecutive
 * non-whitespace spans (field + operator + value + …) merge into one `token`
 * segment; each whitespace span becomes a `gap`. The result tiles the whole string
 * (`segs.map(s => text.slice(s.start, s.end)).join("") === text`).
 */
export function tokenSegments(text: string): TokenSegment[] {
  return tokenGroups(text).map(({ kind, start, end }) => ({ kind, start, end }));
}

/** A token/gap segment that also carries the spans it covers, plus whether the
 *  backdrop should draw a chip around it. */
export interface TokenGroup extends TokenSegment {
  /**
   * True when this run parsed into STRUCTURE — it contains a `field` span. That is
   * what earns a chip, and it is deliberately narrower than "the parser did
   * something with it":
   *
   * - a bare word is free text, and chipping it would claim a structure it has
   *   none of (GitHub leaves its bare words unchipped for the same reason);
   * - a parked whole-token invalid (`-ancestor:x`, which `parseQuery` keeps in
   *   `invalidTokens`) has no field span either, so it keeps its red wavy
   *   underline WITHOUT being dressed up as a working token.
   *
   * A known field with a bad value (`status:banana`) does have a field span, so it
   * chips — the underline on the value is what marks it wrong, not the absence of
   * a chip.
   */
  structured: boolean;
  /** The spans this segment covers, in order. Concatenating every group's spans
   *  reproduces `tokenizeSpans(text)` exactly, so offsets are untouched. */
  spans: Span[];
  /**
   * Index into `spans` where the VALUE RUN begins — everything after the field's
   * colon, to the end of the token. `-1` when there is no run.
   *
   * The backdrop recesses this range into a darker well and leaves the field name
   * on the plain surface, so the fill marks exactly the part that carries meaning.
   * It is a RUN rather than the individual value spans on purpose: filling those
   * one by one breaks at the comma, and `status:todo,in-progress` then reads as two
   * separate pills instead of one token.
   *
   * `-1` covers the cases that must not be marked at all — a gap, a bare word, and
   * a parked whole-token invalid (`-ancestor:x`), which keeps its wavy underline
   * without being dressed up as a working token.
   */
  valueRunStart: number;
}

/**
 * Group `tokenizeSpans(text)` into alternating token / gap groups, keeping each
 * group's spans. This is the backdrop's view: it renders one wrapper per token so
 * a chip can span field + operator + value, with the per-span coloring inside.
 *
 * `tokenSegments` is the same grouping with the spans dropped, so the click layer
 * and the highlight layer can never disagree about where a token starts and ends.
 */
export function tokenGroups(text: string): TokenGroup[] {
  const groups: TokenGroup[] = [];
  for (const span of tokenizeSpans(text)) {
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

  // The run opens right after the FIRST operator — the field's colon. Later
  // operators are the commas between values and belong inside the run, which is
  // what keeps the fill continuous across a multi-value token. A structured token
  // with nothing after its colon (not reachable through `classifyToken`, which
  // routes an empty value to free text) leaves the run closed.
  for (const g of groups) {
    if (!g.structured) continue;
    const colon = g.spans.findIndex((s) => s.kind === "operator");
    if (colon >= 0 && colon + 1 < g.spans.length) g.valueRunStart = colon + 1;
  }

  return groups;
}
