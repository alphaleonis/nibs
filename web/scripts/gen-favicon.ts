// Rasterizes public/favicon.svg into public/favicon.ico (16/32/48).
//
// The ICO exists only for browsers that do not take an SVG favicon; everything
// current prefers the SVG link. It is committed rather than built, because
// generating it needs a browser and the normal build must not depend on one —
// so this is a manual step, run via `task favicon` after the SVG changes.
//
// Run with `node scripts/gen-favicon.ts` from web/ (Node strips the types).

import { chromium } from "@playwright/test";
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const HERE = dirname(fileURLToPath(import.meta.url));
const SVG = join(HERE, "..", "public", "favicon.svg");
const ICO = join(HERE, "..", "public", "favicon.ico");

// 48 is the largest size Windows/Explorer picks from an ICO in practice; past
// that a browser that can read this file would have taken the SVG anyway.
const SIZES = [16, 32, 48];

/**
 * Packs PNG buffers into an ICO container. PNG-compressed entries (rather than
 * BMP) are read by every browser and by Windows Vista onward, which is the
 * whole audience for this file.
 */
function buildIco(images: { size: number; png: Buffer }[]): Buffer {
  const HEADER = 6;
  const ENTRY = 16;
  const header = Buffer.alloc(HEADER);
  header.writeUInt16LE(0, 0); // reserved
  header.writeUInt16LE(1, 2); // 1 = icon
  header.writeUInt16LE(images.length, 4);

  let offset = HEADER + ENTRY * images.length;
  const entries: Buffer[] = [];
  for (const { size, png } of images) {
    const e = Buffer.alloc(ENTRY);
    e.writeUInt8(size >= 256 ? 0 : size, 0); // 0 encodes 256
    e.writeUInt8(size >= 256 ? 0 : size, 1);
    e.writeUInt8(0, 2); // palette size, 0 for truecolor
    e.writeUInt8(0, 3); // reserved
    e.writeUInt16LE(1, 4); // colour planes
    e.writeUInt16LE(32, 6); // bits per pixel
    e.writeUInt32LE(png.length, 8);
    e.writeUInt32LE(offset, 12);
    entries.push(e);
    offset += png.length;
  }

  return Buffer.concat([header, ...entries, ...images.map((i) => i.png)]);
}

const svg = readFileSync(SVG, "utf8");
const browser = await chromium.launch();
const page = await browser.newPage();

const images: { size: number; png: Buffer }[] = [];
for (const size of SIZES) {
  // Render at deviceScaleFactor 1 so one CSS px is one icon px — the point is
  // to capture how the mark rasterizes at exactly this size, not a downscale
  // of something larger.
  const ctx = await browser.newContext({ viewport: { width: size, height: size }, deviceScaleFactor: 1 });
  const p = await ctx.newPage();
  await p.setContent(
    `<style>html,body{margin:0;padding:0;background:transparent}svg{display:block;width:${size}px;height:${size}px}</style>${svg}`,
  );
  const png = await p.locator("svg").screenshot({ omitBackground: true });
  images.push({ size, png });
  await ctx.close();
  console.log(`  ${size}x${size}  ${png.length} bytes`);
}

await page.close();
await browser.close();

const ico = buildIco(images);
writeFileSync(ICO, ico);
console.log(`\nwrote ${ICO} (${ico.length} bytes, ${images.length} sizes)`);
