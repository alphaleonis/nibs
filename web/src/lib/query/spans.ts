import { fieldSpec, isValidValue } from "./fields";
import { FIELD_TOKEN } from "./parse";
import { recognizeRelationship } from "./relations";
import type { RelMatch } from "./relations";
import { isRefusedArea, recognizeArea } from "./area";
import type { AreaMatch } from "./area";
import type { AreaVocabulary } from "../areas";

// The kind of each highlight span. A single-line query is tiled by contiguous
// spans so the backdrop layer can color every character in its exact position:
// - `field`      — the field-name part of a recognized token, `-` negation included
//                  (e.g. `type`, `-tags`). Also the name of a recognized relationship
//                  or existence token (`parent`, `blocked-by`, `has`, `is`).
// - `operator`   — token punctuation: the `:` after a field and the `,` between values.
// - `value`      — a value segment that is legal for its field (enum member / tag pattern /
//                  existence dimension / relationship id / an area path the vocabulary
//                  does not refuse).
// - `invalid`    — a token the parser recognized but REJECTED, drawn with a red underline.
//                  The same rule `parseQuery` uses to fill `invalidTokens`: a known
//                  field's failed value or an undeclared area path (the span covers just
//                  that value), or a negated rel/existence/area token (the span covers
//                  the whole token).
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
 *
 * `areas` is the runtime vocabulary an `area:` value is checked against, and is
 * the one input here that is not a constant. Absent, or answering "unknown", it
 * colors an area value like any other accepted value — the same withholding
 * `parseQuery` performs, so a token the parser kept is never drawn as an error.
 */
export function tokenizeSpans(text: string, areas?: AreaVocabulary): Span[] {
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
      classifyToken(chunk, start, spans, areas);
    }
  }
  return spans;
}

// Emit the spans for one non-whitespace token starting at `base`, appending to `out`.
// Mirrors `parseQuery`'s per-token routing so the coloring matches the parse result.
function classifyToken(token: string, base: number, out: Span[], areas?: AreaVocabulary): void {
  const match = FIELD_TOKEN.exec(token);
  const spec = match ? fieldSpec(match[2]) : undefined;
  if (!match || !spec) {
    // Not a metadata token. Try the relationship/existence grammar next, then the
    // area token, in the same order `parseQuery` does, and only fall through to
    // free text when both decline. Routing through the recognizers (rather than
    // reading the lookup tables here) is what keeps the coloring from drifting:
    // one recognizer answers "did this token do anything?" for both layers.
    const rel = recognizeRelationship(token);
    if (rel) {
      classifyRelToken(token, rel.kind, base, out);
      return;
    }
    const areaToken = recognizeArea(token);
    if (areaToken) {
      classifyAreaToken(token, areaToken, base, out, areas);
      return;
    }
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
// would aim the red at the wrong glyphs. Every non-negated rel/existence spelling
// gains only positive color, never an error mark — there is nothing here to check
// a nib id against.
function classifyRelToken(token: string, kind: RelMatch["kind"], base: number, out: Span[]): void {
  if (kind === "invalid") {
    out.push({ start: base, end: base + token.length, kind: "invalid" });
    return;
  }

  // The value is `value`, never a separate "recognized field, unchecked value" kind.
  // For an existence token the value is genuinely validated — the legal set is the
  // closed `EXISTENCE_TOKENS` vocabulary, and anything outside it never reaches
  // here. For a relationship id, non-empty is the ONLY condition the grammar
  // imposes, and the token has already met it by being recognized at all; whether
  // that id exists is a store question, and this module is pure and synchronous by
  // design. A third color would therefore report "we did not check" — a distinction
  // the user cannot act on, and one that would drift from the parser, which accepts
  // the value outright and writes it straight to the filter.
  emitScalarToken(token, base, "value", out);
}

// Emit the spans for an `area:` token: the whole token as `invalid` when it is
// the negated form `parseQuery` parks, else name / colon / value — with the value
// marked `invalid` exactly when the parser refused it.
//
// The value's color is decided by `isRefusedArea`, the same call `parseQuery`
// routes on, so an undeclared path cannot be parked as invalid while rendering as
// accepted. An area is the one token value this module CAN check, because the
// vocabulary is data the client holds; a rel id would need the store.
function classifyAreaToken(
  token: string,
  areaToken: AreaMatch,
  base: number,
  out: Span[],
  areas: AreaVocabulary | undefined,
): void {
  if (areaToken.kind === "invalid") {
    out.push({ start: base, end: base + token.length, kind: "invalid" });
    return;
  }
  const kind: SpanKind = isRefusedArea(areaToken.value, areas) ? "invalid" : "value";
  emitScalarToken(token, base, kind, out);
}

// Emit the spans for a scalar `name:value` token — the name, its colon, and the
// whole post-colon run as ONE value span. Shared by the relationship and area
// tokens, which are the same shape and differ only in what that run may be
// colored. It splits at the FIRST colon rather than reusing FIELD_TOKEN, whose
// `[A-Za-z]+` name group cannot match a hyphenated rel name (`blocked-by`), and
// takes the run whole — which is what makes an embedded comma or second colon part
// of the value rather than an operator, matching what `parseQuery` stores.
//
// The guard is unreachable through either recognizer — both require a colon at
// index > 0 with a non-empty remainder — but an empty name or value would emit a
// zero-length span and break the tiling invariant the backdrop depends on, so the
// token falls back to one free-text span instead.
function emitScalarToken(token: string, base: number, valueKind: SpanKind, out: Span[]): void {
  const colon = token.indexOf(":");
  if (colon <= 0 || colon === token.length - 1) {
    out.push({ start: base, end: base + token.length, kind: "freetext" });
    return;
  }
  out.push({ start: base, end: base + colon, kind: "field" });
  out.push({ start: base + colon, end: base + colon + 1, kind: "operator" });
  out.push({ start: base + colon + 1, end: base + token.length, kind: valueKind });
}
