import DOMPurify from "dompurify";
import { Marked } from "marked";

export type MentionResolver = (token: string) => string | null;

// Shared Marked instance — no per-call allocation. We do not register a
// mention extension on this instance: all mention rewriting happens in the
// post-sanitize DOM walk below, which is the single authoritative path.
const sharedMarked = new Marked();

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
  const html = sharedMarked.parse(body, { async: false }) as string;
  // DOMPurify allows data-* attributes by default; specifying explicitly for
  // clarity since `data-nib-id` is load-bearing for mention click delegation.
  const safe = DOMPurify.sanitize(html, { ADD_ATTR: ["data-nib-id"] });
  if (!resolveMention) return safe;
  return postProcessMentionsInDom(safe, resolveMention);
}

export const TAG_REGEX = /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/;
