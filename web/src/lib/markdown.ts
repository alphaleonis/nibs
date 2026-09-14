import DOMPurify from "dompurify";
import { Marked } from "marked";
import type { Token, Tokens } from "marked";

export type MentionResolver = (token: string) => string | null;

// --- task-list checkboxes ---------------------------------------------------
//
// GFM task items render as clickable checkboxes stamped with their document-order
// ordinal (`data-task-ordinal`). ActiveNibView maps an ordinal to a source line
// with `taskSourceLines` and flips it with `toggleTaskLine`. The renderer and
// `taskSourceLines` both use `sharedMarked`, so keep them on one parser.
//
// Reset at the top of every `renderMarkdown`; `Marked.parse` is synchronous.
let taskOrdinalCounter = 0;

// Per-render nonce carried only by the checkboxes the renderer emits. The
// sanitize hook strips every other <input>, so raw HTML in a body cannot forge a
// task checkbox. Reset before every parse.
let renderNonce = "";

function freshNonce(): string {
  const g = globalThis as { crypto?: { randomUUID?: () => string } };
  if (typeof g.crypto?.randomUUID === "function") return g.crypto.randomUUID();
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}${Math.random()
    .toString(36)
    .slice(2)}`;
}

// A private DOMPurify instance, so the hook below applies to this module only.
const purify = DOMPurify(window);

// Mentions are rewritten by the post-sanitize DOM walk, not a marked extension.
const sharedMarked = new Marked();
sharedMarked.use({
  renderer: {
    checkbox({ checked }: { checked: boolean }): string {
      const ordinal = taskOrdinalCounter++;
      // ENABLED so it is clickable; the checked state comes from re-rendering
      // after the source flips.
      return `<input type="checkbox" data-task-ordinal="${ordinal}" data-task-nonce="${renderNonce}"${checked ? " checked" : ""}> `;
    },
  },
});

// The only <input> kept is a checkbox carrying this render's nonce. Other form
// elements (<button>, <select>, <textarea>, <details>) pass DOMPurify's defaults,
// without event-handler attributes.
purify.addHook("uponSanitizeElement", (node, data) => {
  if (data.tagName !== "input") return;
  const el = node as Element;
  const isCheckbox = (el.getAttribute("type") ?? "").toLowerCase() === "checkbox";
  const trusted = renderNonce !== "" && el.getAttribute("data-task-nonce") === renderNonce;
  if (!isCheckbox || !trusted) {
    el.remove();
    return;
  }
  el.removeAttribute("data-task-nonce");
});

/**
 * The 0-based source line of each task checkbox in `body`, in rendered-ordinal
 * order: `taskSourceLines(body)[N]` is the line of the Nth checkbox.
 *
 * Walks `sharedMarked.lexer`'s token tree and counts lines instead of searching
 * the source text:
 *   * sibling `.raw` slices concatenate to their parent's text, so a token's `\n`
 *     count is the number of line breaks it spans. Nested `.raw` is
 *     indent-stripped but keeps every `\n`, so the count stays exact at any depth.
 *   * a `checkbox` token holds no `\n` and sits on its item's start line.
 *   * a `- [ ]` inside code, HTML or prose is text inside a leaf token; its
 *     newlines are counted, but it is never descended into.
 *
 * Line endings are normalized as marked's lexer does, matching `toggleTaskLine`.
 */
export function taskSourceLines(body: string): number[] {
  const norm = body.replace(/\r\n?/g, "\n");
  const tokens = sharedMarked.lexer(norm);

  // A `list` holds its children in `.items`, other containers in `.tokens`; leaf
  // tokens (`code`, `html`, `checkbox`, …) have neither.
  const childrenOf = (t: Token): Token[] | undefined =>
    t.type === "list" ? (t as Tokens.List).items : (t as { tokens?: Token[] }).tokens;

  const countNewlines = (s: string): number => {
    let n = 0;
    for (let i = 0; i < s.length; i++) if (s.charCodeAt(i) === 10) n++;
    return n;
  };

  const lines: number[] = [];

  // `siblings[0]` starts on source line `startLine`.
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
 * Flip the Nth (0-based) task checkbox in `body` between `[ ]` and `[x]`, leaving
 * all other text intact. Returns `body` unchanged when `ordinal` is out of range.
 * The first `[ ]`/`[x]`/`[X]` on the line `taskSourceLines` gives is the marker.
 */
export function toggleTaskLine(body: string, ordinal: number): string {
  if (ordinal < 0) return body;
  const lineIdxs = taskSourceLines(body);
  if (ordinal >= lineIdxs.length) return body;

  // Line N's content is parts[N*2]; rejoining with the captured terminators keeps
  // the original line endings.
  const parts = body.split(/(\r\n|\r|\n)/);
  const contentIdx = lineIdxs[ordinal] * 2;
  const line = parts[contentIdx];
  const m = /\[([ xX])\]/.exec(line);
  if (!m) return body;
  const at = m.index + 1; // the marker char inside the brackets
  const flipped = m[1] === " " ? "x" : " ";
  parts[contentIdx] = line.slice(0, at) + flipped + line.slice(at + 1);
  return parts.join("");
}

/**
 * Rewrite `#<id>` tokens in the sanitized DOM's text nodes into mention links.
 * Not a marked extension, because block-level raw HTML bypasses the inline lexer.
 * Skips `<code>`, `<pre>` and existing `<a>`.
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
        // A throwing resolver leaves the token unresolved.
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
 * Render a markdown body to sanitized HTML.
 *
 * Task items render as ENABLED checkboxes with `data-task-ordinal` and persist
 * nothing on their own. A caller must wire a handler that calls `toggleTaskLine`
 * (as ActiveNibView.handleProseClick does) or disable them.
 *
 * @param resolveMention Called with each mention token (the text after `#`);
 *   returns the full nib ID, or null to leave it as text. Return only a trusted
 *   nib ID: it lands in `href` and `data-nib-id`. A throw counts as unresolved.
 *   Mentions inside code are never rewritten.
 */
export function renderMarkdown(body: string, resolveMention?: MentionResolver): string {
  if (!body) return "";
  taskOrdinalCounter = 0;
  renderNonce = freshNonce();
  const html = sharedMarked.parse(body, { async: false });
  // data-* passes by default; listed because click delegation depends on both.
  const safe = purify.sanitize(html, { ADD_ATTR: ["data-nib-id", "data-task-ordinal"] });
  if (!resolveMention) return safe;
  return postProcessMentionsInDom(safe, resolveMention);
}

export const TAG_REGEX = /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/;
