import { describe, it, expect, vi } from "vitest";
import DOMPurify from "dompurify";
import {
  renderMarkdown,
  toggleTaskLine,
  taskSourceLines,
  TAG_REGEX,
} from "./markdown";

/** Parse rendered HTML and return the checked-state of each task checkbox, in
 *  ascending `data-task-ordinal` order. */
function renderedCheckboxStates(html: string): boolean[] {
  const tpl = document.createElement("template");
  tpl.innerHTML = html;
  const boxes = Array.from(
    tpl.content.querySelectorAll<HTMLInputElement>("input[data-task-ordinal]"),
  );
  boxes.sort(
    (a, b) =>
      Number(a.getAttribute("data-task-ordinal")) - Number(b.getAttribute("data-task-ordinal")),
  );
  return boxes.map((b) => b.hasAttribute("checked"));
}

/** The `data-task-ordinal` values a body renders, in document order. */
function renderedOrdinals(html: string): number[] {
  const tpl = document.createElement("template");
  tpl.innerHTML = html;
  return Array.from(
    tpl.content.querySelectorAll<HTMLInputElement>("input[data-task-ordinal]"),
  ).map((b) => Number(b.getAttribute("data-task-ordinal")));
}

describe("renderMarkdown", () => {
  it("renders a heading as h1", () => {
    const html = renderMarkdown("# Hello");
    expect(html).toContain("<h1");
    expect(html).toContain("Hello");
  });

  it("strips XSS script tags", () => {
    const html = renderMarkdown('<script>alert("xss")</script>Safe text');
    expect(html).not.toContain("<script>");
    expect(html).toContain("Safe text");
  });

  it("returns empty string for empty input", () => {
    expect(renderMarkdown("")).toBe("");
  });

  it("renders code blocks as pre>code", () => {
    const html = renderMarkdown("```\nconst x = 1;\n```");
    expect(html).toContain("<pre>");
    expect(html).toContain("<code>");
    expect(html).toContain("const x = 1;");
  });
});

describe("renderMarkdown mention rewriting", () => {
  it("rewrites a resolved short-form #<id> into an anchor with data-nib-id", () => {
    const html = renderMarkdown("see #gx0f", (t) => (t === "gx0f" ? "nibs-gx0f" : null));
    expect(html).toContain('data-nib-id="nibs-gx0f"');
    expect(html).toContain(">#gx0f</a>");
  });

  it("rewrites a resolved full-form #nibs-<id> into an anchor with the same full id", () => {
    const html = renderMarkdown("see #nibs-gx0f", (t) => (t === "nibs-gx0f" ? "nibs-gx0f" : null));
    expect(html).toContain('data-nib-id="nibs-gx0f"');
    expect(html).toContain(">#nibs-gx0f</a>");
  });

  it("leaves an unresolved token as plain text (no anchor)", () => {
    const html = renderMarkdown("see #unknown", () => null);
    expect(html).not.toContain("data-nib-id");
    expect(html).not.toContain("<a ");
    expect(html).toContain("#unknown");
  });

  it("omitted resolver disables rewriting (preview behavior)", () => {
    const html = renderMarkdown("see #gx0f");
    expect(html).not.toContain("data-nib-id");
    expect(html).not.toContain("<a ");
    expect(html).toContain("#gx0f");
  });

  it("does not rewrite #<id> inside a fenced code block", () => {
    const html = renderMarkdown("```\nsee #gx0f\n```", () => "nibs-gx0f");
    expect(html).not.toContain("data-nib-id");
    expect(html).toContain("#gx0f");
    expect(html).toContain("<pre>");
  });

  it("does not rewrite #<id> inside an inline code span", () => {
    const html = renderMarkdown("use `#gx0f` here", () => "nibs-gx0f");
    expect(html).not.toContain("data-nib-id");
    expect(html).toContain("#gx0f");
    expect(html).toContain("<code>");
  });

  it("does not mangle an existing markdown link whose href starts with #", () => {
    const html = renderMarkdown("[see](#elsewhere)", () => "nibs-anything");
    // The existing anchor should preserve its href, and the linked text is "see"
    expect(html).toContain('href="#elsewhere"');
    expect(html).toContain(">see</a>");
    expect(html).not.toContain("data-nib-id");
  });

  it("does not rewrite #<id> when adjacent to a word char (e.g. email#gx0f)", () => {
    const html = renderMarkdown("email#gx0f", () => "nibs-gx0f");
    expect(html).not.toContain("data-nib-id");
    expect(html).toContain("email#gx0f");
  });

  it("does not rewrite #<id> after an underscore or a digit", () => {
    const html = renderMarkdown("a_#gx0f 9#gx0f", () => "nibs-gx0f");
    expect(html).not.toContain("data-nib-id");
    expect(html).toContain("a_#gx0f 9#gx0f");
  });

  it("strips XSS scripts but still rewrites a valid mention in the same body", () => {
    const html = renderMarkdown(
      '<script>alert(1)</script>\n\n#gx0f',
      (t) => (t === "gx0f" ? "nibs-gx0f" : null),
    );
    expect(html).not.toContain("<script");
    expect(html).toContain('data-nib-id="nibs-gx0f"');
    expect(html).toContain(">#gx0f</a>");
  });

  it("rewrites multiple resolved mentions with distinct data-nib-id values", () => {
    const html = renderMarkdown(
      "see #aaaa and #bbbb",
      (t) => (t === "aaaa" ? "nibs-aaaa" : t === "bbbb" ? "nibs-bbbb" : null),
    );
    expect(html).toContain('data-nib-id="nibs-aaaa"');
    expect(html).toContain('data-nib-id="nibs-bbbb"');
    expect(html).toContain(">#aaaa</a>");
    expect(html).toContain(">#bbbb</a>");
  });

  it("generated anchor carries the text-link class", () => {
    const html = renderMarkdown("see #gx0f", () => "nibs-gx0f");
    expect(html).toContain("text-link");
  });

  it("escapes malicious resolver return values (defense in depth)", () => {
    const evil = '" onclick=alert(1) "';
    const tpl = document.createElement("template");
    tpl.innerHTML = renderMarkdown("see #x", () => evil);
    const a = tpl.content.querySelector("a.mention-link");
    expect(a?.getAttribute("data-nib-id")).toBe(evil);
    expect(a?.getAttributeNames().sort()).toEqual(["class", "data-nib-id", "href"]);
  });

  it("round-trips non-mention content without corruption", () => {
    const input = "See &amp; more <em>text</em> with <br/>";
    const html = renderMarkdown(input, () => null);
    // Entity preserved / not corrupted:
    expect(html).toMatch(/&amp;|&#38;/);
    expect(html).toContain("<em>");
  });

  it("treats a throwing resolver as unresolved", () => {
    const html = renderMarkdown("see #gx0f", () => {
      throw new Error("boom");
    });
    expect(html).toContain("#gx0f");
    expect(html).not.toContain("data-nib-id");
  });
});

describe("renderMarkdown bracketed [[<id>]] mentions", () => {
  /** The mention anchors in rendered HTML, as [data-nib-id, text] pairs. */
  function mentionLinks(html: string): [string | null, string | null][] {
    const tpl = document.createElement("template");
    tpl.innerHTML = html;
    return Array.from(tpl.content.querySelectorAll("a.mention-link")).map((a) => [
      a.getAttribute("data-nib-id"),
      a.textContent,
    ]);
  }

  it("renders a resolved [[<id>]] as a mention link whose text is the bare id", () => {
    const html = renderMarkdown("see [[nibs-gx0f]] here", (t) =>
      t === "nibs-gx0f" ? "nibs-gx0f" : null,
    );
    expect(mentionLinks(html)).toEqual([["nibs-gx0f", "nibs-gx0f"]]);
    expect(html).not.toContain("[[");
    expect(html).not.toContain("]]");
    expect(html).toContain("see ");
    expect(html).toContain(" here");
  });

  it("links a one-character bracketed id", () => {
    const html = renderMarkdown("see [[x]]", (t) => (t === "x" ? "nibs-x" : null));
    expect(mentionLinks(html)).toEqual([["nibs-x", "x"]]);
  });

  it("links brackets after a backslash, as the server counts them", () => {
    const html = renderMarkdown("\\[[gx0f]]", () => "nibs-gx0f");
    expect(mentionLinks(html)).toEqual([["nibs-gx0f", "gx0f"]]);
  });

  it("resolves a bracketed short id like a hash one", () => {
    const html = renderMarkdown("[[gx0f]]", (t) => (t === "gx0f" ? "nibs-gx0f" : null));
    expect(mentionLinks(html)).toEqual([["nibs-gx0f", "gx0f"]]);
  });

  it("leaves an unresolved [[<id>]] as literal text", () => {
    const html = renderMarkdown("see [[unknown]]", () => null);
    expect(mentionLinks(html)).toEqual([]);
    expect(html).toContain("[[unknown]]");
  });

  it("does not rewrite [[<id>]] inside an inline code span", () => {
    const html = renderMarkdown("use `[[gx0f]]` here", () => "nibs-gx0f");
    expect(mentionLinks(html)).toEqual([]);
    expect(html).toContain("<code>[[gx0f]]</code>");
  });

  it("does not rewrite [[<id>]] inside a fenced code block", () => {
    const html = renderMarkdown("```\n[[gx0f]]\n```", () => "nibs-gx0f");
    expect(mentionLinks(html)).toEqual([]);
    expect(html).toContain("[[gx0f]]");
  });

  it("does not treat inner spaces or an alias as a mention", () => {
    const html = renderMarkdown("[[ gx0f ]] and [[gx0f|text]]", () => "nibs-gx0f");
    expect(mentionLinks(html)).toEqual([]);
  });

  it("links word-adjacent brackets, since the brackets delimit", () => {
    const html = renderMarkdown("foo[[gx0f]]bar", () => "nibs-gx0f");
    expect(mentionLinks(html)).toEqual([["nibs-gx0f", "gx0f"]]);
    expect(html).toContain("foo<a");
    expect(html).toContain("</a>bar");
  });

  it("links both forms in one text node, in order", () => {
    const html = renderMarkdown(
      "Bracketed [[aaaa]] versus hash #bbbb, then [[cccc]]#dddd.",
      (t) => `nibs-${t}`,
    );
    expect(mentionLinks(html)).toEqual([
      ["nibs-aaaa", "aaaa"],
      ["nibs-bbbb", "#bbbb"],
      ["nibs-cccc", "cccc"],
      ["nibs-dddd", "#dddd"],
    ]);
    expect(html).toContain(" versus hash ");
    expect(html).toContain(", then ");
  });

  it("links a hash inside brackets once, as the hash form", () => {
    const html = renderMarkdown("[[#gx0f]]", () => "nibs-gx0f");
    expect(mentionLinks(html)).toEqual([["nibs-gx0f", "#gx0f"]]);
    expect(html).toContain("[[<a");
    expect(html).toContain("</a>]]");
  });
});

// The same cases, under the same names, are in TestExtractMentionTokensWebParity
// (internal/nib/references_test.go), so both sides must give the same answer.
describe("renderMarkdown mention parity with the server scanner", () => {
  /** The distinct tokens a body links, in order, when every token resolves. */
  function linkedTokens(body: string): string[] {
    const tpl = document.createElement("template");
    tpl.innerHTML = renderMarkdown(body, (t) => `nibs-${t}`);
    const ids = Array.from(tpl.content.querySelectorAll("a.mention-link")).map((a) =>
      (a.getAttribute("data-nib-id") ?? "").slice("nibs-".length),
    );
    return [...new Set(ids)];
  }

  /** A list of n hash mentions between two comments that hold other mentions. */
  function commentedList(n: number): string {
    return `<!-- #zz -->\n\n${Array.from({ length: n }, (_, i) => `- #m${i}\n`).join("")}\n<!-- #yy -->`;
  }
  const listTokens = (n: number): string[] => Array.from({ length: n }, (_, i) => `m${i}`);
  /** A raw anchor, a markdown link, then n hash mentions in one paragraph. */
  const anchorThenMentions = (n: number): string =>
    `x <a name=t> see [d](u)${Array.from({ length: n }, (_, i) => ` #m${i}`).join("")}`;
  const items = (prefix: string, n: number): string =>
    Array.from({ length: n }, (_, i) => `- #${prefix}${i}\n`).join("");
  /** Brackets that only the DOM joins, n hash mentions, the bracketed id, then n more. */
  const domOnlyThenLists = (n: number): string =>
    `[<foo></foo>[x]]\n\n${items("m", n)}\nsee [[x]]\n\n${items("n", n)}\n[<foo></foo>[y]]`;
  const domOnlyThenListsTokens = (n: number): string[] => [
    ...listTokens(n),
    "x",
    ...Array.from({ length: n }, (_, i) => `n${i}`),
  ];

  const parityCases: [string, string, string[]][] = [
    ["escaped brackets: a backslash before the second opening bracket", "[\\[x]]", []],
    ["escaped brackets: a backslash before the second closing bracket", "[[x]\\]", []],
    ["escaped brackets: a backslash before both opening brackets", "\\[\\[x]]", []],
    ["escaped brackets: a backslash before the closing brackets", "[[x\\]]", []],
    ["escaped brackets: a backslash before the first opening bracket", "\\[[x]]", ["x"]],
    ["escaped brackets: an escape inside the id", "[[a\\-b]]", []],
    ["escapes: an escape ends a hash id", "#a\\-b", ["a"]],
    ["escapes: an escaped hash after a word character", "a\\#x", ["x"]],
    ["character references: decimal brackets", "&#91;&#91;x]]", []],
    ["character references: a decimal hash", "&#35;x", []],
    ["character references: a hex hash", "&#x23;x", []],
    ["character references: an uppercase hex hash", "&#X23;x", []],
    ["character references: a named hash", "&num;x", []],
    ["character references: named brackets", "&lsqb;&lsqb;x]]", []],
    ["character references: a decoded closing bracket", "[[x&#93;]", []],
    ["character references: a decoded id character in brackets", "[[a&#45;b]]", []],
    ["character references: a decimal hash without a semicolon", "&#35", ["35"]],
    ["character references: a letter in a decimal reference", "&#9a;", ["9a"]],
    ["character references: too many decimal digits", "&#00000035;", ["00000035"]],
    ["character references: too many hex digits", "&#x0000023;", ["x0000023"]],
    ["character references: an encoded ampersand before a hash", "&amp;#x", ["x"]],
    ["character references: an encoded ampersand before a decimal hash", "&amp;#35;", ["35"]],
    ["character references: an escaped ampersand", "\\&#35;x", ["35"]],
    ["character references: an escaped backslash before the ampersand", "\\\\&#35;x", []],
    ["character references: a decoded hash before a mention", "&#35;#x", ["x"]],
    ["character references: a decoded hash after a mention", "#x&#35;y", ["x"]],
    ["character references: a decoded id character ends a hash id", "#a&#98;c", ["a"]],
    ["character references: a decoded letter before a hash", "&#97;#x", ["x"]],
    ["character references: two decoded letters before a hash", "&fjlig;#x", ["x"]],
    ["character references: an unclosed tag before a reference in raw-block text", "<kbd><span &#35;y</kbd> #y", ["y"]],
    ["character references: an unclosed tag after a reference in raw-block text", "<kbd>&#35;x <span #y</kbd> #y", ["y"]],
    ["character references: a reference in an attribute value in raw-block text", "<kbd><span &a=\"&#35;y\">t</kbd> #y", ["y"]],
    ["character references: a C1 control beside a reference", "\u0080&#35;x #y", ["y"]],
    ["character references: C1 controls beside references in raw-block text", "<kbd>\u0080\u0082&#35;x</kbd> #y", ["y"]],
    ["character references: a marker-shaped run of C1 controls", "#y\u00800\u0080 &#97;", ["y"]],
    ["character references: a space and a letter after a hash id", "#a&#32;&#98;c", ["a"]],
    ["html blocks: a details block without blank lines", "<details>\n[[old-x]] #old-y\n</details>", []],
    [
      "html blocks: a details block with blank lines",
      "<details>\n\n[[old-x]] #old-y\n\n</details>",
      ["old-x", "old-y"],
    ],
    ["html blocks: a div block", "<div>[[x]] #y</div>", []],
    ["html blocks: text between inline tags", "a <span>[[x]] #y</span> b", ["x", "y"]],
    ["html blocks: brackets split by a tag the sanitizer removes", "[<foo></foo>[x]]", []],
    ["html blocks: a block that leaves a link open", '<a href="u">\n\n</a> [[y]]', ["y"]],
    [
      "html blocks: a block that ends inside a start tag",
      "intro #a\n\n<div>\n<details\n\nhello [[x]] world\n\nmore #y text",
      ["a", "x", "y"],
    ],
    [
      "html blocks: a list item block that ends inside a start tag",
      "- <div>\n  <details\n\n  [[x]] item\n- after #y",
      ["x", "y"],
    ],
    ["html blocks: a body that ends inside a start tag", "[[a]]\n\n<div>\n<details", ["a"]],
    ["html blocks: a leading noscript block", "<noscript>\n\n[[x]] visible?\n\n</noscript>", ["x"]],
    ["html blocks: a leading style block", "<style>p{color:red}</style>\n\nlater #y", ["y"]],
    [
      "lost markers: an inline xmp in a div",
      "<div>\nfoo <xmp>\n\nx </xmp> #y\n\n<div>z</div>\n\nend #w",
      ["y", "w"],
    ],
    ["malformed html: a paragraph fostered out of a table", "<table>\n\n#y\n\n<tr><td>#secret</td></tr>", ["y"]],
    [
      "malformed html: a table cell left open across blocks",
      "intro\n\n<table><tr><td> #a\n\n</td></tr>\n\nx </table> #z\n\n<div>d</div>",
      ["z"],
    ],
    ["malformed html: an inline iframe", "a <iframe>\n\n</iframe>\n#z\n\nafter #w", ["w"]],
    ["malformed html: an inline noembed", "x <noembed>\n\n<div>\n</noembed>\n#c\n</div>\n\nafter", []],
    ["raw html text: a comment before a mention of the same id", "<!-- see #x -->\n\nSee #x", ["x"]],
    ["raw html text: a comment after a mention of the same id", "<!-- #q -->\n\n#x\n\n<!-- see #x -->", ["x"]],
    ["raw html text: a comment with no visible twin", "<!-- #x -->\n\nsee #y", ["y"]],
    ["raw html text: a comment before two mentions of the same id", "<!-- #x -->\n\n#x and #x", ["x"]],
    ["raw html text: a bracketed id in a comment", "<!-- [[x]] -->\nText [[x]]", ["x"]],
    ["raw html text: a link target before a mention of the same id", '<a href="#x">jump</a> and #x', ["x"]],
    ["raw html text: image alt text before a mention of the same id", '<img alt="#x" src="a.png"> then #x', ["x"]],
    ["raw html text: a style block before a mention of the same id", "<style>#x{color:red}</style>\n\n#x", ["x"]],
    [
      "raw html text: inline code before an html block with the same id",
      "x <code>#a</code>\n\n<div>#a</div>\n\nsee #a",
      ["a"],
    ],
    [
      "raw html text: an inline textarea before an html block with the same id",
      "x <textarea>#a</textarea>\n\n<div>#a</div>\n\n#a",
      ["a"],
    ],
    [
      "raw html text: an inline noembed closed inside an html block",
      "x <noembed>\n\n<div>\n</noembed>\n#c\n</div>\n\nsee #c",
      ["c"],
    ],
    ["raw html text: a noscript block before a mention of the same id", "intro\n\n<noscript>\n#x\n</noscript>\n\nsee #x", ["x"]],
    [
      "raw html text: a noscript block after a mention of the same id",
      "[<foo></foo>[z]]\n\n#x\n\n<noscript>\n#x\n</noscript>",
      ["x"],
    ],
    ["raw html text: comments around a long list", commentedList(1100), listTokens(1100)],
    ["implicit closes: a raw anchor before a markdown link", "<a name=\"top\">\n\nIntro\n\nSee [docs](u) and #x\n\nMore #w", ["x", "w"]],
    ["implicit closes: a nested anchor", "x <a>y <a>z</a> and #x", ["x"]],
    ["implicit closes: inline code in a table cell", "| a | b |\n|---|---|\n| <code>x | #y |\n\n# Next\n\nsee #z and [[q]]", ["y", "z", "q"]],
    ["implicit closes: a pre in a heading", "# Title <pre>\n\nsee #y", ["y"]],
    ["implicit closes: a pre in a list item", "- x <pre>\n- #y\n\nafter #z", ["y", "z"]],
    ["implicit closes: code in a select", "x <select><code>y</select> and #q\n\nlater #r", ["q", "r"]],
    ["implicit closes: an anchor tag in a quoted attribute", "x <span title=\"p>q<a>\">hi</span> and #x", ["x"]],
    ["implicit closes: a code tag in a quoted attribute", "x <i title=\"> <code>\">t</i> #x", ["x"]],
    ["alignment: brackets only the DOM joins, then a long list", domOnlyThenLists(600), domOnlyThenListsTokens(600)],
    ["implicit closes: a raw anchor before a markdown link in one paragraph", "x <a name=t> see [d](u) #x", ["x"]],
    ["implicit closes: code left open in a table cell", "<table><tr><td><code>#a</td><td>#b</td></tr></table>\n\n#c", ["c"]],
    ["implicit closes: a pre left open in a list item", "- <pre>a\n- #b\n\nafter #c", ["b", "c"]],
    ["implicit closes: a quoted > before a code tag", "x <b title=\"1>2 <code>\">#y</b> #y", ["y"]],
    ["alignment: an id many times after an html block that holds it", "<div>#x</div>\n\n#x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x #x", ["x"]],
    ["alignment: html blocks and paragraphs that alternate one id", "<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x\n\n<div>#x</div>\n\n#x", ["x"]],
    ["unclosed elements: an inline link left open in a table cell", "| <a name=t> |\n|---|\n| c |\n\nafter #z\n\n<div>#z</div>", ["z"]],
    ["unclosed elements: an html block that closes an inline link", "x <a name=t>\n\n<div></a></div>\n\n#z\n\n<div>#z</div>", ["z"]],
    ["unclosed elements: a second inline link closes the first", "x <a name=t> y <a href=u>z</a>\n\n#q\n\n<div>#q</div>", ["q"]],
    ["tables: a mention in a cell before an html block with the same id", "| a |\n|---|\n| #y |\n\n<div>#y</div>", ["y"]],
    ["foreign content: a self-closed style in an svg", "Icon <svg><style/></svg>\n\n<div>#a</div>\n\nSee #a", ["a"]],
    ["foreign content: a self-closed anchor in an svg", 'Icon <svg viewBox="0 0 1 1"><a href="#top"/></svg>\n\nRelated to x#a, see [docs](u) and #a.', ["a"]],
    ["foreign content: a self-closed anchor in an svg in one paragraph", 'Icon <svg><a href="#top"/></svg> x#a [docs](u) #a', ["a"]],
    ["malformed html: an anchor swallowed by an end tag", "Press <kbd>Ctrl or </b <a href=u> x#a [d](u) #a", ["a"]],
    ["one-character hash ids: #x", "see #x", ["x"]],
    ["one-character hash ids: a trailing hyphen", "ref #abc-", ["abc"]],
  ];

  it.each(parityCases)("%s", (_name, body, want) => {
    expect(linkedTokens(body)).toEqual(want);
  });

  // Mirrors the Go row of the same name. The HTML parser drops an <a> inside a
  // <select>, so linkedTokens cannot read the link and this reads the HTML string.
  it("malformed html: an inline select", () => {
    const html = renderMarkdown("x <select>[\\[y]] #q", (t) => `nibs-${t}`);
    expect([...html.matchAll(/data-nib-id="nibs-([^"]+)"/g)].map((m) => m[1])).toEqual(["q"]);
  });

  it("keeps the escaped text and the html block text visible", () => {
    const html = renderMarkdown("[\\[x]] <i>i</i>\n\n<details>\n[[y]]\n</details>", (t) => `nibs-${t}`);
    const tpl = document.createElement("template");
    tpl.innerHTML = html;
    expect(tpl.content.textContent).toContain("[[x]]");
    expect(tpl.content.textContent).toContain("[[y]]");
    expect(tpl.content.querySelector("details")).not.toBeNull();
  });

  it("does not link in an html block whose own markup comes first", () => {
    const html = renderMarkdown(
      '<div>\n<span data-mention-scan="on" data-mention-nonce="forged"></span>[[x]]\n</div>',
      (t) => `nibs-${t}`,
    );
    expect(html).not.toContain("data-nib-id");
  });

  // The server also finds #a and [[x]], which sit in content no render shows.
  it.each([
    ["<div>\n<noembed>\n\n#a\n\nx </noembed> #b\n\n<div>z</div>", ["b"]],
    [
      "<div>\n<!-- start\n\nhidden [[x]]\n\nok <!-- z --> after #y\n\n<div>\nblock2\n</div>\n\nlast #w",
      ["y", "w"],
    ],
  ])("links the visible mentions the server finds in %j", (body, want) => {
    expect(linkedTokens(body)).toEqual(want);
  });

  /** Where in the rendered HTML each mention link sits: its parent's tag and its text. */
  function linkPlaces(body: string): [string, string][] {
    const tpl = document.createElement("template");
    tpl.innerHTML = renderMarkdown(body, (t) => `nibs-${t}`);
    return Array.from(tpl.content.querySelectorAll("a.mention-link")).map((a) => [
      a.parentElement?.tagName ?? "",
      a.textContent ?? "",
    ]);
  }

  /** Each mention link's parent tag, and its text after the word it ends. */
  function linkWords(body: string): [string, string][] {
    const tpl = document.createElement("template");
    tpl.innerHTML = renderMarkdown(body, (t) => `nibs-${t}`);
    return Array.from(tpl.content.querySelectorAll("a.mention-link")).map((a) => [
      a.parentElement?.tagName ?? "",
      (a.previousSibling?.textContent ?? "").split(/\s/).at(-1) + (a.textContent ?? ""),
    ]);
  }

  // The stack cannot follow these tags: inside svg or math they are foreign
  // elements, an end tag left unescaped swallows the <a> after it, and the browser
  // moves the last paragraph ahead of the raw table.
  it.each([
    ["Icon <svg><style/></svg>\n\n<div>#a</div>\n\nSee #a", [["P", "#a"]]],
    ['Icon <svg viewBox="0 0 1 1"><a href="#top"/></svg>\n\nRelated to x#a, see [docs](u) and #a.', [["P", "#a"]]],
    ['Icon <svg><a href="#top"/></svg> x#a [docs](u) #a', [["P", "#a"]]],
    ["Icon <svg><title/></svg>\n\n<!-- todo --><b>#a</b>\n\nSee #a", [["P", "#a"]]],
    ["x <svg> <style>\n\n<div>#a</div>\n\n#a", [["P", "#a"]]],
    ["Icon <math><style/></math>\n\n<div>#a</div>\n\nSee #a", [["P", "#a"]]],
    ["Press <kbd>Ctrl or </b <a href=u> x#a [d](u) #a", [["KBD", "#a"]]],
    [
      "- #a <pre> #a <tr> #a\n  - </ifr &#35;a <A href=u>\n\n&#35;a x#b #b #a",
      [
        ["LI", "#a"],
        ["P", "#b"],
        ["P", "#a"],
      ],
    ],
    ["[d](u) <title> |\n - &#35;a </title> </script>\n<table>\n<tr><td>\\#a</td></tr>\n\n\\#a", []],
  ])("links only the mentions the server counts in %j", (body, want) => {
    expect(linkWords(body)).toEqual(want);
  });

  // The server counts only the last mention, which the browser hides. The visible
  // text with the same key is not a mention.
  it.each([
    ["hidden mention: more than three open code elements, then an anchor reopened in code", "x <code><code><code><code> y\n\n</code></code></code> see x#a\n\n</code> z <code><a name=t>y</code> #a"],
    ["hidden mention: more than three open code elements, then a code end tag in an html block", "<code><code><code><code>\n\n</code></code></code> x#a\n\n<code><code>\n\n<div></code></div>\n\n#a"],
    ["hidden mention: prose about code tags, then a code end tag in an object", "Wrap it in <code>, <code>, <code> and <code>.\n\nClose with </code></code></code> as in issue x#a.\n\nThen </code> <code> <object></code></object> see #a."],
    ["hidden mention: a raw-text end tag in link text, then an anchor reopened in code", "x <iframe> [</iframe>](u)\n\n<div>#a</div>\n\n</iframe>\n\nz <code><a name=t>y</code> #a"],
    ["hidden mention: a decoded hash after more than three code elements", "<code><code><code><code>\n\n</code></code></code> &#35;a </code> <code> <marquee></code></marquee> #a"],
    ["hidden mention: an anchor in an object, then an anchor reopened in code", "<object data=\"d.pdf\"><a href=\"d.pdf\">Download the PDF\n\n</object>\n\nThe build broke in x#a; see <code><a name=n>note</code> #a"],
    ["hidden mention: a decoded hash, then a hash after an ampersand", "<code><code><code><code>\n\n</code></code></code> &#35;a </code> <code> <marquee></code></marquee> &#a"],
  ])("links nothing where %s", (_name, body) => {
    expect(linkWords(body)).toEqual([]);
  });

  it.each([
    ["the mention after an html block that holds the same text", "<div>#x</div>\n\n#x", [["P", "#x"]]],
    ["the mention before an html block that holds the same text", "#x\n\n<div>#x</div>", [["P", "#x"]]],
    ["the mention after a comment that holds the same text", "<!-- see #x -->\n\nSee #x", [["P", "#x"]]],
    ["the mention after code in an html block", "<div><code>#x</code></div>\n\n#x", [["P", "#x"]]],
    ["the mention after a character reference in an html block", "<div>&#35;a</div>\n\n#a", [["P", "#a"]]],
    ["the mention after named references in an html block", "<div>&lbrack;&lbrack;a]]</div>\n\n[[a]]", [["P", "a"]]],
    ["the mention after a leading noscript block whose text the parser moves", "<noscript>\n#x\n</noscript>\n\n#x", [["P", "#x"]]],
    [
      "the mention after an end tag inside an inline textarea",
      "x <code><textarea></code>#a</textarea> #b</code>\n\n<div>#a</div>\n\nsee #a",
      [["P", "#a"]],
    ],
    ["nothing in an html block before a link label that leaves code open", "<div>#x</div>\n\n[<code>](u) #x", []],
    ["nothing in an html block before a template", "<div>#x</div>\n\nsee <template>#x</template> end", []],
    ["the mention between a comment and an html block that hold the same text", "<!-- #x -->\n\n#x\n\n<div>#x</div>", [["P", "#x"]]],
    [
      "the mention between a noscript block and an html block that hold the same text",
      "intro\n\n<noscript>\n#x\n</noscript>\n\n#x\n\n<div>#x</div>",
      [["P", "#x"]],
    ],
    ["the mention after an inline textarea and an html block", "x <textarea>#a</textarea>\n\n<div>#a</div>\n\n#a", [["P", "#a"]]],
    ["both mentions after a comment that holds the same text", "<!-- #x -->\n\n#x and #x", [["P", "#x"], ["P", "#x"]]],
    ["the mention after inline code and an html block", "x <code>#a</code>\n\n<div>#a</div>\n\nsee #a", [["P", "#a"]]],
    ["the mention after an inline link and an html block", '<a href="u">#a</a>\n\n<div>#a</div>\n\nsee #a', [["P", "#a"]]],
    ["the mention after a word-joined hash of the same id", "foo#x and #x", [["P", "#x"]]],
    ["the mention after escaped brackets around the same id", "[\\[x]] and [[x]]", [["P", "x"]]],
    ["the mention after a longer id in an html block", "<div>#ab</div>\n\n#a", [["P", "#a"]]],
    [
      "the mention after one that malformed html hides",
      '<div title="t\n\n#a\n\n<p title="u">\n\n#b [<foo></foo>[x]]',
      [["P", "#b"]],
    ],
  ])("links %s", (_name, body, want) => {
    expect(linkPlaces(body)).toEqual(want);
  });

  it("links the mention after an html block that closes an inline noembed, not the block's text", () => {
    const html = renderMarkdown("x <noembed>\n\n<div>\n</noembed>\n#c\n</div>\n\nsee #c", (t) => `nibs-${t}`);
    expect(html).toContain('<p>see <a data-nib-id="nibs-c"');
    expect(html.match(/mention-link/g)).toHaveLength(1);
  });

  // The comment holds the first id, and the DOM alone joins the brackets around <foo>.
  it("links every mention in a list over the alignment cap after a comment", () => {
    const items = Array.from({ length: 1100 }, (_, i) => `- #m${i}\n`).join("");
    expect(linkedTokens(`<!-- #m0 -->\n\n${items}\n[<foo></foo>[x]]`)).toEqual(listTokens(1100));
  });

  // The server finds #x, but the DOM fosters it ahead of the table's #y, and the
  // keys alone cannot tell which of the two pairs with the table's text.
  it("links nothing in a paragraph fostered ahead of a table's text", () => {
    expect(linkedTokens("<table><tr><td>#y</td></tr>\n\n#x")).toEqual([]);
  });

  // The server also finds #m and [[n]], but the DOM fosters their paragraph ahead
  // of the table's text, an order the keys cannot settle.
  it("malformed html: a paragraph fostered out of a table between rows", () => {
    const body =
      "<table>\n<tr><td>see #a1</td><td>#a2</td></tr>\n\nNote: blocked by #m and [[n]].\n\n<tr><td>c</td></tr>\n</table>\n\nAfter #z";
    expect(linkedTokens(body)).toEqual(["z"]);
  });

  // The server finds the mentions these mirrored rows name, but the collector resets
  // its open elements at each block, which the browser does not.
  it.each([
    // The browser keeps the iframe open and hides the html block's #x, whose candidate takes the visible #x.
    ["unclosed elements: an inline iframe left open into an html block", "x <iframe>\n\n<div>#x</iframe></div>\n\n#x", []],
    // The browser reopens the anchor in the next paragraph and hides x#1, whose candidate takes the visible #1.
    ["unclosed elements: an inline link left open into the next paragraph", '<a href="x">foo\n\nx#1 hid </a> #1 vis', []],
    // The markdown link closes the raw anchor, but the collector keeps it open, so 1100 mentions exceed MAX_EDITS.
    ["alignment: a raw anchor before a markdown link, then a long run of mentions", anchorThenMentions(1100), []],
  ])("%s", (_name, body, want) => {
    expect(linkedTokens(body)).toEqual(want);
  });

  // Each `[<foo></foo>[x]]` is a candidate only the DOM shows, so each one is an edit.
  it.each([
    [1000, ["y"]],
    [1001, []],
  ])("with %i edits between the DOM and the tokens, links %j", (n, want) => {
    expect(linkedTokens(`${"[<foo></foo>[x]] ".repeat(n)}\n\n#y`)).toEqual(want);
  });

  // An xmp from an html block shows the rest of the body as text, so the DOM stays
  // small while the body opens many <code> elements.
  it.each([
    ["table cells", `<xmp>\n\nx ${"<code>".repeat(20000)}\n\n| h |\n|---|\n${"| #a |\n".repeat(20000)}`],
    ["paragraphs", `<xmp>\n\n${"x <code>\n\n".repeat(80000)}#a`],
  ])("renders many open elements before %s in linear time", (_name, body) => {
    const start = performance.now();
    renderMarkdown(body, (t) => `nibs-${t}`);
    expect(performance.now() - start).toBeLessThan(1500);
  });

  it("renders many unterminated start tags in linear time", () => {
    const body = "<div\n" + "<a ".repeat(80000);
    const start = performance.now();
    renderMarkdown(body, (t) => `nibs-${t}`);
    expect(performance.now() - start).toBeLessThan(1500);
  });

  // The quoted attribute hides #h, and the DOM alone joins the brackets around <foo>,
  // so neither end pairs. 1000 items align in under 2^20 cells, and 1100 do not.
  it.each([1000, 1100])("links every mention in a list of %i after a hidden one", (n) => {
    const items = Array.from({ length: n }, (_, i) => `- #m${i}\n`).join("");
    const body = `<div title="t\n\n#h\n\n<p title="u">\n\n${items}\n[<foo></foo>[x]]`;
    expect(linkedTokens(body)).toEqual(listTokens(n));
  });

  // Shapes of raw HTML that the browser parses into a different tree than the
  // markdown structure suggests.
  const htmlShapes: [string, string][] = [
    ["escapes in text and a link", "[\\[x]] [a\\[b](u)\n\n<div>[[y]]</div>"],
    ["a leading style block", "<style>p{color:red}</style>\n\nlater #y"],
    ["a leading noscript block", "<noscript>\n\n[[x]] visible?\n\n</noscript>"],
    ["a leading template block", "<template>\n\n[[x]]\n\n</template>\n\nlater #y"],
    ["a leading title block", "<title>\n\n[[x]]\n\n</title>\n\nlater #y"],
    ["a leading div block", "<div>[[x]]</div>\n\nlater #y"],
    ["a leading comment", "<!-- c -->\n\nlater #y"],
    ["a template block", "intro\n\n<template>\n\n[[x]]\n\n</template>\n\nlater #y"],
    ["a title block", "intro\n\n<title>\n\n[[x]]\n\n</title>\n\nlater #y"],
    ["an unclosed textarea", "#a\n\n<textarea>\n[[x]]"],
    ["an unclosed style", "#a\n\n<style>\np{color:red}"],
    ["a quoted attribute left open", '<div title="t\n\n[[x]] #y'],
    ["a quoted attribute left open, then a block", '<div title="t\n\n[[x]] #y\n\n<div>z</div>\n\nafter #w'],
    ["a quoted attribute left open, then a paragraph tag", '<div title="t\n\n#a\n\n<p>x</p>\n\nhidden #b'],
    ["a cdata section in a blockquote", "> <![CDATA[\n<img src=x onerror=alert(1)>\n\nend #z"],
    ["a start tag left open at the end", "<p "],
    ["an escape in math content", "<math>\\["],
    ["a span left open in a block", "<div>\n<span\n\n[[x]]"],
    ["a block that ends inside a start tag", "intro #a\n\n<details\n\nhello [[x]] world\n\nmore #y text"],
    ["a list item block that ends inside a start tag", "- <div\n\n  [[x]] item\n- after #y"],
    ["an image tag left open", '<div>\n<img src="x.png"\n\nafter #y'],
    ["an inline xmp in a div", "<div>\nfoo <xmp>\n\nx </xmp> #y\n\n<div>z</div>\n\nend #w"],
    ["an inline noembed in a div", "<div>\n<noembed>\n\n#a\n\nx </noembed> #b\n\n<div>z</div>"],
    ["a comment left open in a div", "<div>\n<!-- start\n\nhidden [[x]]\n\nok <!-- z --> after #y\n\n<div>\nblock2\n</div>\n\nlast #w"],
    ...parityCases.map(([name, body]): [string, string] => [name, body]),
  ];

  // Mention linking only replaces text with links: with every link turned back
  // into the text it replaced, the HTML is the render without a resolver.
  it.each(htmlShapes)("changes only the mention text in %s", (_name, body) => {
    const tpl = document.createElement("template");
    tpl.innerHTML = renderMarkdown(body, (t) => `nibs-${t}`);
    for (const a of Array.from(tpl.content.querySelectorAll("a.mention-link"))) {
      const text = a.textContent ?? "";
      a.replaceWith(text.startsWith("#") ? text : `[[${text}]]`);
    }
    tpl.content.normalize();
    const plain = document.createElement("template");
    plain.innerHTML = renderMarkdown(body);
    expect(tpl.innerHTML).toBe(plain.innerHTML);
  });

  it.each(htmlShapes)("renders %s the same with a resolver that links nothing", (_name, body) => {
    expect(renderMarkdown(body, () => null)).toBe(renderMarkdown(body));
  });

  // The server also finds #a, but a link in a textarea would show as markup.
  it("leaves the text of an inline textarea as written", () => {
    const tpl = document.createElement("template");
    tpl.innerHTML = renderMarkdown("x <textarea>#a</textarea> #b", (t) => `nibs-${t}`);
    expect(tpl.content.querySelector("textarea")?.textContent).toBe("#a");
    expect(tpl.content.querySelector("a.mention-link")?.textContent).toBe("#b");
  });

  it("keeps an empty span of the body's own", () => {
    expect(renderMarkdown("<span></span> #y", (t) => `nibs-${t}`)).toContain("<span></span>");
  });

  it("keeps an escape in image alt text as plain text", () => {
    const tpl = document.createElement("template");
    tpl.innerHTML = renderMarkdown("![a\\[b](x.png)", (t) => `nibs-${t}`);
    expect(tpl.content.querySelector("img")?.getAttribute("alt")).toBe("a[b");
  });
});

describe("renderMarkdown task-list checkboxes", () => {
  it("renders task items as ENABLED checkboxes (no disabled attr)", () => {
    const html = renderMarkdown("- [ ] one\n- [x] two");
    expect(html).toContain("<input");
    expect(html).toContain('type="checkbox"');
    expect(html).not.toContain("disabled");
  });

  it("stamps each checkbox with its document-order ordinal", () => {
    const html = renderMarkdown("- [ ] one\n- [x] two\n- [ ] three");
    expect(html).toContain('data-task-ordinal="0"');
    expect(html).toContain('data-task-ordinal="1"');
    expect(html).toContain('data-task-ordinal="2"');
  });

  it("marks a checked task item's checkbox as checked", () => {
    const html = renderMarkdown("- [ ] unchecked\n- [x] checked");
    // Parse to inspect the checked property robustly (attr serialization varies).
    const tpl = document.createElement("template");
    tpl.innerHTML = html;
    const boxes = tpl.content.querySelectorAll<HTMLInputElement>("input[data-task-ordinal]");
    expect(boxes).toHaveLength(2);
    expect(boxes[0].hasAttribute("checked")).toBe(false);
    expect(boxes[1].hasAttribute("checked")).toBe(true);
  });

  it("assigns ordinals across nesting in document order", () => {
    const html = renderMarkdown("- [ ] a\n  - [x] b\n- [ ] c");
    const tpl = document.createElement("template");
    tpl.innerHTML = html;
    const ords = Array.from(
      tpl.content.querySelectorAll<HTMLInputElement>("input[data-task-ordinal]"),
    ).map((el) => el.getAttribute("data-task-ordinal"));
    expect(ords).toEqual(["0", "1", "2"]);
  });

  it("emits NO checkbox for a `[ ]` inside a fenced code block", () => {
    const html = renderMarkdown("```\n- [ ] not a task\n```");
    expect(html).not.toContain("data-task-ordinal");
    expect(html).not.toContain('type="checkbox"');
    expect(html).toContain("<pre>");
  });

  it("resets ordinals per render call (no cross-call leakage)", () => {
    renderMarkdown("- [ ] a\n- [ ] b\n- [ ] c");
    const html = renderMarkdown("- [ ] only");
    expect(html).toContain('data-task-ordinal="0"');
    expect(html).not.toContain('data-task-ordinal="1"');
  });

  it("strips a non-checkbox raw <input> smuggled via inline HTML", () => {
    const html = renderMarkdown('before <input type="text" value="x"> after');
    expect(html).not.toContain("<input");
    expect(html).toContain("before");
    expect(html).toContain("after");
  });

  it("strips a raw <input> with an event handler (defense in depth)", () => {
    const html = renderMarkdown('x <input type="button" onclick="alert(1)"> y');
    expect(html).not.toContain("<input");
    expect(html).not.toMatch(/onclick/i);
  });

  it("keeps task checkboxes clickable alongside a resolved mention", () => {
    const html = renderMarkdown("- [ ] do #gx0f", (t) => (t === "gx0f" ? "nibs-gx0f" : null));
    expect(html).toContain('data-task-ordinal="0"');
    expect(html).toContain('data-nib-id="nibs-gx0f"');
  });
});

describe("toggleTaskLine", () => {
  it("flips an unchecked item to checked (ordinal 0)", () => {
    expect(toggleTaskLine("- [ ] task", 0)).toBe("- [x] task");
  });

  it("flips a checked item back to unchecked", () => {
    expect(toggleTaskLine("- [x] task", 0)).toBe("- [ ] task");
  });

  it("treats uppercase [X] as checked and unchecks it", () => {
    expect(toggleTaskLine("- [X] task", 0)).toBe("- [ ] task");
  });

  it("flips only the addressed ordinal, leaving other task lines untouched", () => {
    const body = "- [ ] a\n- [ ] b\n- [ ] c";
    expect(toggleTaskLine(body, 1)).toBe("- [ ] a\n- [x] b\n- [ ] c");
  });

  it("maps ordinals by position, NOT text — duplicate lines don't drift", () => {
    const body = "- [ ] same\n- [ ] same\n- [ ] same";
    expect(toggleTaskLine(body, 2)).toBe("- [ ] same\n- [ ] same\n- [x] same");
    expect(toggleTaskLine(body, 0)).toBe("- [x] same\n- [ ] same\n- [ ] same");
  });

  it("preserves indentation and nesting when flipping a nested item", () => {
    const body = "- [ ] parent\n  - [ ] child\n  - [x] child2";
    expect(toggleTaskLine(body, 1)).toBe("- [ ] parent\n  - [x] child\n  - [x] child2");
    expect(toggleTaskLine(body, 2)).toBe("- [ ] parent\n  - [ ] child\n  - [ ] child2");
  });

  it("supports * and + bullets", () => {
    expect(toggleTaskLine("* [ ] star", 0)).toBe("* [x] star");
    expect(toggleTaskLine("+ [x] plus", 0)).toBe("+ [ ] plus");
  });

  it("supports ordered-list task markers (matches marked)", () => {
    expect(toggleTaskLine("1. [ ] first", 0)).toBe("1. [x] first");
    expect(toggleTaskLine("2) [x] second", 0)).toBe("2) [ ] second");
  });

  it("preserves surrounding non-task content and trailing text", () => {
    const body = "# Title\n\nSome text.\n\n- [ ] todo item with **bold**\n\nMore text.";
    expect(toggleTaskLine(body, 0)).toBe(
      "# Title\n\nSome text.\n\n- [x] todo item with **bold**\n\nMore text.",
    );
  });

  it("does NOT count a `[ ]` inside a fenced code block (ordinal skips it)", () => {
    const body = "- [ ] real\n```\n- [ ] fake in code\n```\n- [ ] real2";
    // Ordinal 1 is the SECOND real task, not the fenced one.
    expect(toggleTaskLine(body, 1)).toBe(
      "- [ ] real\n```\n- [ ] fake in code\n```\n- [x] real2",
    );
  });

  it("leaves a fenced-code `[ ]` untouched even when flipping around it", () => {
    const body = "```\n- [ ] fake\n```\n- [ ] real";
    expect(toggleTaskLine(body, 0)).toBe("```\n- [ ] fake\n```\n- [x] real");
  });

  it("handles tilde fences too", () => {
    const body = "~~~\n- [ ] fake\n~~~\n- [ ] real";
    expect(toggleTaskLine(body, 0)).toBe("~~~\n- [ ] fake\n~~~\n- [x] real");
  });

  it("returns the body unchanged for an out-of-range ordinal", () => {
    const body = "- [ ] a\n- [ ] b";
    expect(toggleTaskLine(body, 5)).toBe(body);
    expect(toggleTaskLine(body, -1)).toBe(body);
  });

  it("returns the body unchanged when there are no task lines", () => {
    const body = "just a paragraph\n\n- a plain bullet\n1. an ordered item";
    expect(toggleTaskLine(body, 0)).toBe(body);
  });

  it("does not treat a bare `- [ ]` with no content as a task (matches marked)", () => {
    // `- [ ]` (no trailing content) is not a task item; the real one is ordinal 0.
    const body = "- [ ]\n- [ ] real";
    expect(toggleTaskLine(body, 0)).toBe("- [ ]\n- [x] real");
  });

  it("does not treat `[ ]` mid-text as a task", () => {
    expect(toggleTaskLine("- foo [ ] bar", 0)).toBe("- foo [ ] bar");
  });
});

// These lock the fixes for review findings #1/#2/#4: the ordinal->source-line
// map is derived from marked's tokenizer (the same parser that renders), so
// clicking the Nth rendered checkbox flips the Nth task line even for the
// constructs a flat line-scanner diverges on (blockquote, imperfect fence
// detection, tab/HTML-block indented code) — a regex scanner flips the WRONG
// source line (or a line inside code) for every case below.
describe("toggleTaskLine — divergence classes (token-derived map)", () => {
  it("#1 blockquoted task: flips the QUOTED line, not the real one below it", () => {
    // Old impl: `> - [ ]` never matched, so ordinal 0 flipped `real` (line 1).
    expect(toggleTaskLine("> - [ ] quoted\n- [ ] real", 0)).toBe("> - [x] quoted\n- [ ] real");
    expect(toggleTaskLine("> - [ ] quoted\n- [ ] real", 1)).toBe("> - [ ] quoted\n- [x] real");
  });

  it("#1 tasks nested inside a blockquote map in document order", () => {
    const body = "> - [ ] q1\n> - [x] q2\n\n- [ ] real";
    expect(toggleTaskLine(body, 0)).toBe("> - [x] q1\n> - [x] q2\n\n- [ ] real");
    expect(toggleTaskLine(body, 1)).toBe("> - [ ] q1\n> - [ ] q2\n\n- [ ] real");
    expect(toggleTaskLine(body, 2)).toBe("> - [ ] q1\n> - [x] q2\n\n- [x] real");
  });

  it("#2 fence closer with trailing content: whole block is code, so NO tasks", () => {
    // ```x is not a valid closer -> marked treats everything as one code block.
    const body = "```\n- [ ] a\n```x\n- [ ] b";
    expect(taskSourceLines(body)).toEqual([]);
    expect(toggleTaskLine(body, 0)).toBe(body); // nothing to flip
  });

  it("#2 fence indented >=4 spaces inside a nested list item is code (not a task)", () => {
    const body = "- outer\n\n    ```\n    - [ ] fake\n    ```\n\n- [ ] real";
    // Only `real` (line 6) is a task; the fenced `- [ ] fake` is code.
    expect(taskSourceLines(body)).toEqual([6]);
    expect(toggleTaskLine(body, 0)).toBe(
      "- outer\n\n    ```\n    - [ ] fake\n    ```\n\n- [x] real",
    );
  });

  it("#4 tab-indented top-level code: only the real task flips", () => {
    // `\t- [ ] tabbed` is an indented code block; `real` is the only task.
    const body = "\t- [ ] tabbed\n- [ ] real";
    expect(taskSourceLines(body)).toEqual([1]);
    expect(toggleTaskLine(body, 0)).toBe("\t- [ ] tabbed\n- [x] real");
  });

  it("#4 HTML-block code: a `[ ]` inside an HTML block is not a task", () => {
    const body = "<div>\n- [ ] x\n</div>\n\n- [ ] real";
    expect(taskSourceLines(body)).toEqual([4]);
    expect(toggleTaskLine(body, 0)).toBe("<div>\n- [ ] x\n</div>\n\n- [x] real");
  });

  it("nested tasks map to their own indented source lines", () => {
    const body = "- [ ] a\n  - [x] b\n- [ ] c";
    expect(taskSourceLines(body)).toEqual([0, 1, 2]);
    expect(toggleTaskLine(body, 1)).toBe("- [ ] a\n  - [ ] b\n- [ ] c");
  });

  it("CRLF bodies flip the correct line and preserve \\r\\n endings", () => {
    const body = "- [ ] a\r\n- [x] b";
    expect(taskSourceLines(body)).toEqual([0, 1]);
    expect(toggleTaskLine(body, 0)).toBe("- [x] a\r\n- [x] b");
    expect(toggleTaskLine(body, 1)).toBe("- [ ] a\r\n- [ ] b");
  });

  it("duplicate task lines flip by position, not text", () => {
    const body = "- [ ] same\n- [ ] same\n- [ ] same";
    expect(taskSourceLines(body)).toEqual([0, 1, 2]);
    expect(toggleTaskLine(body, 1)).toBe("- [ ] same\n- [x] same\n- [ ] same");
  });
});

// Findings #1/#2 (round 2): the ordinal->source-line map must stay MONOTONIC
// over the FULL token stream. A non-structural token (fenced code documenting
// checklist syntax, or prose echoing `- [ ] x`) sitting before/between real
// tasks must be CONSUMED by the search cursor so a real task's checkbox can
// never back-match into it; and lone-`\r` / mixed line endings must map each
// checkbox to its own line (marked normalizes lone `\r` to `\n`).
describe("toggleTaskLine — cursor must not back-match into non-task regions (#1)", () => {
  it("a fenced ```md block documenting `- [ ]` before the real task does not steal the ordinal", () => {
    const body = "How to:\n\n```md\n- [ ] Buy milk\n```\n\n- [ ] Buy milk";
    // The ONLY real task is line 6; line 3 lives inside the code fence.
    expect(taskSourceLines(body)).toEqual([6]);
    expect(toggleTaskLine(body, 0)).toBe(
      "How to:\n\n```md\n- [ ] Buy milk\n```\n\n- [x] Buy milk",
    );
  });

  it("a ```md doc fence BETWEEN two real tasks does not shift either ordinal", () => {
    const body = "- [ ] real\n\n```md\n- [ ] doc\n```\n\n- [ ] real2";
    expect(taskSourceLines(body)).toEqual([0, 6]);
    expect(toggleTaskLine(body, 0)).toBe(
      "- [x] real\n\n```md\n- [ ] doc\n```\n\n- [ ] real2",
    );
    expect(toggleTaskLine(body, 1)).toBe(
      "- [ ] real\n\n```md\n- [ ] doc\n```\n\n- [x] real2",
    );
  });

  it("a prose paragraph literally containing `- [ ] a` between two real tasks maps to the REAL lines", () => {
    const body = "- [ ] a\n\ntext that says - [ ] a literally as prose\n\n- [ ] a";
    expect(taskSourceLines(body)).toEqual([0, 4]);
    // Flipping ordinal 1 must touch the real line 4, NOT the prose echo on line 2.
    expect(toggleTaskLine(body, 1)).toBe(
      "- [ ] a\n\ntext that says - [ ] a literally as prose\n\n- [x] a",
    );
    expect(toggleTaskLine(body, 0)).toBe(
      "- [x] a\n\ntext that says - [ ] a literally as prose\n\n- [ ] a",
    );
  });

  it("an HTML block containing `- [ ]` before the real task does not steal the ordinal", () => {
    const body = "<pre>- [ ] second</pre>\n\n- [ ] second";
    const map = taskSourceLines(body);
    expect(map).toHaveLength(1);
    expect(map).toEqual([2]);
    expect(toggleTaskLine(body, 0)).toBe("<pre>- [ ] second</pre>\n\n- [x] second");
  });
});

describe("toggleTaskLine — line-ending normalization (#2)", () => {
  it("lone-`\\r` separated tasks map to distinct lines and preserve the `\\r`", () => {
    const body = "- [ ] a\r- [ ] b";
    expect(taskSourceLines(body)).toEqual([0, 1]);
    expect(toggleTaskLine(body, 1)).toBe("- [ ] a\r- [x] b");
    expect(toggleTaskLine(body, 0)).toBe("- [x] a\r- [ ] b");
  });

  it("mixed CRLF/LF/CR in one body maps each checkbox to its own line", () => {
    const body = "- [ ] a\r\n- [ ] b\n- [ ] c\r- [ ] d";
    expect(taskSourceLines(body)).toEqual([0, 1, 2, 3]);
    expect(toggleTaskLine(body, 0)).toBe("- [x] a\r\n- [ ] b\n- [ ] c\r- [ ] d");
    expect(toggleTaskLine(body, 1)).toBe("- [ ] a\r\n- [x] b\n- [ ] c\r- [ ] d");
    expect(toggleTaskLine(body, 2)).toBe("- [ ] a\r\n- [ ] b\n- [x] c\r- [ ] d");
    expect(toggleTaskLine(body, 3)).toBe("- [ ] a\r\n- [ ] b\n- [ ] c\r- [x] d");
  });
});

// A fenced/indented code block echoing `- [ ]` INSIDE a list item, FOLLOWED by
// a multi-line nested sub-task, is the trap: back-matching the sub-task's
// ordinal lands on a source line INSIDE the code fence (silent code-block
// corruption on Save). The ordinal->line map is instead derived by
// accumulating LINE offsets over marked's own token tree (a `- [ ]` inside a
// `code`/`html`/prose span is inert text, never a `checkbox` token), so it can
// never be recorded and a real checkbox can never map onto it.
describe("toggleTaskLine — code-echo INSIDE a list item, then a nested sub-task (#round3)", () => {
  it("the proven failing body maps to [0, 4], NOT [0, 2] inside the fence", () => {
    const body = "- [ ] Set up CI\n  ```\n  - [ ] example\n  ```\n  - [ ] Deploy\n    - detail";
    expect(taskSourceLines(body)).toEqual([0, 4]);
    // ordinal 1 is "Deploy" (line 4) — NOT the fenced `- [ ] example` (line 2).
    // The fenced echo stays `- [ ] example`; only Deploy flips.
    expect(toggleTaskLine(body, 1)).toBe(
      "- [ ] Set up CI\n  ```\n  - [ ] example\n  ```\n  - [x] Deploy\n    - detail",
    );
    expect(toggleTaskLine(body, 0)).toBe(
      "- [x] Set up CI\n  ```\n  - [ ] example\n  ```\n  - [ ] Deploy\n    - detail",
    );
  });

  it("yaml fence in item + nested sub-task maps to [0, 5]", () => {
    const body =
      "- [ ] Configure pipeline\n  ```yaml\n  steps:\n    - [ ] run\n  ```\n  - [ ] Add caching\n    - use actions/cache";
    expect(taskSourceLines(body)).toEqual([0, 5]);
    expect(toggleTaskLine(body, 1)).toBe(
      "- [ ] Configure pipeline\n  ```yaml\n  steps:\n    - [ ] run\n  ```\n  - [x] Add caching\n    - use actions/cache",
    );
  });

  it("multiple independent fence-echo occurrences (multi-line subtasks) do not compound", () => {
    // Each sub-task ("b", "d") is MULTI-LINE (has a child bullet), so the old
    // impl parked the cursor and mapped ordinals 1 and 3 INTO the fences
    // (old buggy map ~= [0, 2, 8, 10]). Both must resolve to the real subtasks.
    const body =
      "- [ ] a\n  ```\n  - [ ] x\n  ```\n  - [ ] b\n    - d1\n- [ ] c\n  ```\n  - [ ] y\n  ```\n  - [ ] d\n    - d2";
    expect(taskSourceLines(body)).toEqual([0, 4, 6, 10]);
    expect(toggleTaskLine(body, 1)).toBe(
      "- [ ] a\n  ```\n  - [ ] x\n  ```\n  - [x] b\n    - d1\n- [ ] c\n  ```\n  - [ ] y\n  ```\n  - [ ] d\n    - d2",
    );
    expect(toggleTaskLine(body, 3)).toBe(
      "- [ ] a\n  ```\n  - [ ] x\n  ```\n  - [ ] b\n    - d1\n- [ ] c\n  ```\n  - [ ] y\n  ```\n  - [x] d\n    - d2",
    );
  });

  it("ordered-list outer marker with a fence echo + nested multi-line subtask maps to [0, 4]", () => {
    const body = "1. [ ] Set up\n   ```\n   - [ ] echo\n   ```\n   - [ ] Deploy\n     - detail";
    expect(taskSourceLines(body)).toEqual([0, 4]);
    expect(toggleTaskLine(body, 1)).toBe(
      "1. [ ] Set up\n   ```\n   - [ ] echo\n   ```\n   - [x] Deploy\n     - detail",
    );
  });

  it("a fence echo nested inside a blockquote item + sub-task maps to [0, 4]", () => {
    const body =
      "> - [ ] Set up\n>   ```\n>   - [ ] echo\n>   ```\n>   - [ ] Deploy\n>     - detail";
    expect(taskSourceLines(body)).toEqual([0, 4]);
    expect(toggleTaskLine(body, 1)).toBe(
      "> - [ ] Set up\n>   ```\n>   - [ ] echo\n>   ```\n>   - [x] Deploy\n>     - detail",
    );
  });
});

// The regression LOCK the test-reviewer asked for: render each tricky body,
// read the DOM `data-task-ordinal` values in document order, then assert that
// `toggleTaskLine(body, ord)` flips EXACTLY the checkbox rendered at that ordinal
// (and no other) — so the render side and the flip side cannot silently drift
// apart. Under a regex scanner the blockquote / fence / tab / HTML cases flip
// the wrong (or a code) line, changing a different checkbox.
describe("ordinal-consistency cross-check (render <-> toggleTaskLine)", () => {
  const trickyBodies: Record<string, string> = {
    simple: "- [ ] a\n- [x] b\n- [ ] c",
    blockquote: "> - [ ] quoted\n- [ ] real",
    "blockquote multi": "> - [ ] q1\n> - [x] q2\n\n- [ ] real",
    nested: "- [ ] a\n  - [x] b\n- [ ] c",
    "deeply nested": "- [ ] a\n  - [ ] b\n    - [x] c\n- [ ] d",
    "loose (blank lines)": "- [ ] a\n\n  - [ ] b\n\n- [ ] c",
    "fence between tasks": "- [ ] real\n```\n- [ ] fake\n```\n- [ ] real2",
    "wide-indent nested fence": "- outer\n\n    ```\n    - [ ] fake\n    ```\n\n- [ ] real",
    "tab-indented code": "\t- [ ] tabbed\n- [ ] real",
    "html block": "<div>\n- [ ] x\n</div>\n\n- [ ] real",
    "ordered markers": "1. [ ] first\n2) [x] second",
    "star and plus bullets": "* [ ] star\n+ [x] plus",
    crlf: "- [ ] a\r\n- [x] b",
    duplicates: "- [ ] same\n- [ ] same\n- [ ] same",
    "uppercase X": "- [X] done\n- [ ] todo",
    "text after checkbox with brackets": "- [ ] see [x] in text\n- [ ] two",
    // #1: a fenced ```md block documenting `- [ ]` syntax must not steal an
    // ordinal. IDENTICAL text to the real task so the list `.raw` back-matches
    // into the fence unless the cursor has consumed the code block.
    "doc fence before task": "How to:\n\n```md\n- [ ] task\n```\n\n- [ ] task",
    "doc fence between tasks": "- [ ] task\n\n```md\n- [ ] task\n```\n\n- [ ] task",
    "doc fence after task": "- [ ] task\n\n```md\n- [ ] task\n```",
    // #1: prose that literally echoes `- [ ] a` between two real tasks.
    "prose echo between tasks": "- [ ] a\n\ntext with - [ ] a inline as prose\n\n- [ ] a",
    // #1 (adversarial): HTML comment / top-level 4-space indented code echoing a task.
    "html comment echo": "<!-- - [ ] hidden -->\n\n- [ ] hidden",
    "top-level indented-code echo": "    - [ ] snippet\n\n- [ ] snippet",
    // #2: lone `\r` and mixed line endings — each checkbox its own line.
    "lone CR separators": "- [ ] a\r- [ ] b",
    "mixed CRLF/LF/CR": "- [ ] a\r\n- [ ] b\n- [ ] c\r- [ ] d",
    // #round3: a fenced/indented code echo INSIDE a list item, followed by a
    // multi-line nested sub-task — the shape all three prior rounds shipped
    // broken. The sub-task must NOT map onto a line inside the fence.
    "fence echo in item + nested subtask (proven)":
      "- [ ] Set up CI\n  ```\n  - [ ] example\n  ```\n  - [ ] Deploy\n    - detail",
    "yaml fence in item + nested subtask":
      "- [ ] Configure pipeline\n  ```yaml\n  steps:\n    - [ ] run\n  ```\n  - [ ] Add caching\n    - use actions/cache",
    "tilde fence in item + nested subtask":
      "- [ ] Set up\n  ~~~\n  - [ ] echo\n  ~~~\n  - [ ] Deploy\n    - more",
    "fence echo in item, ordered outer + nested subtask":
      "1. [ ] Set up\n   ```\n   - [ ] echo\n   ```\n   - [ ] Deploy\n     - detail",
    "fence echo nested in blockquote":
      "> - [ ] Set up\n>   ```\n>   - [ ] echo\n>   ```\n>   - [ ] Deploy\n>     - detail",
    "multiple fence-echo occurrences (multi-line subtasks)":
      "- [ ] a\n  ```\n  - [ ] x\n  ```\n  - [ ] b\n    - d1\n- [ ] c\n  ```\n  - [ ] y\n  ```\n  - [ ] d\n    - d2",
    "4-space nested fence in loose item + multi-line subtask":
      "- [ ] Set up\n\n    ```\n    - [ ] echo\n    ```\n\n  - [ ] Deploy\n    - detail",
    "tab-indented fence in item + multi-line subtask":
      "- [ ] Set up\n\t```\n\t- [ ] echo\n\t```\n  - [ ] Deploy\n    - detail",
    "very deeply nested (4 levels)": "- [ ] a\n  - [ ] b\n    - [ ] c\n      - [x] d",
  };

  for (const [name, body] of Object.entries(trickyBodies)) {
    it(`ordinals stay aligned with source lines: ${name}`, () => {
      const before = renderedCheckboxStates(renderMarkdown(body));
      // Rendered ordinals must be a contiguous 0..n-1 document-order sequence.
      expect(renderedOrdinals(renderMarkdown(body))).toEqual(before.map((_, i) => i));
      // The source-line map must have exactly one entry per rendered checkbox.
      expect(taskSourceLines(body)).toHaveLength(before.length);

      for (let ord = 0; ord < before.length; ord++) {
        const flipped = toggleTaskLine(body, ord);
        const after = renderedCheckboxStates(renderMarkdown(flipped));
        // No checkbox appears/disappears (we never flip a code/non-task line).
        expect(after).toHaveLength(before.length);
        for (let i = 0; i < before.length; i++) {
          // Only the addressed ordinal's checkbox changes; all others are stable.
          expect(after[i]).toBe(i === ord ? !before[i] : before[i]);
        }
      }
    });
  }
});

// a raw-HTML checkbox (with or without a forged data-task-ordinal)
// must NOT become a clickable, ordinal-bearing task checkbox, and must not shift
// the ordinals of real task items around it. Provenance is enforced at the
// sanitize boundary via a per-render nonce that body content cannot predict.
describe("renderMarkdown task-checkbox provenance", () => {
  it("strips a raw <input type=checkbox> with a forged data-task-ordinal", () => {
    const html = renderMarkdown(
      '- [ ] real\n\n<input type="checkbox" data-task-ordinal="0" checked onclick="alert(1)"> decoy',
    );
    // Exactly one checkbox survives — the real task item at ordinal 0.
    expect(renderedOrdinals(html)).toEqual([0]);
    expect(html).not.toMatch(/onclick/i);
  });

  it("strips a raw <input type=checkbox> with no ordinal (decoy neutralized)", () => {
    const html = renderMarkdown('<input type="checkbox"> decoy\n\n- [ ] real');
    const tpl = document.createElement("template");
    tpl.innerHTML = html;
    // The decoy must be REMOVED, not merely ordinal-less: only the real task's
    // checkbox may survive sanitization. (Counting inputs — not filtering on
    // data-task-ordinal — is what actually observes the strip.)
    expect(tpl.content.querySelectorAll("input")).toHaveLength(1);
    const survivor = tpl.content.querySelector("input");
    expect(survivor?.getAttribute("data-task-ordinal")).toBe("0");
    expect(renderedOrdinals(html)).toEqual([0]);
  });

  it("strips a malformed raw <input> with junk/unquoted attrs adjacent to a real task", () => {
    // marked escapes malformed inline HTML to text before DOMPurify runs; either
    // way the forged control must not survive as a clickable checkbox.
    const html = renderMarkdown('<input type=checkbox foo=bar baz> decoy\n\n- [ ] real');
    const tpl = document.createElement("template");
    tpl.innerHTML = html;
    // Only the real task's checkbox survives; the malformed decoy is gone.
    expect(tpl.content.querySelectorAll("input")).toHaveLength(1);
    expect(renderedOrdinals(html)).toEqual([0]);
  });

  it("mints a working (non-empty) nonce via the fallback when crypto.randomUUID is absent", () => {
    // freshNonce()'s fallback path (insecure context: no crypto.randomUUID).
    // If it returned "" the `renderNonce !== ""` provenance guard would strip
    // even OUR real checkboxes — so a surviving, ordinal-bearing box proves the
    // fallback produced a usable nonce.
    vi.stubGlobal("crypto", {}); // no randomUUID -> Date.now()+Math.random() path
    try {
      const html = renderMarkdown("- [ ] a\n- [x] b");
      expect(renderedOrdinals(html)).toEqual([0, 1]);
      expect(html).not.toContain("data-task-nonce"); // still stripped from output
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("a raw checkbox before real tasks does not shift their ordinals", () => {
    const html = renderMarkdown(
      '<input type="checkbox" data-task-ordinal="5"> decoy\n\n- [ ] a\n- [ ] b',
    );
    expect(renderedOrdinals(html)).toEqual([0, 1]);
  });

  it("does not leak the provenance nonce into the rendered DOM", () => {
    const html = renderMarkdown("- [ ] a\n- [x] b");
    expect(html).not.toContain("data-task-nonce");
  });
});

// the input-hardening hook is registered on a MODULE-SCOPED
// DOMPurify instance, not the shared global default export — so it never leaks
// onto other consumers or accumulates across module re-evaluation.
describe("DOMPurify isolation", () => {
  it("does not mutate the shared global DOMPurify singleton", () => {
    renderMarkdown("- [ ] ensure our scoped hook is active");
    // The GLOBAL default export must still keep a plain text input — proof our
    // input-stripping hook is scoped to the module instance only.
    const out = DOMPurify.sanitize('<input type="text" value="y">');
    expect(out).toContain("<input");
  });
});

describe("TAG_REGEX", () => {
  it("accepts valid tags", () => {
    expect(TAG_REGEX.test("auth")).toBe(true);
    expect(TAG_REGEX.test("new-tag")).toBe(true);
    expect(TAG_REGEX.test("a1")).toBe(true);
    expect(TAG_REGEX.test("my-long-tag-123")).toBe(true);
  });

  it("rejects invalid tags", () => {
    expect(TAG_REGEX.test("INVALID")).toBe(false);
    expect(TAG_REGEX.test("123")).toBe(false);
    expect(TAG_REGEX.test("-start")).toBe(false);
    expect(TAG_REGEX.test("end-")).toBe(false);
    expect(TAG_REGEX.test("has space")).toBe(false);
  });
});
