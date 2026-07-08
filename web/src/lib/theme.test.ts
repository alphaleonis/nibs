import { describe, it, expect, afterEach } from "vitest";
import { applyTheme } from "./theme";
import type { Theme } from "./types";

describe("applyTheme", () => {
  afterEach(() => {
    delete document.documentElement.dataset.theme;
    document.documentElement.classList.remove("dark");
  });

  it("sets data-theme on the document element", () => {
    applyTheme("dracula");
    expect(document.documentElement.dataset.theme).toBe("dracula");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dracula");
  });

  it("overwrites a previously applied theme", () => {
    applyTheme("graphite");
    expect(document.documentElement.dataset.theme).toBe("graphite");
    applyTheme("midnight");
    expect(document.documentElement.dataset.theme).toBe("midnight");
  });

  it("keeps `.dark` present for every dark theme", () => {
    for (const theme of ["graphite", "midnight", "dracula"] as const) {
      document.documentElement.classList.remove("dark");
      applyTheme(theme);
      expect(document.documentElement.classList.contains("dark"), theme).toBe(true);
      expect(document.documentElement.dataset.theme).toBe(theme);
    }
  });

  it("removes `.dark` for the light Daylight theme", () => {
    document.documentElement.classList.add("dark");
    applyTheme("daylight");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    expect(document.documentElement.dataset.theme).toBe("daylight");
  });

  it("flips `.dark` correctly when switching from a dark start", () => {
    document.documentElement.classList.add("dark");
    applyTheme("graphite");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    applyTheme("daylight");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    applyTheme("midnight");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("flips `.dark` correctly when switching from a light start", () => {
    document.documentElement.classList.remove("dark");
    applyTheme("daylight");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    applyTheme("dracula");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    applyTheme("daylight");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
  });

  it("defaults an unknown theme to dark (safe fallback)", () => {
    // `Theme` is a closed union, so this branch is unreachable from any
    // type-checked call site — but the `meta?.dark ?? true` fallback is
    // intentional safety behavior. The cast forces the unknown-theme path so a
    // future edit (e.g. `?? true` → `?? false`) fails loudly here.
    document.documentElement.classList.remove("dark");
    applyTheme("nonexistent" as Theme);
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(document.documentElement.dataset.theme).toBe("nonexistent");
  });
});
