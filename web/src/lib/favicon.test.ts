import { describe, it, expect } from "vitest";
// Vite `?raw` imports keep this free of node built-ins, matching fouc-guard.test.ts.
import html from "../../index.html?raw";
import faviconSvg from "../../public/favicon.svg?raw";

// index.html points at two files in public/ that nothing else references. Vite
// copies public/ verbatim without resolving those hrefs, so a renamed or deleted
// icon fails at runtime as a missing tab icon and at no earlier point. These
// assertions are the only thing tying the markup to the files.
//
// favicon.ico is deliberately not imported: it is binary, generated from the SVG
// by `task favicon`, and a `?raw` import of it would be meaningless. Its presence
// is covered by the declaration check plus the generator's own output.

describe("index.html favicon declarations", () => {
  const iconLinks = [...html.matchAll(/<link\s+rel="icon"[^>]*>/g)].map((m) => m[0]);

  it("declares exactly the two icons that exist in public/", () => {
    expect(iconLinks).toHaveLength(2);
    expect(iconLinks.some((l) => l.includes('href="/favicon.ico"'))).toBe(true);
    expect(iconLinks.some((l) => l.includes('href="/favicon.svg"'))).toBe(true);
  });

  it("marks the SVG with its MIME type so a browser can skip it when unsupported", () => {
    const svgLink = iconLinks.find((l) => l.includes("favicon.svg"))!;
    expect(svgLink).toContain('type="image/svg+xml"');
  });

  it("lists the ICO before the SVG, so the fallback order holds", () => {
    const ico = html.indexOf('href="/favicon.ico"');
    const svg = html.indexOf('href="/favicon.svg"');
    expect(ico).toBeGreaterThan(-1);
    expect(svg).toBeGreaterThan(ico);
  });
});

describe("favicon.svg", () => {
  it("is square, so no browser letterboxes it", () => {
    const vb = faviconSvg.match(/viewBox="([-\d.]+) ([-\d.]+) ([\d.]+) ([\d.]+)"/);
    expect(vb, "favicon.svg has no viewBox").not.toBeNull();
    expect(Number(vb![3])).toBe(Number(vb![4]));
  });

  it("carries an accessible name", () => {
    expect(faviconSvg).toContain("<title>Nibs</title>");
  });

  // The ring is what makes the full mark illegible at 16px; dropping it is the
  // entire reason this is a separate asset rather than a link to logo-only.svg.
  // The ring paths are the two carrying the 0.517282 orbit transform.
  it("omits the orbiting ring that turns to mush at 16px", () => {
    expect(faviconSvg).not.toContain("0.517282");
    expect([...faviconSvg.matchAll(/<path/g)]).toHaveLength(2);
  });

  it("references only gradient ids it defines", () => {
    const defined = [...faviconSvg.matchAll(/<linearGradient\s+id="([^"]+)"/g)].map((m) => m[1]);
    const referenced = [...faviconSvg.matchAll(/fill="url\(#([^)]+)\)"/g)].map((m) => m[1]);
    expect(referenced.length).toBeGreaterThan(0);
    for (const id of referenced) expect(defined).toContain(id);
  });
});
