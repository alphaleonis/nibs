// Rasterizes public/favicon.svg into public/favicon.ico (16/32/48), for
// browsers without SVG favicon support. Run `task favicon` after editing the
// SVG and commit the result; the build must not depend on a browser.

import { chromium } from "@playwright/test";
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const HERE = dirname(fileURLToPath(import.meta.url));
const SVG = join(HERE, "..", "public", "favicon.svg");
const ICO = join(HERE, "..", "public", "favicon.ico");

const SIZES = [16, 32, 48];

/** Packs PNG buffers into an ICO container (PNG entries, not BMP). */
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
  // deviceScaleFactor 1: rasterize at this exact size, not a downscale.
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
