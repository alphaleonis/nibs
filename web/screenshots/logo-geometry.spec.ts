import { test, expect, type Page } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// Guards the logo assets against dead margin in their viewBoxes.
//
// This exists because it already went wrong: every asset was "corrected" to its
// getBBox(), which reports geometry far outside anything the paths draw — the
// banner's ring path claims a bbox 165 units left of its leftmost ink. That
// padding pushed the README banner 3.2% right of centre and shrank the mark in
// the app header by 28%, and neither symptom looked like a viewBox problem.
//
// So these assertions measure INK, by rasterizing and scanning the alpha
// channel. getBBox() would re-introduce the exact bug being guarded against,
// and a visual check cannot help either: artwork touching a viewBox edge is
// indistinguishable from artwork clipped by it.
//
// Lives here rather than in the vitest suite because it needs a real renderer,
// and because `?raw` cannot reach assets/ from web/src.

const REPO = join(import.meta.dirname, "..", "..");
const ASSETS = join(REPO, "assets", "logo");

type Ink = { l: number; t: number; r: number; b: number; w: number; h: number };

async function inkMargins(page: Page, svgText: string): Promise<Ink> {
  const clean = svgText.replace(/<\?xml[^>]*\?>/, "").replace(/<!DOCTYPE[^>]*>/, "");
  return page.evaluate(async (text) => {
    const m = text.match(/viewBox="([-\d.]+) ([-\d.]+) ([\d.]+) ([\d.]+)"/);
    if (!m) throw new Error("no viewBox");
    const VB = { w: +m[3], h: +m[4] };
    const scale = 1600 / Math.max(VB.w, VB.h);
    const W = Math.round(VB.w * scale);
    const H = Math.round(VB.h * scale);
    const data = await new Promise<Uint8ClampedArray>((res, rej) => {
      const img = new Image();
      img.onload = () => {
        const cv = document.createElement("canvas");
        cv.width = W;
        cv.height = H;
        const ctx = cv.getContext("2d", { willReadFrequently: true })!;
        ctx.clearRect(0, 0, W, H);
        ctx.drawImage(img, 0, 0, W, H);
        res(ctx.getImageData(0, 0, W, H).data);
      };
      img.onerror = () => rej(new Error("svg failed to rasterize"));
      img.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(text);
    });
    let minX = W, maxX = -1, minY = H, maxY = -1;
    for (let y = 0; y < H; y++) {
      for (let x = 0; x < W; x++) {
        if (data[(y * W + x) * 4 + 3] > 8) {
          if (x < minX) minX = x;
          if (x > maxX) maxX = x;
          if (y < minY) minY = y;
          if (y > maxY) maxY = y;
        }
      }
    }
    if (maxX < 0) throw new Error("no ink at all");
    // margins as a fraction of the viewBox, so thresholds are scale-free
    return {
      l: minX / W,
      t: minY / H,
      r: (W - 1 - maxX) / W,
      b: (H - 1 - maxY) / H,
      w: VB.w,
      h: VB.h,
    };
  }, clean);
}

const SOURCE_ASSETS = [
  "banner-dark-text.svg",
  "banner-white-text.svg",
  "logo-and-dark-text.svg",
  "logo-and-white-text.svg",
  "logo-only.svg",
];

test.describe("logo asset geometry", () => {
  for (const name of SOURCE_ASSETS) {
    test(`${name} has no dead margin`, async ({ page }) => {
      await page.goto("about:blank");
      const ink = await inkMargins(page, readFileSync(join(ASSETS, name), "utf8"));

      // The artboards are tight: ink reaches all four edges. 1% leaves room for
      // antialiasing without admitting the 6-14% of padding the bbox added.
      for (const [side, v] of Object.entries({ left: ink.l, top: ink.t, right: ink.r, bottom: ink.b })) {
        expect(v, `${name}: ${side} margin is ${(v * 100).toFixed(1)}% of the viewBox`).toBeLessThan(0.01);
      }
    });
  }

  test("favicon.svg is padded symmetrically", async ({ page }) => {
    await page.goto("about:blank");
    const ink = await inkMargins(page, readFileSync(join(REPO, "web", "public", "favicon.svg"), "utf8"));

    // Unlike the source assets this one is deliberately padded, so the check is
    // symmetry rather than tightness — an off-centre icon is the failure mode.
    expect(Math.abs(ink.l - ink.r), "horizontal padding is uneven").toBeLessThan(0.01);
    expect(Math.abs(ink.t - ink.b), "vertical padding is uneven").toBeLessThan(0.01);
    expect(ink.w).toBe(ink.h);
  });

  test("NibsLogo.svelte tracks the viewBox of the asset it was derived from", async () => {
    const vb = (s: string) => s.match(/viewBox="([^"]+)"/)?.[1];
    const asset = vb(readFileSync(join(ASSETS, "banner-white-text.svg"), "utf8"));
    const component = vb(readFileSync(join(REPO, "web", "src", "lib", "components", "NibsLogo.svelte"), "utf8"));

    expect(asset).toBeTruthy();
    expect(component).toBe(asset);
  });
});
