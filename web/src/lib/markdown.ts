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

function renderSanitized(body: string): string {
  taskOrdinalCounter = 0;
  renderNonce = freshNonce();
  const html = sharedMarked.parse(body, { async: false });
  // data-* passes by default; listed because click delegation depends on both.
  return purify.sanitize(html, { ADD_ATTR: ["data-nib-id", "data-task-ordinal"] });
}

function toTemplate(html: string): HTMLTemplateElement {
  const tpl = document.createElement("template");
  tpl.innerHTML = html;
  return tpl;
}

// --- mentions ---------------------------------------------------------------
//
// The server's scanner (internal/nib/references.go) decides which text is a
// mention, because the mention index and prefix rewrites use its answer. The web
// takes that answer from marked's tokens, then links it where the rendered DOM
// shows the same text. How the browser parses raw HTML decides only where a
// mention can be linked, never which text is one.

// The server's id grammar (MentionIDPattern).
const MENTION_ID = "[a-z0-9](?:[a-z0-9-]*[a-z0-9])?";
const LEADING_ID = new RegExp(`^${MENTION_ID}`);

/**
 * A run of text that looks like a mention: `#` with the longest id after it, or
 * `[[<id>]]`. Its key is that text, so the token side and the DOM side compare.
 */
interface Candidate {
  key: string;
  index: number;
}

function findCandidates(text: string): Candidate[] {
  // Group 1 is a hash id, group 2 a bracketed one. A fresh regex per call keeps
  // `/g` state local.
  const pattern = new RegExp(`#(${MENTION_ID})|\\[\\[(${MENTION_ID})\\]\\]`, "g");
  const out: Candidate[] = [];
  for (let m = pattern.exec(text); m !== null; m = pattern.exec(text)) {
    out.push({ key: m[0], index: m.index });
  }
  return out;
}

/**
 * Where an expected candidate's text comes from. Markdown `text` is where the
 * DOM shows it. `raw` HTML holds no mentions. `skipped` is markdown text inside
 * an inline element the DOM walk skips, which the browser may still close first.
 */
type CandidateSource = "text" | "raw" | "skipped";

/** A candidate the rendered text shows, and the id it mentions, or null when it is not a mention. */
interface ExpectedCandidate {
  key: string;
  mention: string | null;
  source: CandidateSource;
}

// The DOM walk skips these elements: code, existing links, and the elements whose
// content the browser reads as text. The serializer writes a link in one of the
// latter as markup that re-parses as text.
const RAW_TEXT_TAGS = new Set(["iframe", "noembed", "noframes", "plaintext", "script", "style", "textarea", "title", "xmp"]);
const SKIPPED_TAGS = new Set(["code", "pre", "a", "noscript", ...RAW_TEXT_TAGS]);

/** Returns the text nodes under `root` that the mention walk reads, in document order. */
function visibleTextNodes(root: Node): Text[] {
  const out: Text[] = [];
  const walk = (node: Node): void => {
    if (node.nodeType === Node.TEXT_NODE) {
      out.push(node as Text);
      return;
    }
    if (node.nodeType === Node.ELEMENT_NODE && SKIPPED_TAGS.has((node as Element).localName)) return;
    // jsdom rebuilds a read `childNodes` list on each insert, so linking many mentions would be O(n²).
    for (let child = node.firstChild; child; child = child.nextSibling) walk(child);
  };
  walk(root);
  return out;
}

/**
 * Returns the candidates in `body` in document order, each with the mention the
 * server finds there.
 *
 * The text of code, links, images and link definitions is skipped, as the
 * server skips it. Raw HTML holds no mentions, but the candidates in the text it
 * shows are kept, so the DOM text pairs with them and links nothing.
 */
function expectedMentions(body: string): ExpectedCandidate[] {
  const tokens = sharedMarked.lexer(body);
  const out: ExpectedCandidate[] = [];
  collectExpected(tokens, { out, leading: tokens[0], references: new Map(), htmlLines: serverHtmlLines(body) }, [], 0);
  return out;
}

interface CollectContext {
  out: ExpectedCandidate[];
  /** The body's first token, which the sanitizer may parse into the document head. */
  leading: Token | undefined;
  /** The text each character reference in the body shows, by the reference. */
  references: Map<string, string>;
  /** Whether the server reads each source line, by its 0-based number, as part of an HTML block. */
  htmlLines: Uint8Array;
}

function countNewlines(s: string): number {
  let n = 0;
  for (let i = 0; i < s.length; i++) if (s.charCodeAt(i) === 10) n++;
  return n;
}

/**
 * `openInline` holds the skipped elements an inline tag opened in the current
 * block. It ends with the block, though the browser keeps a raw-text element,
 * an `a` or a `code` open into later blocks. `tokens[0]` starts on source line
 * `startLine`.
 */
function collectExpected(tokens: Token[], ctx: CollectContext, openInline: string[], startLine: number): void {
  // Adjacent text and escape tokens render as one DOM text node.
  let text = "";
  let breaks: number[] = [];
  let decoded = new Set<number>();
  // The source lines the buffered text starts and ends on.
  let firstLine = -1;
  let lastLine = -1;
  const flush = (): void => {
    const source: CandidateSource = openInline.length === 0 ? "text" : "skipped";
    let line = firstLine;
    let from = 0;
    for (const c of text === "" ? [] : segmentMentions(text, breaks, decoded)) {
      // A newline that a character reference shows is not a line break in the source.
      for (; from < c.index; from++) if (text.charCodeAt(from) === 10 && !decoded.has(from)) line++;
      const inHtml = ctx.htmlLines[Math.min(line, lastLine)] === 1;
      ctx.out.push({ key: c.key, mention: inHtml ? null : c.mention, source });
    }
    text = "";
    breaks = [];
    decoded = new Set();
    firstLine = -1;
  };
  let line = startLine;
  for (const token of tokens) {
    const tokenLine = line;
    line += countNewlines(token.raw);
    if ((token.type === "text" && !(token as Tokens.Text).tokens) || token.type === "escape") {
      if (firstLine < 0) firstLine = tokenLine;
      lastLine = line;
    }
    if (token.type === "text" && !(token as Tokens.Text).tokens) {
      const shown = shownText(token as Tokens.Text, ctx.references);
      for (const i of shown.decoded) {
        breaks.push(text.length + i);
        decoded.add(text.length + i);
      }
      text += shown.text;
      continue;
    }
    if (token.type === "escape") {
      breaks.push(text.length);
      text += token.text;
      continue;
    }
    flush();
    switch (token.type) {
      case "code":
      case "codespan":
      case "link":
      case "image":
      case "def":
        break;
      case "html":
        if ((token as Tokens.HTML | Tokens.Tag).block) {
          for (const c of htmlCandidates(token.text, token === ctx.leading)) ctx.out.push(c);
        } else {
          trackInlineTag(token.text, openInline);
        }
        break;
      case "list":
        collectExpected((token as Tokens.List).items, ctx, [], tokenLine);
        break;
      case "table": {
        // A table's rows follow its header and delimiter lines, one row a line.
        const table = token as Tokens.Table;
        for (const cell of table.header) collectExpected(cell.tokens, ctx, [], tokenLine);
        table.rows.forEach((row, r) => {
          for (const cell of row) collectExpected(cell.tokens, ctx, [], tokenLine + 2 + r);
        });
        break;
      }
      case "em":
      case "strong":
      case "del":
        collectExpected((token as Tokens.Em).tokens, ctx, openInline, tokenLine);
        break;
      default: {
        const kids = (token as { tokens?: Token[] }).tokens;
        if (kids) collectExpected(kids, ctx, [], tokenLine);
      }
    }
  }
  flush();
}

/**
 * Returns the candidates in the text that raw `html` shows once sanitized, none
 * of them a mention. The sanitizer parses a body's leading HTML into the head,
 * so other HTML is parsed as body content, as it is in the whole render.
 */
function htmlCandidates(html: string, leading: boolean): ExpectedCandidate[] {
  // A candidate needs a `#` or `[[`, written out or as a character reference.
  if (!html.includes("#") && !html.includes("[[") && !html.includes("&")) return [];
  const out: ExpectedCandidate[] = [];
  const tpl = toTemplate(purify.sanitize(html, { FORCE_BODY: !leading }));
  for (const node of visibleTextNodes(tpl.content)) {
    for (const c of findCandidates(node.nodeValue ?? "")) out.push({ key: c.key, mention: null, source: "raw" });
  }
  return out;
}

/** Updates `open`, the skipped elements open in a block, with one inline tag. */
function trackInlineTag(tag: string, open: string[]): void {
  const m = /^<(\/?)([A-Za-z][A-Za-z0-9-]*)/.exec(tag);
  const name = m?.[2].toLowerCase();
  // Sanitizing removes a noscript element but can keep its content as text.
  if (m === null || name === undefined || name === "noscript" || !SKIPPED_TAGS.has(name)) return;
  const top = open.at(-1);
  // Only its own end tag ends an element whose content is text.
  if (top !== undefined && RAW_TEXT_TAGS.has(top) && !(m[1] === "/" && name === top)) return;
  if (m[1] === "") open.push(name);
  else if (open.includes(name)) open.length = open.lastIndexOf(name);
}

// The server's markdown parser is goldmark, whose HTML block start and end conditions differ from marked's:
// https://github.com/yuin/goldmark/blob/v1.8.5/parser/html_block.go#L13-L214
const SERVER_BLOCK_TAGS = new Set(
  (
    "address article aside base basefont blockquote body caption center col colgroup dd details dialog dir div dl dt " +
    "fieldset figcaption figure footer form frame frameset h1 h2 h3 h4 h5 h6 head header hr html iframe legend li " +
    "link main menu menuitem meta nav noframes ol optgroup option p param search section summary table tbody td " +
    "tfoot th thead title tr track ul"
  ).split(" "),
);
// The `u` flag folds case as Go's `(?i)` does, so `ſ` matches `s`.
const SERVER_HTML_TYPE1_OPEN = /^ {0,3}<(?:script|pre|style|textarea)(?:[\t\f\r ][^\n]*|\/?>[^\n]*|)$/iu;
const SERVER_HTML_TYPE1_CLOSE = /<\/(?:script|pre|style|textarea)>/iu;
// Types 2 to 5: a comment, a processing instruction, a declaration and CDATA, each with the text that ends it.
const SERVER_HTML_DELIMITED: [RegExp, string][] = [
  [/^ {0,3}<!--/, "-->"],
  [/^ {0,3}<\?/, "?>"],
  [/^ {0,3}<![A-Z]/, ">"],
  [/^ {0,3}<!\[CDATA\[/, "]]>"],
];
const SERVER_ATTRIBUTE = `(?:[\\r\\n \\t]+[a-zA-Z_:][a-zA-Z0-9:._-]*(?:[\\r\\n \\t]*=[\\r\\n \\t]*(?:[^"'=<>\`\\x00-\\x20]+|'[^']*'|"[^"]*"))?)`;
// goldmark's `[a-zA-Z]+[a-zA-Z0-9\-]*` matches the same tags, but backtracks quadratically in JavaScript.
const SERVER_HTML_TYPE7 = new RegExp(`^ {0,3}<(/ *)?([a-zA-Z][a-zA-Z0-9-]*)(${SERVER_ATTRIBUTE}*) *\\/?> *$`);
const SERVER_HTML_TYPE6 = /^ {0,3}<(?:\/ *)?([a-zA-Z][a-zA-Z0-9-]*)(?: [^\n]*|\/?>[^\n]*|)$/;
const SERVER_FENCE = /^ {0,3}(`{3,}(?=[^`]*$)|~{3,})/;
const SERVER_FENCE_CLOSE = /^ {0,3}(`+|~+)[ \t]*$/;
// A list marker, then the spaces before the item's content, or none when the item is empty or its content is indented code.
const SERVER_LIST_ITEM = /^(?<marker> {0,3}(?:[-+*]|(?<number>\d{1,9})[.)]))(?:(?<empty>[ \t]*$)|(?<gap> {1,4})(?=[^ ])|(?= {5}))/;
const SERVER_NOT_PARAGRAPH = /^ {0,3}(?:#{1,6}(?:[ \t]|$)|(?:-[ \t]*){3,}$|(?:\*[ \t]*){3,}$|(?:_[ \t]*){3,}$)/;

/** The groups of a `SERVER_LIST_ITEM` match. */
interface ListItem {
  marker: string;
  number?: string;
  gap?: string;
  empty?: string;
}

/**
 * Returns how the server's HTML block that starts on `line` ends, or null when
 * none starts there. It is "blank" for a block that ends before the next blank
 * line, or else a test that accepts the block's last line. `paragraph` is
 * whether the line before is paragraph text.
 */
function serverHtmlBlockStart(line: string, paragraph: boolean): "blank" | ((line: string) => boolean) | null {
  if (SERVER_HTML_TYPE1_OPEN.test(line)) return (l) => SERVER_HTML_TYPE1_CLOSE.test(l);
  for (const [open, close] of SERVER_HTML_DELIMITED) if (open.test(line)) return (l) => l.includes(close);
  const m = SERVER_HTML_TYPE7.exec(line);
  if (m !== null) {
    const tag = m[2].toLowerCase();
    if (SERVER_BLOCK_TAGS.has(tag)) return "blank";
    // goldmark lets a textarea tag start a type 7 block, and reads an end tag as one only when no space follows its slash.
    const closeWithAttributes = m[1] === "/" && m[3] !== "";
    if (!paragraph && tag !== "script" && tag !== "style" && tag !== "pre" && !closeWithAttributes) return "blank";
  }
  const m6 = SERVER_HTML_TYPE6.exec(line);
  return m6 !== null && SERVER_BLOCK_TAGS.has(m6[1].toLowerCase()) ? "blank" : null;
}

/**
 * Returns whether `content`, a line without the list markers before it, is
 * paragraph text that a later line can continue. Text in a blockquote is, and
 * a blockquote's heading, fence or HTML block is not.
 */
function serverParagraphText(content: string): boolean {
  const inner = content.replace(/^(?: {0,3}> ?)+/, "");
  return (
    !/^[ \t]*$/.test(inner) &&
    !SERVER_NOT_PARAGRAPH.test(inner) &&
    serverHtmlBlockStart(inner, false) === null &&
    !SERVER_FENCE.test(inner)
  );
}

/**
 * Returns whether the server reads each line of `body` as part of an HTML block,
 * by the 0-based number marked gives the line.
 *
 * It follows list items by their content columns, and ends a block or a fence
 * where its list item ends. It reads a blockquote line as paragraph text, and
 * a line with two list markers as opening only the first item.
 */
function serverHtmlLines(body: string): Uint8Array {
  // goldmark ends a line only at `\n`.
  const lines = body.split("\n").map((l) => (l.endsWith("\r") ? l.slice(0, -1) : l));
  const html = new Uint8Array(lines.length);
  let open: ReturnType<typeof serverHtmlBlockStart> = null;
  let fence = "";
  // The content column of the list item the open block or fence is in, or 0 outside a list.
  let openItem = 0;
  let paragraph = false;
  // The content columns of the open list items, outermost first.
  const items: number[] = [];
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const blank = /^[ \t\r]*$/.test(line);
    const indent = /^ */.exec(line)?.[0].length ?? 0;
    // A block or a fence takes no lazy line, so a line indented less than its list item's content ends both.
    const inItem = blank || indent >= openItem;
    if (open !== null) {
      if (inItem && !(open === "blank" && blank)) {
        html[i] = 1;
        if (open !== "blank" && open(line)) open = null;
        continue;
      }
      open = null;
    }
    if (fence !== "") {
      if (inItem) {
        const close = SERVER_FENCE_CLOSE.exec(line.slice(openItem));
        if (close !== null && close[1][0] === fence[0] && close[1].length >= fence.length) fence = "";
        continue;
      }
      fence = "";
    }
    if (blank) {
      paragraph = false;
      continue;
    }
    // The line belongs to the deepest list item whose content column it reaches, unless it continues a paragraph.
    let depth = items.length;
    while (depth > 0 && items[depth - 1] > indent) depth--;
    const column = depth > 0 ? items[depth - 1] : 0;
    const rest = line.slice(column);
    const item = (SERVER_NOT_PARAGRAPH.test(rest) ? null : (SERVER_LIST_ITEM.exec(rest)?.groups ?? null)) as ListItem | null;
    // A new list cannot interrupt a paragraph with an empty item, or with a numbered one that does not start at 1.
    const newList = items.length === depth;
    const interrupts: boolean = !paragraph || !newList || (item?.empty === undefined && Number(item?.number ?? 1) === 1);
    const marker: ListItem | null = interrupts ? item : null;
    const start: ReturnType<typeof serverHtmlBlockStart> = marker === null ? serverHtmlBlockStart(rest, paragraph) : null;
    const opened: RegExpExecArray | null = marker === null && start === null ? SERVER_FENCE.exec(rest) : null;
    // A blockquote line ends the list items it is not indented into, though it reads as paragraph text here.
    const quote = /^ {0,3}>/.test(rest);
    const lazy: boolean = paragraph && marker === null && start === null && opened === null && !quote && !SERVER_NOT_PARAGRAPH.test(rest);
    if (!lazy) items.length = depth;
    if (marker !== null) {
      const content = column + marker.marker.length + (marker.gap?.length ?? 1);
      items.push(content);
      // The text after the marker can open a block, or be indented code, which is not paragraph text.
      const first: string = marker.gap === undefined ? "" : rest.slice(marker.marker.length + marker.gap.length);
      const inner = first === "" ? null : serverHtmlBlockStart(first, false);
      const innerFence = first === "" || inner !== null ? null : SERVER_FENCE.exec(first);
      if (inner !== null) {
        html[i] = 1;
        open = inner !== "blank" && inner(first) ? null : inner;
        openItem = content;
      } else if (innerFence !== null) {
        fence = innerFence[1];
        openItem = content;
      }
      paragraph = inner === null && innerFence === null && serverParagraphText(first);
    } else if (start !== null) {
      html[i] = 1;
      // A block of types 1 to 5 can end on its first line.
      open = start !== "blank" && start(line) ? null : start;
      openItem = column;
      paragraph = false;
    } else if (opened !== null) {
      fence = opened[1];
      openItem = column;
      paragraph = false;
    } else {
      // A line 4 columns past its container is indented code, which a paragraph would take as text.
      paragraph = lazy || (!/^(?: {4}| {0,3}\t)/.test(rest) && serverParagraphText(rest));
    }
  }
  if (!body.includes("\r")) return html;
  // marked also ends a line at a lone `\r`, so one server line can hold several of marked's.
  const out: number[] = [];
  lines.forEach((l, i) => {
    for (let n = l.split("\r").length; n > 0; n--) out.push(html[i]);
  });
  return Uint8Array.from(out);
}

// The character references marked's renderer keeps for the browser to decode.
const CHARACTER_REFERENCE = /&(?:#\d{1,7}|#[Xx][a-fA-F0-9]{1,6}|\w+);/g;

// A C1 control that no character reference shows, because the browser remaps that number:
// https://html.spec.whatwg.org/multipage/parsing.html#numeric-character-reference-end-state
const MARKER = "\u0080";
const MARKED = new RegExp(`${CHARACTER_REFERENCE.source}|${MARKER}`, "g");
const MARKED_OR_ESCAPED = new RegExp(`${CHARACTER_REFERENCE.source}|[${MARKER}<&]`, "g");
const MARKER_INDEX = new RegExp(`${MARKER}(\\d+)${MARKER}`, "g");

/**
 * The text the DOM shows for a text token, and the offset in that text of each
 * character that a character reference shows.
 *
 * The token decodes in one pass, so raw HTML in it parses in context, with a
 * marker in place of each reference. A reference that an unclosed tag swallows
 * vanishes with its marker. `references` caches the text each reference shows.
 */
function shownText(token: Tokens.Text, references: Map<string, string>): { text: string; decoded: number[] } {
  // The marker character itself is marked too, so every marker in the decoded text is one of these.
  const marked: string[] = [];
  // marked's renderer escapes text the same way, except for quotes and `>`.
  const html = token.text.replace(token.escaped ? MARKED : MARKED_OR_ESCAPED, (s) => {
    if (s === "<") return "&lt;";
    if (s === "&") return "&amp;";
    marked.push(s);
    return `${MARKER}${marked.length - 1}${MARKER}`;
  });
  const shown = decodeHtml(html);
  if (marked.length === 0) return { text: shown, decoded: [] };
  // A complete reference decodes the same in any context, so the new ones share one parse.
  const fresh = [...new Set(marked)].filter((s) => s !== MARKER && !references.has(s));
  if (fresh.length > 0) decodeHtml(fresh.join(MARKER)).split(MARKER).forEach((v, i) => references.set(fresh[i], v));

  let text = "";
  const decoded: number[] = [];
  let last = 0;
  for (const m of shown.matchAll(MARKER_INDEX)) {
    text += shown.slice(last, m.index);
    last = m.index + m[0].length;
    const s = marked[Number(m[1])];
    if (s === MARKER) {
      text += s;
      continue;
    }
    const value = references.get(s) ?? "";
    for (let i = 0; i < value.length; i++) decoded.push(text.length + i);
    text += value;
  }
  return { text: text + shown.slice(last), decoded };
}

function decodeHtml(html: string): string {
  if (!html.includes("&") && !html.includes("<")) return html;
  return toTemplate(html).content.textContent ?? "";
}

/**
 * Returns the candidates in `text` with the mention the server finds at each.
 * `breaks` holds, in ascending order, the offset of each character a backslash
 * escaped and of each character in `decoded`, the ones a character reference
 * shows.
 *
 * The server reads the source, where a backslash stands before an escaped
 * character and a decoded character is still its reference. So a candidate that
 * spans a break holds no mention, except that an escaped character may start one,
 * and a hash id ends before a break. Before a `#`, a decoded character reads as
 * the `;` that ends its reference.
 */
function segmentMentions(text: string, breaks: number[], decoded: Set<number>): (Candidate & { mention: string | null })[] {
  // Candidates come in ascending order, so breaks[b] is the first break after each.
  let b = 0;
  return findCandidates(text).map(({ key, index }) => {
    if (decoded.has(index)) return { key, index, mention: null };
    while (b < breaks.length && breaks[b] <= index) b++;
    const nextBreak = breaks[b] ?? text.length;
    if (key.startsWith("[[")) {
      return { key, index, mention: nextBreak >= index + key.length ? key.slice(2, -2) : null };
    }
    const escaped = b > 0 && breaks[b - 1] === index;
    const wordBefore = index > 0 && !decoded.has(index - 1) && /[A-Za-z0-9_]/.test(text[index - 1]);
    if (wordBefore && !escaped) return { key, index, mention: null };
    return { key, index, mention: LEADING_ID.exec(text.slice(index + 1, nextBreak))?.[0] ?? null };
  });
}

// The most edits diffPairs searches for. Its trace holds about MAX_EDITS² numbers.
const MAX_EDITS = 1000;

/**
 * Returns the index pairs of equal keys that a shortest edit script from `a` to
 * `b` keeps, in order, or null when every such script takes more than
 * MAX_EDITS edits. It is Myers' O((N+M)D) diff.
 */
function diffPairs(a: string[], b: string[]): [number, number][] | null {
  const n = a.length;
  const m = b.length;
  const max = Math.min(n + m, MAX_EDITS);
  // v[k + max + 1] is the furthest x reached on diagonal k = x - y.
  const off = max + 1;
  const v = new Int32Array(2 * max + 3);
  const trace: Int32Array[] = [];
  for (let d = 0; d <= max; d++) {
    trace.push(v.slice(off - d - 1, off + d + 2));
    for (let k = -d; k <= d; k += 2) {
      let x = k === -d || (k !== d && v[off + k - 1] < v[off + k + 1]) ? v[off + k + 1] : v[off + k - 1] + 1;
      let y = x - k;
      while (x < n && y < m && a[x] === b[y]) {
        x++;
        y++;
      }
      v[off + k] = x;
      if (x >= n && y >= m) return backtrack(a, b, trace, d, n, m);
    }
  }
  return null;
}

/** Walks the trace `diffPairs` recorded back from (n, m) and returns the pairs it passes. */
function backtrack(a: string[], b: string[], trace: Int32Array[], dEnd: number, n: number, m: number): [number, number][] {
  const pairs: [number, number][] = [];
  let x = n;
  let y = m;
  for (let d = dEnd; d > 0; d--) {
    // trace[d] holds v before step d, for diagonals -d - 1 through d + 1.
    const prev = (k: number): number => trace[d][k + d + 1];
    const k = x - y;
    const down = k === -d || (k !== d && prev(k - 1) < prev(k + 1));
    const prevK = down ? k + 1 : k - 1;
    const prevX = prev(prevK);
    const prevY = prevX - prevK;
    const startX = down ? prevX : prevX + 1;
    while (x > startX) pairs.push([--x, --y]);
    x = prevX;
    y = prevY;
  }
  while (x > 0 && y > 0 && a[x - 1] === b[y - 1]) pairs.push([--x, --y]);
  return pairs.reverse();
}

/**
 * Returns the pairs `diffPairs(a, b)` finds, each with whether a diff of the
 * reversed lists pairs the same two indexes, or null when `diffPairs` finds none.
 *
 * Where a key repeats, more than one shortest edit script can pair it, and the
 * two directions can pick different ones. A pair is settled when both directions
 * find it.
 */
function settledPairs(a: string[], b: string[]): { pairs: [number, number][]; settled: boolean[] } | null {
  const pairs = diffPairs(a, b);
  if (pairs === null) return null;
  // Both directions take the same number of edits, so the reversed diff stays under MAX_EDITS too.
  const reversed = diffPairs([...a].reverse(), [...b].reverse()) ?? [];
  const partner = new Int32Array(a.length).fill(-1);
  for (const [i, k] of reversed) partner[a.length - 1 - i] = b.length - 1 - k;
  return { pairs, settled: pairs.map(([i, k]) => partner[i] === k) };
}

/**
 * Returns the index pairs of `found` and `expected` candidates with equal keys,
 * in order, or none when the two lists differ in more than MAX_EDITS places.
 *
 * Text and raw candidates pair first. Each `skipped` candidate can then pair
 * only with a found candidate left between the same two pairs. A pair that the
 * keys do not settle, as `settledPairs` decides, is left out, though it still
 * bounds the skipped candidates.
 */
function alignCandidates(found: string[], expected: ExpectedCandidate[]): [number, number][] {
  const firm: number[] = [];
  const skipped: number[] = [];
  expected.forEach((c, j) => (c.source === "skipped" ? skipped : firm).push(j));
  const firstPass = settledPairs(
    found,
    firm.map((j) => expected[j].key),
  );
  if (firstPass === null) return [];
  const pairs: [number, number][] = firstPass.pairs.map(([i, k]) => [i, firm[k]]);
  if (skipped.length === 0) return pairs.filter((_, p) => firstPass.settled[p]);

  const out: [number, number][] = [];
  let s = 0;
  let fromFound = 0;
  for (let p = 0; p <= pairs.length; p++) {
    const pair: [number, number] = p < pairs.length ? pairs[p] : [found.length, expected.length];
    const gapSkipped: number[] = [];
    while (s < skipped.length && skipped[s] < pair[1]) gapSkipped.push(skipped[s++]);
    if (gapSkipped.length > 0 && pair[0] > fromFound) {
      const gapFound = found.slice(fromFound, pair[0]);
      const gap = settledPairs(
        gapFound,
        gapSkipped.map((j) => expected[j].key),
      );
      gap?.pairs.forEach(([i, k], g) => {
        if (gap.settled[g]) out.push([fromFound + i, gapSkipped[k]]);
      });
    }
    if (p < pairs.length && firstPass.settled[p]) out.push(pair);
    fromFound = pair[0] + 1;
  }
  return out;
}

/** One mention to link: `node`'s text from `index` on is `text`. */
interface MentionLink {
  index: number;
  text: string;
  fullId: string;
}

/**
 * Rewrites the mentions in sanitized `html` into mention links, and returns
 * `html` itself when it links none. A bracketed link shows the bare id.
 *
 * A candidate in the DOM's text links only when `alignCandidates` pairs it with
 * an expected mention. The walk reads the text `visibleTextNodes` returns.
 */
function linkMentions(html: string, expected: ExpectedCandidate[], resolve: MentionResolver): string {
  const tpl = toTemplate(html);
  const found: (Candidate & { node: Text })[] = [];
  for (const node of visibleTextNodes(tpl.content)) {
    for (const c of findCandidates(node.nodeValue ?? "")) found.push({ ...c, node });
  }

  const links = new Map<Text, MentionLink[]>();
  const pairs = alignCandidates(
    found.map((c) => c.key),
    expected,
  );
  for (const [i, j] of pairs) {
    const token = expected[j].mention;
    if (token === null) continue;
    let fullId: string | null;
    try {
      fullId = resolve(token);
    } catch {
      // A throwing resolver leaves the token unresolved.
      fullId = null;
    }
    if (!fullId) continue;
    const { node, index, key } = found[i];
    const text = key.startsWith("[[") ? key : `#${token}`;
    const list = links.get(node) ?? [];
    list.push({ index, text, fullId });
    links.set(node, list);
  }
  if (links.size === 0) return html;

  for (const [node, list] of links) {
    const value = node.nodeValue ?? "";
    const frag = document.createDocumentFragment();
    let last = 0;
    for (const { index, text, fullId } of list) {
      if (index > last) frag.appendChild(document.createTextNode(value.slice(last, index)));
      const a = document.createElement("a");
      a.setAttribute("data-nib-id", fullId);
      a.setAttribute("href", `#${fullId}`);
      a.setAttribute("class", "mention-link text-link");
      a.textContent = text.startsWith("[[") ? text.slice(2, -2) : text;
      frag.appendChild(a);
      last = index + text.length;
    }
    if (last < value.length) frag.appendChild(document.createTextNode(value.slice(last)));
    node.parentNode?.replaceChild(frag, node);
  }
  return tpl.innerHTML;
}

/**
 * Render a markdown body to sanitized HTML.
 *
 * Task items render as ENABLED checkboxes with `data-task-ordinal` and persist
 * nothing on their own. A caller must wire a handler that calls `toggleTaskLine`
 * (as ActiveNibView.handleProseClick does) or disable them.
 *
 * @param resolveMention Called with each mention token; returns the full nib ID,
 *   or null to leave it as text. Return only a trusted nib ID: it lands in
 *   `href` and `data-nib-id`. A throw counts as unresolved. Mentions inside
 *   code are never rewritten.
 */
export function renderMarkdown(body: string, resolveMention?: MentionResolver): string {
  if (!body) return "";
  const html = renderSanitized(body);
  if (!resolveMention || typeof document === "undefined") return html;
  return linkMentions(html, expectedMentions(body), resolveMention);
}

export const TAG_REGEX = /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/;
