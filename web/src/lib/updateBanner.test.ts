import { describe, it, expect, beforeEach, vi } from "vitest";
import { isUpdateDismissed, dismissUpdate, UPDATE_DISMISS_KEY } from "./updateBanner";

const store: Record<string, string> = {};
const mockStorage = {
  getItem: vi.fn((k: string) => store[k] ?? null),
  setItem: vi.fn((k: string, v: string) => { store[k] = v; }),
  removeItem: vi.fn((k: string) => { delete store[k]; }),
};
Object.defineProperty(globalThis, "localStorage", { value: mockStorage, writable: true });

describe("updateBanner dismissal", () => {
  beforeEach(() => { for (const k in store) delete store[k]; });

  it("is not dismissed by default", () => {
    expect(isUpdateDismissed("v0.6.0")).toBe(false);
  });

  it("dismiss persists for that exact version", () => {
    dismissUpdate("v0.6.0");
    expect(isUpdateDismissed("v0.6.0")).toBe(true);
    expect(store[UPDATE_DISMISS_KEY]).toBe("v0.6.0");
  });

  it("dismissing one version does not suppress a newer one", () => {
    dismissUpdate("v0.6.0");
    expect(isUpdateDismissed("v0.7.0")).toBe(false);
  });

  it("empty version is never stored and never dismissed", () => {
    dismissUpdate("");
    expect(store[UPDATE_DISMISS_KEY]).toBeUndefined();
    expect(isUpdateDismissed("")).toBe(false);
  });
});
