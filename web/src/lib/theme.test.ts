import { describe, it, expect, afterEach } from "vitest";
import { applyTheme } from "./theme";

describe("applyTheme", () => {
  afterEach(() => {
    delete document.documentElement.dataset.theme;
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
});
