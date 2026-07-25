import { tokenizeSpans } from "./spans";

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
  const segs: TokenSegment[] = [];
  for (const span of tokenizeSpans(text)) {
    if (span.kind === "whitespace") {
      segs.push({ kind: "gap", start: span.start, end: span.end });
      continue;
    }
    const last = segs[segs.length - 1];
    if (last && last.kind === "token" && last.end === span.start) {
      last.end = span.end;
    } else {
      segs.push({ kind: "token", start: span.start, end: span.end });
    }
  }
  return segs;
}

/**
 * Remove the token occupying `[start, end)` from `text` and collapse the now-adjacent
 * whitespace so no leading, trailing, or double space is left behind:
 *  - removing the FIRST token drops its trailing separator (no leading space),
 *  - removing a MIDDLE token leaves exactly one space between the neighbors,
 *  - removing the LAST token drops its leading separator (no trailing space),
 *  - removing the ONLY token yields the empty string.
 *
 * Pure: the caller feeds the result back through the box's parse/emit path, so an
 * invalid token (which lives in the query string like any other) drops out of the
 * invalid sidecar via the re-parse — no special casing here.
 */
export function removeTokenRange(text: string, start: number, end: number): string {
  const before = text.slice(0, start).replace(/\s+$/, "");
  const after = text.slice(end).replace(/^\s+/, "");
  if (before === "") return after;
  if (after === "") return before;
  return `${before} ${after}`;
}
