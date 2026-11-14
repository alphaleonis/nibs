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

// --- mention scan markers ---------------------------------------------------
//
// The mention walk reads DOM text, where the source's backslashes and block
// boundaries are gone. These empty spans carry them, so the walk finds the
// mentions the server's scanner (internal/nib/references.go) finds:
//   * `split` stands where a backslash escape was. The element splits the text
//     node there, so `[\[x]]` does not read as `[[x]]` while `\[[x]]` still does.
//   * `off` and `on` enclose a raw HTML block, whose text the server skips.
// Markers carry the render nonce, so raw HTML in a body cannot forge one. They
// are emitted only when a resolver will run the walk, which removes them.
// `renderMarkdown` checks what it can of how they read back (see there).
type ScanMarker = "split" | "off" | "on";
const SCAN_ATTR = "data-mention-scan";
const SCAN_NONCE_ATTR = "data-mention-nonce";
let emitScanMarkers = false;
// Markers emitted in the current render; reset before every parse.
let emittedScanMarkers = 0;

function scanMarker(kind: ScanMarker): string {
  emittedScanMarkers++;
  return `<span ${SCAN_ATTR}="${kind}" ${SCAN_NONCE_ATTR}="${renderNonce}"></span>`;
}

const HTML_ESCAPES: Record<string, string> = {
  "&": "&amp;",
  "<": "&lt;",
  ">": "&gt;",
  '"': "&quot;",
  "'": "&#39;",
};

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
    // marked renders an escape token through `text`. An image's alt text uses a
    // separate text renderer, so no marker lands in an attribute.
    text(token: Tokens.Text | Tokens.Escape): string | false {
      if (!emitScanMarkers || token.type !== "escape") return false;
      return scanMarker("split") + token.text.replace(/[&<>"']/g, (c) => HTML_ESCAPES[c]);
    },
    html(token: Tokens.HTML | Tokens.Tag): string | false {
      if (!emitScanMarkers || !token.block) return false;
      return scanMarker("off") + token.text + scanMarker("on");
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

/** Sanitized HTML for a body, and the scan state its first node starts in. */
interface SanitizedRender {
  html: string;
  /** The render nonce when the HTML carries scan markers, or null. */
  nonce: string | null;
  /** How many scan markers the HTML held before it was sanitized. */
  markerCount: number;
  startsInRawBlock: boolean;
}

function renderSanitized(body: string, withMarkers: boolean): SanitizedRender {
  taskOrdinalCounter = 0;
  renderNonce = freshNonce();
  emitScanMarkers = withMarkers;
  emittedScanMarkers = 0;
  let html = sharedMarked.parse(body, { async: false });
  // A leading marker would open <body> before a leading raw HTML block, so a
  // body that starts with a <style> would parse differently.
  let markerCount = emittedScanMarkers;
  const leadingOff = scanMarker("off");
  const startsInRawBlock = withMarkers && html.startsWith(leadingOff);
  if (startsInRawBlock) {
    html = html.slice(leadingOff.length);
    markerCount--;
  }
  // data-* passes by default; listed because click delegation depends on both.
  const safe = purify.sanitize(html, { ADD_ATTR: ["data-nib-id", "data-task-ordinal"] });
  return { html: safe, nonce: withMarkers ? renderNonce : null, markerCount, startsInRawBlock };
}

function toTemplate(html: string): HTMLTemplateElement {
  const tpl = document.createElement("template");
  tpl.innerHTML = html;
  return tpl;
}

/**
 * Parses the marked render of `body` and removes its scan markers.
 *
 * @returns The parsed render and the text nodes inside raw HTML blocks, or null
 *   when the render without markers does not serialize the same as `plain`, a
 *   marker was lost, or block starts and ends do not alternate.
 */
function readScanMarkers(
  body: string,
  plain: string,
): { tpl: HTMLTemplateElement; rawText: Set<Node> } | null {
  const render = renderSanitized(body, true);
  const tpl = toTemplate(render.html);
  const rawText = new Set<Node>();
  const markers: Element[] = [];
  let inRawBlock = render.startsInRawBlock;
  const walker = document.createTreeWalker(tpl.content, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node !== null; node = walker.nextNode()) {
    if (node.nodeType === Node.TEXT_NODE) {
      if (inRawBlock) rawText.add(node);
      continue;
    }
    const el = node as Element;
    const isMarker =
      el.tagName === "SPAN" &&
      el.attributes.length === 2 &&
      !el.hasChildNodes() &&
      el.getAttribute(SCAN_NONCE_ATTR) === render.nonce;
    if (!isMarker) continue;
    markers.push(el);
    // Raw HTML blocks do not nest, so `off` and `on` alternate unless one was lost.
    const kind = el.getAttribute(SCAN_ATTR);
    if (kind === "off" || kind === "on") {
      if (inRawBlock === (kind === "off")) return null;
      inRawBlock = kind === "off";
    }
  }
  if (inRawBlock || markers.length !== render.markerCount) return null;
  for (const marker of markers) marker.remove();
  // Text-node splits do not serialize, so any difference is one the markers
  // caused in the parse or the sanitize.
  if (tpl.innerHTML !== toTemplate(plain).innerHTML) return null;
  return { tpl, rawText };
}

/**
 * Whether renderMarkdown reads the mentions of `body` with the scan markers.
 * False means it reads them from the unmarked render. Exported for tests.
 */
export function readsWithScanMarkers(body: string): boolean {
  return readScanMarkers(body, renderSanitized(body, false).html) !== null;
}

/**
 * Rewrite `#<id>` and `[[<id>]]` tokens in the text nodes of `tpl` into mention
 * links. A bracketed link shows the bare id.
 * Not a marked extension, because block-level raw HTML bypasses the inline lexer.
 * Skips `<code>`, `<pre>`, existing `<a>` and the text nodes in `rawText`.
 */
function linkMentions(tpl: HTMLTemplateElement, resolve: MentionResolver, rawText: Set<Node>): string {
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
    if (node.nodeType !== Node.TEXT_NODE || rawText.has(node)) return;
    const text = node.nodeValue ?? "";
    if (!text.includes("#") && !text.includes("[[")) return;

    // Local regex — avoids sharing `/g` lastIndex across recursive walk calls.
    // Group 1 is a hash id, group 2 a bracketed one, both in the server's id
    // grammar (MentionIDPattern). The hash boundary is a lookbehind, so a `]]`
    // closing the previous match still counts as one.
    const pattern =
      /(?<![A-Za-z0-9_])#([a-z0-9](?:[a-z0-9-]*[a-z0-9])?)|\[\[([a-z0-9](?:[a-z0-9-]*[a-z0-9])?)\]\]/g;
    let lastIndex = 0;
    const frag = document.createDocumentFragment();
    let anyMatch = false;
    let m: RegExpExecArray | null;
    while ((m = pattern.exec(text)) !== null) {
      const bracketed = m[2] !== undefined;
      const token = bracketed ? m[2] : m[1];
      let fullId: string | null;
      try {
        fullId = resolve(token);
      } catch {
        // A throwing resolver leaves the token unresolved.
        fullId = null;
      }
      if (!fullId) continue;
      anyMatch = true;
      if (m.index > lastIndex) {
        frag.appendChild(document.createTextNode(text.slice(lastIndex, m.index)));
      }
      const a = document.createElement("a");
      a.setAttribute("data-nib-id", fullId);
      a.setAttribute("href", `#${fullId}`);
      a.setAttribute("class", "mention-link text-link");
      a.textContent = bracketed ? token : `#${token}`;
      frag.appendChild(a);
      lastIndex = m.index + m[0].length;
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
 * With a resolver, the body renders twice: once with scan markers and once
 * without. The marked render is used only when every marker it emitted reads
 * back, block starts and ends alternate, and its sanitized HTML without the
 * markers is identical to the unmarked render. Otherwise the mentions are read
 * from the unmarked render, where escapes and raw HTML blocks link as plain text
 * does.
 *
 * @param resolveMention Called with each mention token; returns the full nib ID,
 *   or null to leave it as text. Return only a trusted nib ID: it lands in
 *   `href` and `data-nib-id`. A throw counts as unresolved. Mentions inside
 *   code are never rewritten.
 */
export function renderMarkdown(body: string, resolveMention?: MentionResolver): string {
  if (!body) return "";
  const plain = renderSanitized(body, false).html;
  if (!resolveMention || typeof document === "undefined") return plain;
  const marked = readScanMarkers(body, plain);
  if (marked !== null) return linkMentions(marked.tpl, resolveMention, marked.rawText);
  return linkMentions(toTemplate(plain), resolveMention, new Set());
}

export const TAG_REGEX = /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/;
