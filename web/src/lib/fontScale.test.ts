import { describe, it, expect, afterEach } from "vitest";
import { applyFontScale } from "./fontScale";
import { FONT_SCALES } from "./types";
import type { FontSize } from "./types";

describe("applyFontScale", () => {
  afterEach(() => {
    document.documentElement.style.removeProperty("--font-scale");
  });

  it("writes the numeric scale for each font size onto the root --font-scale", () => {
    const cases: [FontSize, string][] = [
      ["small", "0.9"],
      ["medium", "1"],
      ["large", "1.15"],
    ];
    for (const [size, expected] of cases) {
      applyFontScale(size);
      expect(document.documentElement.style.getPropertyValue("--font-scale")).toBe(expected);
    }
  });

  it("drives the root variable from the FONT_SCALES map", () => {
    applyFontScale("small");
    expect(document.documentElement.style.getPropertyValue("--font-scale")).toBe(String(FONT_SCALES.small));
    applyFontScale("large");
    expect(document.documentElement.style.getPropertyValue("--font-scale")).toBe(String(FONT_SCALES.large));
  });

  it("overwrites a previously applied scale", () => {
    applyFontScale("large");
    expect(document.documentElement.style.getPropertyValue("--font-scale")).toBe("1.15");
    applyFontScale("small");
    expect(document.documentElement.style.getPropertyValue("--font-scale")).toBe("0.9");
  });
});
