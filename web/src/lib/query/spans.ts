import { fieldSpec, isValidValue } from "./fields";
import { FIELD_TOKEN } from "./parse";
import { recognizeRelationship } from "./relations";
import type { RelMatch } from "./relations";
import { isRefusedArea, recognizeArea } from "./area";
import type { AreaMatch } from "./area";
import type { AreaVocabulary } from "../areas";

// Highlight span kinds:
// - `field`      — a recognized token's name, `-` included (`-tags`, `blocked-by`, `has`).
// - `operator`   — the `:` after a field and the `,` between values.
// - `value`      — a value the parser accepts.
// - `invalid`    — what `parseQuery` puts in `invalidTokens`: a rejected value (the
//                  span covers the value), or a negated rel/existence/area token (the
//                  whole token).
// - `freetext`   — everything `parseQuery` routes to `search`, `type:` and `type:,` included.
// - `whitespace` — a whitespace run.
export type SpanKind = "field" | "operator" | "value" | "invalid" | "freetext" | "whitespace";

export interface Span {
  /** Inclusive start offset into the source text. */
  start: number;
  /** Exclusive end offset into the source text. */
  end: number;
  kind: SpanKind;
}

/**
 * Split query text into highlight spans, using `parseQuery`'s recognizers in the
 * same order so the coloring matches what the parser did.
 *
 * The spans tile the text — ordered, non-empty, gap-free — so
 * `spans.map(s => text.slice(s.start, s.end)).join("") === text`. The backdrop
 * relies on this to stay aligned with the input.
 *
 * `areas` checks `area:` values. Absent or answering "unknown", an area value is
 * colored as accepted, as `parseQuery` keeps it.
 */
export function tokenizeSpans(text: string, areas?: AreaVocabulary): Span[] {
  const spans: Span[] = [];
  // Alternating whitespace / non-whitespace runs: `parseQuery`'s split, with offsets.
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

// Append the spans for one non-whitespace token starting at `base`, routed as
// `parseQuery` routes it.
function classifyToken(token: string, base: number, out: Span[], areas?: AreaVocabulary): void {
  const match = FIELD_TOKEN.exec(token);
  const spec = match ? fieldSpec(match[2]) : undefined;
  if (!match || !spec) {
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
    out.push({ start: base, end: base + token.length, kind: "freetext" });
    return;
  }

  const fieldName = match[1] + match[2]; // optional '-' + field name
  const valuePart = match[3];
  const segments = valuePart.split(",");
  if (segments.every((seg) => seg === "")) {
    // Only empty/comma segments (`type:,`): free text, as in parseQuery.
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

// Append the spans for a token `recognizeRelationship` claimed. A negated one is a
// single `invalid` span over the whole token, which is what `parseQuery` parks.
function classifyRelToken(token: string, kind: RelMatch["kind"], base: number, out: Span[]): void {
  if (kind === "invalid") {
    out.push({ start: base, end: base + token.length, kind: "invalid" });
    return;
  }

  // Never `invalid`: recognition already checked an existence value, and whether a
  // nib id exists is a store question the parser does not ask either.
  emitScalarToken(token, base, "value", out);
}

// Append the spans for an `area:` token. A negated one is a single `invalid` span;
// otherwise the value is `invalid` when `isRefusedArea`, the check `parseQuery`
// routes on, refuses it.
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

// Append a scalar `name:value` token as name, colon, and the whole post-colon run
// as one value span, so a later `,` or `:` stays in the value as `parseQuery`
// stores it. Splits at the first colon because `FIELD_TOKEN` cannot match
// hyphenated names.
//
// Both recognizers guarantee a non-empty name and value; the fallback keeps a
// zero-length span out of the tiling regardless.
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
