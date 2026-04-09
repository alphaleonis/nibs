import { describe, it, expect, vi, afterEach } from "vitest";
import {
  bindShortcuts,
  bindGlobalShortcuts,
  getRegisteredShortcuts,
  shortcuts,
} from "./keyboard";

describe("keyboard", () => {
  // Clean up registry between tests by tracking unbind functions
  const cleanups: (() => void)[] = [];
  afterEach(() => {
    for (const cleanup of cleanups) cleanup();
    cleanups.length = 0;
  });

  describe("getRegisteredShortcuts", () => {
    it("returns empty array when no shortcuts are registered", () => {
      expect(getRegisteredShortcuts()).toEqual([]);
    });

    it("returns registered shortcuts with descriptions", () => {
      const el = document.createElement("div");
      const unbind = bindShortcuts(
        el,
        { Escape: () => {}, "$mod+k": () => {} },
        { Escape: "Close panel", "$mod+k": "Open search" },
      );
      cleanups.push(unbind);

      const registered = getRegisteredShortcuts();
      expect(registered).toHaveLength(2);
      expect(registered).toContainEqual({
        combo: "Escape",
        description: "Close panel",
      });
      expect(registered).toContainEqual({
        combo: "$mod+k",
        description: "Open search",
      });
    });

    it("does not register shortcuts without descriptions", () => {
      const el = document.createElement("div");
      const unbind = bindShortcuts(el, { Escape: () => {} });
      cleanups.push(unbind);

      expect(getRegisteredShortcuts()).toEqual([]);
    });
  });

  describe("bindShortcuts", () => {
    it("returns an unsubscribe function", () => {
      const el = document.createElement("div");
      const unbind = bindShortcuts(el, { Escape: () => {} });
      expect(typeof unbind).toBe("function");
      unbind();
    });

    it("removes registry entries on unsubscribe", () => {
      const el = document.createElement("div");
      const unbind = bindShortcuts(
        el,
        { Escape: () => {} },
        { Escape: "Close panel" },
      );

      expect(getRegisteredShortcuts()).toHaveLength(1);
      unbind();
      expect(getRegisteredShortcuts()).toEqual([]);
    });

    it("only removes its own registry entries on unsubscribe", () => {
      const el = document.createElement("div");
      const unbind1 = bindShortcuts(
        el,
        { Escape: () => {} },
        { Escape: "Close panel" },
      );
      cleanups.push(unbind1);

      const unbind2 = bindShortcuts(
        el,
        { "$mod+k": () => {} },
        { "$mod+k": "Open search" },
      );

      expect(getRegisteredShortcuts()).toHaveLength(2);
      unbind2();
      expect(getRegisteredShortcuts()).toHaveLength(1);
      expect(getRegisteredShortcuts()[0].combo).toBe("Escape");
    });

    it("handles multiple registrations of the same combo without collision", () => {
      const el = document.createElement("div");
      const unbind1 = bindShortcuts(
        el,
        { Escape: () => {} },
        { Escape: "Close panel" },
      );
      cleanups.push(unbind1);

      const unbind2 = bindShortcuts(
        el,
        { Escape: () => {} },
        { Escape: "Close dialog" },
      );
      cleanups.push(unbind2);

      // Both descriptions should be registered
      const registered = getRegisteredShortcuts();
      expect(registered).toHaveLength(2);
      expect(registered).toContainEqual({
        combo: "Escape",
        description: "Close panel",
      });
      expect(registered).toContainEqual({
        combo: "Escape",
        description: "Close dialog",
      });

      // Unsubscribing one should leave the other intact
      unbind2();
      const remaining = getRegisteredShortcuts();
      expect(remaining).toHaveLength(1);
      expect(remaining[0]).toEqual({
        combo: "Escape",
        description: "Close panel",
      });
    });

    it("invokes handler when key is pressed", () => {
      const el = document.createElement("div");
      document.body.appendChild(el);
      try {
        const handler = vi.fn();
        const unbind = bindShortcuts(el, { Escape: handler });
        cleanups.push(unbind);

        el.dispatchEvent(
          new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
        );
        expect(handler).toHaveBeenCalledOnce();
      } finally {
        document.body.removeChild(el);
      }
    });
  });

  describe("bindGlobalShortcuts", () => {
    it("binds to window and returns unsubscribe function", () => {
      const unbind = bindGlobalShortcuts(
        { Escape: () => {} },
        { Escape: "Close panel" },
      );

      expect(getRegisteredShortcuts()).toHaveLength(1);
      unbind();
      expect(getRegisteredShortcuts()).toEqual([]);
    });
  });

  describe("shortcuts action", () => {
    it("returns an object with destroy method", () => {
      const el = document.createElement("div");
      const result = shortcuts(el, { Escape: () => {} });
      expect(typeof result.destroy).toBe("function");
      result.destroy();
    });
  });
});
