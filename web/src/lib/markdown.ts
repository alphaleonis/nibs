import DOMPurify from "dompurify";
import { Marked } from "marked";
import type { Token, Tokens } from "marked";

export type MentionResolver = (token: string) => string | null;

// --- task-list checkbox stamping ------------------------------------------
//
// GitHub-style task-list items (`- [ ]` / `- [x]`) render as CLICKABLE
// checkboxes. Each rendered checkbox carries its document-order ORDINAL in a
// `data-task-ordinal` attribute; ActiveNibView's delegated click handler maps
// that ordinal back to a SOURCE line in the working copy (see `toggleTaskLine`
// / `taskSourceLines`) and flips the marker there.
//
// The ordinal is assigned by OVERRIDING marked's `checkbox` renderer (below):
// marked invokes it exactly once per real GFM task item, in document order.
// `taskSourceLines` derives the ordinal -> source-line map from the SAME
// tokenizer (`sharedMarked.lexer`) that renders, walking task items in the same
// document order. Because rendering and mapping now share ONE parser, rendered
// ordinal N maps to `taskSourceLines(body)[N]` for blockquotes, fenced/indented/
// tab/HTML-block code, ordered-list markers, nesting, duplicates, prose/code
// that echoes `- [ ]` text, and every line ending (CRLF, lone CR, LF, mixed) —
// constructs where a second, independent line-scanner would drift.
//
// `taskOrdinalCounter` is reset at the top of every `renderMarkdown` call.
// `Marked.parse` is synchronous, so nothing interleaves between the reset and
// the renderer's reads.
let taskOrdinalCounter = 0;

// Per-render provenance nonce. Body content is authored/stored BEFORE render,
// so it cannot contain this freshly generated value: a raw-HTML `<input>` a body
// smuggles in (even one forging `type="checkbox"` and a `data-task-ordinal`)
// lacks the nonce and is stripped at the sanitize boundary, so it can neither
// become a clickable control nor alias/shift a real task ordinal. Only the
// checkboxes WE emit for real task items carry it. Reset before every parse.
let renderNonce = "";

function freshNonce(): string {
  const g = globalThis as { crypto?: { randomUUID?: () => string } };
  if (typeof g.crypto?.randomUUID === "function") return g.crypto.randomUUID();
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}${Math.random()
    .toString(36)
    .slice(2)}`;
}

// Module-scoped DOMPurify instance (NOT the shared global default export) so our
// input-hardening hook governs only this module's sanitize calls and never leaks
// onto other consumers or accumulates across HMR / module re-evaluation.
const purify = DOMPurify(window);

// Shared Marked instance — no per-call allocation. We do not register a
// mention extension on this instance: all mention rewriting happens in the
// post-sanitize DOM walk below, which is the single authoritative path.
const sharedMarked = new Marked();
sharedMarked.use({
  renderer: {
    checkbox({ checked }: { checked: boolean }): string {
      const ordinal = taskOrdinalCounter++;
      // Emitted ENABLED (no `disabled`) so the box is clickable. The click is
      // handled via delegation on the prose container; the checked state is
      // driven by the re-render after the source flip, not the native toggle.
      // `data-task-nonce` proves provenance (see `renderNonce`); the sanitize
      // hook strips it from the final DOM.
      return `<input type="checkbox" data-task-ordinal="${ordinal}" data-task-nonce="${renderNonce}"${checked ? " checked" : ""}> `;
    },
  },
});

// Input hardening + provenance: the ONLY <input> permitted in rendered nib
// bodies is a GFM task-list checkbox emitted by the renderer above (identified
// by the current per-render nonce). Strip every other <input> — a wrong/absent
// nonce (raw-HTML forgery) or a non-checkbox type — so a body can never present
// a stray/interactive form control OR forge a clickable task checkbox. NOTE this
// is input-TYPE + provenance filtering only: DOMPurify's defaults still pass
// non-interactive controls like <button>/<select>/<textarea>/<details>, but
// event-handler attributes are stripped, so none carries a click handler.
purify.addHook("uponSanitizeElement", (node, data) => {
  if (data.tagName !== "input") return;
  const el = node as Element;
  const isCheckbox = (el.getAttribute("type") ?? "").toLowerCase() === "checkbox";
  const trusted = renderNonce !== "" && el.getAttribute("data-task-nonce") === renderNonce;
  if (!isCheckbox || !trusted) {
    el.remove();
    return;
  }
  // Don't leak the provenance nonce into the rendered DOM.
  el.removeAttribute("data-task-nonce");
});

/**
 * Map each rendered task-list checkbox to its 0-based SOURCE line in `body`,
 * in document (== rendered ordinal) order. `taskSourceLines(body)[N]` is the
 * line the Nth rendered checkbox lives on.
 *
 * Derived from `sharedMarked.lexer` — the SAME parser used to render — so the
 * per-checkbox COUNT can never drift from the rendered ordinals: marked emits
 * exactly one `checkbox` token per real GFM task item and the renderer stamps
 * exactly one `<input>` per such token, so we record exactly one source line
 * per checkbox token here, in the same document order.
 *
 * How the LINE of each checkbox is found — and why it never back-matches into a
 * code/HTML/prose echo of `- [ ]`:
 *
 *   * We do NOT search the source for a marker's text. We accumulate LINE
 *     offsets over marked's own token tree. marked guarantees that a run of
 *     sibling `.raw` slices concatenates EXACTLY to their parent's text (holds
 *     for the pinned marked major — see the empirical probe in the round-3
 *     redesign notes), so the number of `\n`s in a token's `.raw` is exactly the
 *     number of line breaks it spans. Advancing a running line counter by that
 *     count lands the next sibling on its true source line.
 *   * `.raw` is indent-STRIPPED for nested content (a list nested inside a list
 *     item; a blockquote's inner list) — but stripping removes only leading
 *     spaces, never `\n`s. So newline COUNTS (and therefore line offsets) stay
 *     exact at every nesting depth, even though absolute character offsets would
 *     not. This is why the design counts lines instead of chasing char offsets.
 *   * A `checkbox` token is marked's task marker. It is the FIRST inline token of
 *     its task item (tight lists) or of the item's first paragraph (loose
 *     lists) and contains no `\n`, so it sits on its item's own start line.
 *   * A `- [ ]` echoed inside a fenced/indented code block, an HTML block, or
 *     prose is NOT a `checkbox` token — it is inert text living inside a
 *     `code` / `html` / `text` token's `.raw`. We only ever COUNT the newlines
 *     in such a `.raw`; we never descend into it and never search it. So an echo
 *     can never be recorded, and a real checkbox's line can never be resolved
 *     onto one. (The three prior `indexOf`-search rounds all failed exactly
 *     here: a later task back-matched onto an earlier fenced `- [ ]`.)
 *
 * Line endings are normalized the way marked's lexer normalizes them — `\r\n`
 * AND a lone `\r` each collapse to a single `\n` — before line indices are
 * computed, so a classic-Mac or mixed-ending body still maps each checkbox to
 * its own line (and those indices align 1:1 with `toggleTaskLine`'s
 * terminator-preserving split).
 */
export function taskSourceLines(body: string): number[] {
  // Normalize EXACTLY as marked's lexer does (`\r\n` and a lone `\r` -> `\n`) so
  // line indices line up 1:1 with the terminator-preserving split in
  // `toggleTaskLine` (which treats each CRLF / lone CR / LF as a single line).
  const norm = body.replace(/\r\n?/g, "\n");
  const tokens = sharedMarked.lexer(norm);

  // Children of a container token: a `list` holds them in `.items`; every other
  // container (`list_item`, `blockquote`, `paragraph`, `text`, …) holds them in
  // `.tokens`. Leaf tokens (`code`, `html`, `space`, `hr`, `checkbox`, …) have
  // neither — so we never descend into a code/HTML block's `- [ ]` text.
  const childrenOf = (t: Token): Token[] | undefined =>
    t.type === "list" ? (t as Tokens.List).items : (t as { tokens?: Token[] }).tokens;

  const countNewlines = (s: string): number => {
    let n = 0;
    for (let i = 0; i < s.length; i++) if (s.charCodeAt(i) === 10) n++;
    return n;
  };

  const lines: number[] = [];

  // Walk a run of sibling tokens whose FIRST token begins on absolute source
  // line `startLine`, in document order. A `checkbox` sits on the current line;
  // every other container is descended into at its own start line; then the
  // line counter advances by the token's own newline count (indent-safe, since
  // dedenting nested `.raw` never removes `\n`s) to reach the next sibling.
  const walk = (siblings: Token[], startLine: number): void => {
    let line = startLine;
    for (const token of siblings) {
      if (token.type === "checkbox") {
        lines.push(line);
      } else {
        const kids = childrenOf(token);
        if (kids && kids.length > 0) walk(kids, line);
      }
      line += countNewlines(token.raw);
    }
  };
  walk(tokens, 0);
  return lines;
}

/**
 * Toggle the Nth (0-based `ordinal`) GFM task-list checkbox in `body`, flipping
 * its marker `[ ]` <-> `[x]` while preserving indentation, bullet style, and all
 * surrounding text. Returns the body unchanged when `ordinal` is out of range.
 *
 * The ordinal -> source-line mapping comes from `taskSourceLines`, which shares
 * marked's tokenizer with the renderer, so `ordinal` here always equals the
 * ordinal stamped on the rendered checkbox — for blockquotes, fenced/indented/
 * tab/HTML-block code, ordered-list markers, nesting, duplicates, and all line
 * endings (CRLF, lone CR, LF, mixed). On the resolved line, the first
 * `[ ]`/`[x]`/`[X]` marker is the checkbox (a task item's marker precedes its
 * content), so we flip that one.
 */
export function toggleTaskLine(body: string, ordinal: number): string {
  if (ordinal < 0) return body;
  const lineIdxs = taskSourceLines(body);
  if (ordinal >= lineIdxs.length) return body;

  // Split into alternating [content, terminator, content, …, content] so line N's
  // content sits at index N*2. `taskSourceLines` collapses `\r\n`, a lone `\r`,
  // and `\n` each to one line break, so its line indices align with these content
  // segments — and rejoining with the CAPTURED terminators preserves the body's
  // original endings (CRLF stays CRLF, a lone CR stays a lone CR) rather than
  // rewriting them all to LF.
  const parts = body.split(/(\r\n|\r|\n)/);
  const contentIdx = lineIdxs[ordinal] * 2;
  const line = parts[contentIdx];
  const m = /\[([ xX])\]/.exec(line);
  // Defensive: only fires if the resolved line has literally NO `[ ]`/`[x]`/`[X]`
  // marker at all (it should always have one — `taskSourceLines` resolves a real
  // task item's marker line). It does NOT guard against a WRONG marker-bearing
  // line; that soundness is `taskSourceLines`' responsibility.
  if (!m) return body;
  const at = m.index + 1; // the marker char inside the brackets
  const flipped = m[1] === " " ? "x" : " ";
  parts[contentIdx] = line.slice(0, at) + flipped + line.slice(at + 1);
  return parts.join("");
}

/**
 * Post-sanitize DOM walk that rewrites `#<id>` tokens in text nodes. This is
 * the single authoritative mention-rewriting path: we do not use a marked
 * inline extension because block-level raw HTML in the body would bypass the
 * inline lexer entirely. Running over the sanitized DOM instead lets us catch
 * every text node regardless of how marked tokenized the source.
 *
 * We skip subtrees that must preserve literal text (`<code>`, `<pre>`) and
 * avoid rewriting inside existing anchors (`<a>`) — the latter also prevents
 * re-processing anchors we ourselves created if the walk is ever re-run.
 */
function postProcessMentionsInDom(html: string, resolve: MentionResolver): string {
  if (typeof document === "undefined") return html;
  const tpl = document.createElement("template");
  tpl.innerHTML = html;

  const SKIP = new Set(["CODE", "PRE", "A"]);

  function walk(node: Node): void {
    if (node.nodeType === Node.ELEMENT_NODE) {
      const el = node as Element;
      if (SKIP.has(el.tagName)) return;
      // iterate over a snapshot; replaceChild mutates childNodes
      const children = Array.from(node.childNodes);
      for (const child of children) walk(child);
      return;
    }
    if (node.nodeType === Node.DOCUMENT_FRAGMENT_NODE) {
      const children = Array.from(node.childNodes);
      for (const child of children) walk(child);
      return;
    }
    if (node.nodeType !== Node.TEXT_NODE) return;
    const text = node.nodeValue ?? "";
    if (!text.includes("#")) return;

    // Local regex — avoids sharing `/g` lastIndex across recursive walk calls.
    const pattern = /(^|[^A-Za-z0-9_])#([a-z0-9][a-z0-9-]*[a-z0-9])(?!-)/g;
    let lastIndex = 0;
    const frag = document.createDocumentFragment();
    let anyMatch = false;
    let m: RegExpExecArray | null;
    while ((m = pattern.exec(text)) !== null) {
      const token = m[2];
      let fullId: string | null;
      try {
        fullId = resolve(token);
      } catch {
        // Fail-open: treat resolver exceptions as unresolved so the body still
        // renders rather than blowing up the whole markdown pipeline.
        fullId = null;
      }
      if (!fullId) continue;
      anyMatch = true;
      const matchStart = m.index + m[1].length; // skip the boundary char
      if (matchStart > lastIndex) {
        frag.appendChild(document.createTextNode(text.slice(lastIndex, matchStart)));
      }
      const a = document.createElement("a");
      a.setAttribute("data-nib-id", fullId);
      a.setAttribute("href", `#${fullId}`);
      a.setAttribute("class", "mention-link text-link");
      a.textContent = `#${token}`;
      frag.appendChild(a);
      lastIndex = matchStart + 1 + token.length; // `#` + token
    }
    if (!anyMatch) return;
    if (lastIndex < text.length) {
      frag.appendChild(document.createTextNode(text.slice(lastIndex)));
    }
    node.parentNode?.replaceChild(frag, node);
  }

  walk(tpl.content);
  return tpl.innerHTML;
}

/**
 * Render markdown body to sanitized HTML.
 *
 * GFM task-list items (`- [ ]` / `- [x]`) render as ENABLED, clickable
 * `<input type="checkbox" data-task-ordinal="N">` controls (N = document order).
 * They are interactive by design and do NOT persist a toggle on their own: a
 * delegated handler must map the ordinal to a source line and flip it (see
 * `ActiveNibView.handleProseClick` + `toggleTaskLine`/`taskSourceLines`). A
 * read-only reuse (table cell, tooltip, print view) must either wire that
 * handler or disable the checkboxes, or clicks will toggle visually without
 * persisting and reset on the next re-render.
 *
 * When a `resolveMention` callback is provided, `#<id>` tokens in body text are
 * rewritten into `<a data-nib-id="…">` anchors for any token the callback resolves
 * (returning the full nib ID). Unresolved tokens remain as plain text. Code spans
 * and fenced code blocks are never rewritten.
 *
 * Omitting the resolver yields the original plain-render behavior (used where
 * mention resolution isn't needed).
 *
 * @param resolveMention Optional callback invoked with each mention token
 *   (the raw text after `#`). Returns the full nib ID when resolved, or
 *   null when unresolved. MUST return a trusted, constrained nib ID
 *   (matching /^[a-z0-9][a-z0-9-]*$/) or null. Do NOT pass user-controlled
 *   content as the return value — it is interpolated into `href` and
 *   `data-nib-id` attributes. DOM APIs (setAttribute, textContent) escape
 *   automatically, but an unconstrained return value could still produce
 *   confusing HTML. The resolver should NOT throw; any thrown error is
 *   caught and treated as unresolved (fail-open).
 */
export function renderMarkdown(body: string, resolveMention?: MentionResolver): string {
  if (!body) return "";
  // Reset the task-ordinal counter and mint a fresh provenance nonce before
  // parsing; the checkbox renderer reads both as marked emits each task item,
  // and the sanitize hook reads the nonce. parse() + sanitize() are synchronous,
  // so nothing interleaves between these writes and those reads.
  taskOrdinalCounter = 0;
  renderNonce = freshNonce();
  const html = sharedMarked.parse(body, { async: false });
  // DOMPurify allows data-* attributes by default; specifying explicitly for
  // clarity since `data-nib-id` (mention delegation) and `data-task-ordinal`
  // (task-list toggle delegation) are both load-bearing for click handling.
  const safe = purify.sanitize(html, { ADD_ATTR: ["data-nib-id", "data-task-ordinal"] });
  if (!resolveMention) return safe;
  return postProcessMentionsInDom(safe, resolveMention);
}

export const TAG_REGEX = /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/;
