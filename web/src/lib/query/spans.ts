import { fieldSpec, isValidValue } from "./fields";
import { FIELD_TOKEN } from "./parse";
import { recognizeRelationship } from "./relations";
import type { RelMatch } from "./relations";

// The kind of each highlight span. A single-line query is tiled by contiguous
// spans so the backdrop layer can color every character in its exact position:
// - `field`      — the field-name part of a recognized token, `-` negation included
//                  (e.g. `type`, `-tags`). Also the name of a recognized relationship
//                  or existence token (`parent`, `blocked-by`, `has`, `is`).
// - `operator`   — token punctuation: the `:` after a field and the `,` between values.
// - `value`      — a value segment that is legal for its field (enum member / tag pattern /
//                  existence dimension / relationship id).
// - `invalid`    — a token the parser recognized but REJECTED, drawn with a red underline.
//                  The same rule `parseQuery` uses to fill `invalidTokens`: a known
//                  field's failed value (the span covers just that value), or a negated
//                  rel/existence token (the span covers the whole token).
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
 * `FIELD_TOKEN` grammar plus `fieldSpec`/`isValidValue` for metadata tokens and
 * `recognizeRelationship` for the rel/existence ones, in that same order, and never
 * changes parse or serialize semantics. Sharing the recognizers (not copies of their
 * vocabularies) is what keeps the overlay from claiming a token did nothing when the
 * parser in fact acted on it.
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
    // Not a metadata token. Try the relationship/existence grammar next, in the
    // same order `parseQuery` does, and only fall through to free text when that
    // also declines. Routing through `recognizeRelationship` (rather than reading
    // the lookup tables here) is what keeps the coloring from drifting: one
    // recognizer answers "did this token do anything?" for both layers.
    const rel = recognizeRelationship(token);
    if (!rel) {
      // Bare word or unknown field → free text (same as parseQuery).
      out.push({ start: base, end: base + token.length, kind: "freetext" });
      return;
    }
    classifyRelToken(token, rel.kind, base, out);
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

// Emit the spans for a token `recognizeRelationship` claimed: a relationship-id
// token (`blocked-by:tnib-1`), an existence token (`has:parent`, `is:blocked`), or
// a negated one it rejected (`-ancestor:tnib-1`).
//
// NEGATED TOKENS ARE `invalid`, not free text, and the span covers the WHOLE token.
// `parseQuery` parks them in its `invalidTokens` sidecar, which the box already
// renders as an "Unrecognized:" warning chip — so leaving them muted made the
// overlay contradict a warning that was on screen at the same moment. The whole
// token is marked because the whole token is what gets parked, and because the
// fault is the negation itself rather than the value: underlining only the value
// would aim the red at the wrong glyphs. This is the one token shape that gains a
// red underline here; every non-negated rel/existence spelling gains only positive
// color, never an error mark.
function classifyRelToken(token: string, kind: RelMatch["kind"], base: number, out: Span[]): void {
  if (kind === "invalid") {
    out.push({ start: base, end: base + token.length, kind: "invalid" });
    return;
  }

  // Both accepted shapes split at the FIRST colon: the name (hyphens included —
  // `blocked-by`, which FIELD_TOKEN's `[A-Za-z]+` group cannot match) then the
  // value, taken whole. A rel id is SCALAR, so an embedded comma or second colon
  // belongs to the value and is not an operator — exactly what `parseQuery` stores.
  const colon = token.indexOf(":");
  if (colon <= 0 || colon === token.length - 1) {
    // Unreachable through `recognizeRelationship`: the id path requires a colon at
    // index > 0 with a non-empty remainder, and every existence spelling is exactly
    // `word:value`. Guarded anyway, because an empty name or value would emit a
    // zero-length span and break the tiling invariant the backdrop depends on.
    out.push({ start: base, end: base + token.length, kind: "freetext" });
    return;
  }

  out.push({ start: base, end: base + colon, kind: "field" });
  out.push({ start: base + colon, end: base + colon + 1, kind: "operator" });
  // The value is `value`, never a separate "recognized field, unchecked value" kind.
  // For an existence token the value is genuinely validated — the legal set is the
  // closed `EXISTENCE_TOKENS` vocabulary, and anything outside it never reaches
  // here. For a relationship id, non-empty is the ONLY condition the grammar
  // imposes, and the token has already met it by being recognized at all; whether
  // that id exists is a store question, and this module is pure and synchronous by
  // design. A third color would therefore report "we did not check" — a distinction
  // the user cannot act on, and one that would drift from the parser, which accepts
  // the value outright and writes it straight to the filter.
  out.push({ start: base + colon + 1, end: base + token.length, kind: "value" });
}
