import { render, screen, fireEvent } from "@testing-library/svelte";
import { readable } from "svelte/store";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { UPDATE_DISMISS_KEY } from "$lib/updateBanner";

vi.mock("@urql/svelte", async () => {
  const actual = await vi.importActual<typeof import("@urql/svelte")>("@urql/svelte");
  return { ...actual, getContextClient: vi.fn(), queryStore: vi.fn() };
});

import { queryStore } from "@urql/svelte";
import UpdateBanner from "./UpdateBanner.svelte";

const mockQueryStore = vi.mocked(queryStore);

const store: Record<string, string> = {};
const mockStorage = {
  getItem: vi.fn((k: string) => store[k] ?? null),
  setItem: vi.fn((k: string, v: string) => { store[k] = v; }),
  removeItem: vi.fn((k: string) => { delete store[k]; }),
};
Object.defineProperty(globalThis, "localStorage", { value: mockStorage, writable: true });

type Status = { current: string; latest: string; updateAvailable: boolean };

function mockStatus(status: Status | undefined) {
  mockQueryStore.mockReturnValue(
    readable({
      fetching: false,
      error: undefined,
      data: status ? { updateStatus: status } : undefined,
      stale: false,
    }) as any,
  );
}

describe("UpdateBanner", () => {
  beforeEach(() => {
    mockQueryStore.mockReset();
    for (const k in store) delete store[k];
  });

  it("shows the banner and version when an update is available", () => {
    mockStatus({ current: "v0.5.0", latest: "v0.6.0", updateAvailable: true });
    render(UpdateBanner);
    expect(screen.getByTestId("update-banner")).toBeInTheDocument();
    expect(screen.getByText(/v0\.6\.0/)).toBeInTheDocument();
    expect(screen.getByText(/nibs upgrade/)).toBeInTheDocument();
  });

  it("stays hidden when up to date", () => {
    mockStatus({ current: "v0.6.0", latest: "v0.6.0", updateAvailable: false });
    render(UpdateBanner);
    expect(screen.queryByTestId("update-banner")).not.toBeInTheDocument();
  });

  it("stays hidden while the status is still loading", () => {
    mockStatus(undefined);
    render(UpdateBanner);
    expect(screen.queryByTestId("update-banner")).not.toBeInTheDocument();
  });

  it("stays hidden when already dismissed for that version", () => {
    localStorage.setItem(UPDATE_DISMISS_KEY, "v0.6.0");
    mockStatus({ current: "v0.5.0", latest: "v0.6.0", updateAvailable: true });
    render(UpdateBanner);
    expect(screen.queryByTestId("update-banner")).not.toBeInTheDocument();
  });

  it("dismiss hides the banner and persists the version", async () => {
    mockStatus({ current: "v0.5.0", latest: "v0.6.0", updateAvailable: true });
    render(UpdateBanner);

    await fireEvent.click(screen.getByTestId("update-banner-dismiss"));

    expect(screen.queryByTestId("update-banner")).not.toBeInTheDocument();
    expect(localStorage.getItem(UPDATE_DISMISS_KEY)).toBe("v0.6.0");
  });
});
