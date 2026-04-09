import { describe, it, expect, vi, beforeEach } from "vitest";
import { MutationStore } from "./store.svelte";
import { MutationDispatcher } from "./dispatcher";
import { deleteNib, updateNib, batch, sequence, reorderNib, setParent, createNib } from "./commands";
import type { CommandResult, BatchResult, SequenceResult } from "./types";

// Mock svelte-sonner toast (dispatcher imports it)
vi.mock("svelte-sonner", async () => {
  const actual = await vi.importActual<typeof import("svelte-sonner")>("svelte-sonner");
  return {
    ...actual,
    toast: { ...actual.toast, error: vi.fn() },
  };
});

function createMockClient(result?: { data?: unknown; error?: { message: string } }) {
  const mockMutation = vi.fn().mockReturnValue({
    toPromise: vi.fn().mockResolvedValue(
      result ?? { data: { updateNib: { id: "nibs-abc1" } }, error: undefined }
    ),
  });
  return { mutation: mockMutation } as any;
}

describe("MutationStore", () => {
  it("delegates to dispatcher and returns the result", async () => {
    const client = createMockClient({ data: { deleteNib: true } });
    const dispatcher = new MutationDispatcher(client);
    const store = new MutationStore(dispatcher);

    const result = await store.execute(deleteNib("nibs-abc1"));

    expect(result.ok).toBe(true);
    expect(client.mutation).toHaveBeenCalledTimes(1);
  });

  it("extracts affected ID from leaf command", async () => {
    // Use a deferred promise to control timing
    let resolvePromise!: (value: any) => void;
    const client = {
      mutation: vi.fn().mockReturnValue({
        toPromise: () => new Promise((resolve) => { resolvePromise = resolve; }),
      }),
    } as any;
    const dispatcher = new MutationDispatcher(client);
    const store = new MutationStore(dispatcher);

    const promise = store.execute(deleteNib("nibs-abc1"));

    // During execution, the ID should be in-flight
    expect(store.isMutating("nibs-abc1")).toBe(true);
    expect(store.pending).toBe(true);

    // Resolve the mutation
    resolvePromise({ data: { deleteNib: true } });
    await promise;

    // After completion, ID should no longer be in-flight
    expect(store.isMutating("nibs-abc1")).toBe(false);
    expect(store.pending).toBe(false);
  });

  it("extracts affected IDs from batch command", async () => {
    let resolves: Array<(v: any) => void> = [];
    const client = {
      mutation: vi.fn().mockImplementation(() => ({
        toPromise: () => new Promise((resolve) => { resolves.push(resolve); }),
      })),
    } as any;
    const dispatcher = new MutationDispatcher(client);
    const store = new MutationStore(dispatcher);

    const promise = store.execute(batch([deleteNib("a"), deleteNib("b")]));

    expect(store.isMutating("a")).toBe(true);
    expect(store.isMutating("b")).toBe(true);
    expect(store.pending).toBe(true);

    // Resolve all
    resolves.forEach((r) => r({ data: { deleteNib: true } }));
    await promise;

    expect(store.isMutating("a")).toBe(false);
    expect(store.isMutating("b")).toBe(false);
    expect(store.pending).toBe(false);
  });

  it("extracts affected IDs from sequence command (static steps)", async () => {
    const client = createMockClient({ data: { reorderNib: { id: "a" } } });
    const dispatcher = new MutationDispatcher(client);
    const store = new MutationStore(dispatcher);

    const cmd = sequence([
      reorderNib("a", { afterId: "target" }),
      reorderNib("b", { afterId: "a" }),
    ]);
    const promise = store.execute(cmd);

    // Both IDs should be tracked as in-flight immediately
    expect(store.isMutating("a")).toBe(true);
    expect(store.isMutating("b")).toBe(true);

    await promise;

    expect(store.isMutating("a")).toBe(false);
    expect(store.isMutating("b")).toBe(false);
  });

  it("cleans up in-flight IDs even on error", async () => {
    const client = createMockClient({ error: { message: "fail" } });
    const dispatcher = new MutationDispatcher(client);
    const store = new MutationStore(dispatcher);

    await store.execute(deleteNib("nibs-abc1"));

    expect(store.isMutating("nibs-abc1")).toBe(false);
    expect(store.pending).toBe(false);
  });

  it("does not track IDs for create-nib (no known id yet)", async () => {
    const client = createMockClient({ data: { createNib: { id: "nibs-new1" } } });
    const dispatcher = new MutationDispatcher(client);
    const store = new MutationStore(dispatcher);

    const promise = store.execute(createNib({ title: "x", type: "task" }));
    // create-nib has no id field, so no specific ID should be tracked
    expect(store.pending).toBe(true);

    await promise;
    expect(store.pending).toBe(false);
  });
});
