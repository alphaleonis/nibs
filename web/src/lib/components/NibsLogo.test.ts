import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/svelte";
import NibsLogo from "./NibsLogo.svelte";

describe("NibsLogo", () => {
  it("exposes the banner as an image named Nibs by default", () => {
    render(NibsLogo);

    const svg = screen.getByRole("img", { name: "Nibs" });
    expect(svg.tagName.toLowerCase()).toBe("svg");
  });

  it("goes decorative when label is empty, so adjacent text is not doubled", () => {
    const { container } = render(NibsLogo, { label: "" });

    const svg = container.querySelector("svg")!;
    expect(svg).toHaveAttribute("aria-hidden", "true");
    expect(svg).not.toHaveAttribute("aria-label");
    expect(screen.queryByRole("img")).not.toBeInTheDocument();
  });

  it("passes class through to the svg", () => {
    const { container } = render(NibsLogo, { class: "h-10 w-auto" });

    expect(container.querySelector("svg")).toHaveClass("h-10", "w-auto");
  });

  // The reason this component exists rather than an <img> to one of the exported
  // files: the wordmark has to inherit the text color. A baked-in fill fails at
  // least one theme (white is 1.04:1 on daylight, the gray gradient is 1.7-2.3:1
  // on the three dark themes), so a hardcoded color here is a real regression.
  it("fills the wordmark with currentColor, not a fixed color", () => {
    const { container } = render(NibsLogo);

    const wordmark = container.querySelector("g[fill]")!;
    expect(wordmark).toHaveAttribute("fill", "currentColor");
    // Four glyphs (N, I, B, S), none overriding the inherited fill.
    const glyphs = [...wordmark.children];
    expect(glyphs).toHaveLength(4);
    for (const glyph of glyphs) expect(glyph).not.toHaveAttribute("fill");
  });

  it("keeps the mark's gradients off currentColor", () => {
    const { container } = render(NibsLogo);

    const gradientFilled = [...container.querySelectorAll("path[fill^='url(']")];
    expect(gradientFilled).toHaveLength(4);
  });

  // Two banners on one page would otherwise define each gradient id twice, and
  // every url(#id) reference resolves to whichever element came first.
  it("gives each instance its own gradient ids", () => {
    const { container } = render(NibsLogo);
    const { container: second } = render(NibsLogo);

    const idsOf = (root: HTMLElement) => [...root.querySelectorAll("linearGradient")].map((g) => g.id);
    const first = idsOf(container);
    const other = idsOf(second);

    expect(first).toHaveLength(3);
    expect(new Set(first).size).toBe(3);
    expect(first.some((id) => other.includes(id))).toBe(false);
  });

  it("references only gradient ids it defines", () => {
    const { container } = render(NibsLogo);

    const defined = new Set([...container.querySelectorAll("linearGradient")].map((g) => g.id));
    const referenced = [...container.querySelectorAll("[fill^='url(']")].map(
      (el) => el.getAttribute("fill")!.match(/^url\(#(.*)\)$/)![1],
    );

    expect(referenced.length).toBeGreaterThan(0);
    for (const id of referenced) expect(defined).toContain(id);
  });

  // The Affinity artboard cropped the ring and swoosh; the viewBox here is the
  // measured content bbox, so a re-export that reintroduces the crop is caught.
  it("uses the uncropped viewBox", () => {
    const { container } = render(NibsLogo);

    expect(container.querySelector("svg")).toHaveAttribute("viewBox", "-167 -203 2956 1371");
  });
});
