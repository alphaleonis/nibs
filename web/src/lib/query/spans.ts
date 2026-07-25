import { fieldSpec, isValidValue } from "./fields";
import { FIELD_TOKEN } from "./parse";

// The kind of each highlight span. A single-line query is tiled by contiguous
// spans so the backdrop layer can color every character in its exact position:
// - `field`      — the field-name part of a recognized token, `-` negation included
//                  (e.g. `type`, `-tags`).
// - `operator`   — token punctuation: the `:` after a field and the `,` between values.
// - `value`      — a value segment that is legal for its field (enum member / tag pattern).
// - `invalid`    — a value segment for a KNOWN field that fails validation (drawn with a
//                  red underline). Same rule `parseQuery` uses to fill `invalidTokens`.
// - `freetext`   — bare words, unknown `field:value` tokens, and empty-value field tokens
//                  (`type:`, `type:,`) — i.e. everything `parseQuery` routes to `search`.
// - `whitespace` — the runs of spaces between tokens (and any leading/trailing run).
export type SpanKind = "field" | "operator" | "value" | "invalid" | "freetext" | "whitespace";

export interface Span {
  /** Inclusive start offset into the source text. */
  start: number;
  /** Exclusive end offset into the source text. */
  end: number;
  kind: SpanKind;
}

/**
 * Tokenize query text into per-character-position highlight spans. This is a pure,
 * READ-ONLY view over the same tokens `parseQuery` recognizes — it reuses the exact
 * `FIELD_TOKEN` grammar plus `fieldSpec`/`isValidValue`, and never changes parse or
 * serialize semantics.
 *
 * The returned spans TILE the whole string: they are ordered, non-empty, gap-free and
 * non-overlapping, so `spans.map(s => text.slice(s.start, s.end)).join("") === text`.
 * That contiguity is what lets the backdrop stay pixel-aligned with the input — every
 * character is emitted exactly once, in order, in a same-metrics `<span>`.
 */
export function tokenizeSpans(text: string): Span[] {
  const spans: Span[] = [];
  // Alternating runs of whitespace / non-whitespace cover the whole string with
  // correct offsets — the same split boundary `parseQuery` uses (`/\s+/`), but
  // position-preserving.
  const re = /\s+|\S+/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text)) !== null) {
    const chunk = m[0];
    const start = m.index;
    if (/\s/.test(chunk[0])) {
      spans.push({ start, end: start + chunk.length, kind: "whitespace" });
    } else {
      classifyToken(chunk, start, spans);
    }
  }
  return spans;
}

// Emit the spans for one non-whitespace token starting at `base`, appending to `out`.
// Mirrors `parseQuery`'s per-token routing so the coloring matches the parse result.
function classifyToken(token: string, base: number, out: Span[]): void {
  const match = FIELD_TOKEN.exec(token);
  const spec = match ? fieldSpec(match[2]) : undefined;
  if (!match || !spec) {
    // Bare word or unknown field → free text (same as parseQuery).
    out.push({ start: base, end: base + token.length, kind: "freetext" });
    return;
  }

  const fieldName = match[1] + match[2]; // optional '-' + field name
  const valuePart = match[3];
  const segments = valuePart.split(",");
  if (segments.every((seg) => seg === "")) {
    // A known-field token whose value is only empty/comma segments (`type:,`) is
    // preserved verbatim as free text by parseQuery — color it the same.
    out.push({ start: base, end: base + token.length, kind: "freetext" });
    return;
  }

  let cursor = base;
  out.push({ start: cursor, end: cursor + fieldName.length, kind: "field" });
  cursor += fieldName.length;
  out.push({ start: cursor, end: cursor + 1, kind: "operator" }); // the ':'
  cursor += 1;

  segments.forEach((seg, i) => {
    if (seg.length > 0) {
      const kind: SpanKind = isValidValue(spec, seg.toLowerCase()) ? "value" : "invalid";
      out.push({ start: cursor, end: cursor + seg.length, kind });
    }
    cursor += seg.length;
    if (i < segments.length - 1) {
      out.push({ start: cursor, end: cursor + 1, kind: "operator" }); // a ','
      cursor += 1;
    }
  });
}
