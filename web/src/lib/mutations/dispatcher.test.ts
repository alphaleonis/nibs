import { describe, it, expect, vi, beforeEach } from "vitest";
import { MutationDispatcher } from "./dispatcher";
import {
  createNib,
  updateNib,
  deleteNib,
  archiveNib,
  setParent,
  reorderNib,
  batch,
  sequence,
} from "./commands";
import type { CommandResult } from "./types";

// Mock svelte-sonner toast
const { mockToastError } = vi.hoisted(() => {
  return { mockToastError: vi.fn() };
});
vi.mock("svelte-sonner", async () => {
  const actual = await vi.importActual<typeof import("svelte-sonner")>("svelte-sonner");
  return {
    ...actual,
    toast: {
      ...actual.toast,
      error: mockToastError,
    },
  };
});

function createMockClient(result?: {
  data?: unknown;
  error?: { message: string; graphQLErrors?: { extensions?: { code?: string } }[] };
}) {
  const mockMutation = vi.fn().mockReturnValue({
    toPromise: vi.fn().mockResolvedValue(
      result ?? { data: { updateNib: { id: "nibs-abc1" } }, error: undefined }
    ),
  });
  return { mutation: mockMutation } as any;
}

describe("MutationDispatcher", () => {
  beforeEach(() => {
    mockToastError.mockReset();
  });

  describe("leaf command execution", () => {
    it("executes updateNib with correct mutation doc and variables", async () => {
      const client = createMockClient({ data: { updateNib: { id: "nibs-abc1" } } });
      const dispatcher = new MutationDispatcher(client);

      const result = await dispatcher.execute(updateNib("nibs-abc1", { title: "New" }, "etag-1"));

      expect(client.mutation).toHaveBeenCalledTimes(1);
      const [doc, vars] = client.mutation.mock.calls[0];
      expect(vars).toEqual({ id: "nibs-abc1", input: { title: "New", ifMatch: "etag-1" } });
      expect(result).toEqual({ ok: true, data: { updateNib: { id: "nibs-abc1" } } });
    });

    it("executes createNib with correct variables", async () => {
      const client = createMockClient({ data: { createNib: { id: "nibs-new1" } } });
      const dispatcher = new MutationDispatcher(client);

      const result = await dispatcher.execute(createNib({ title: "Task", type: "task" }));

      const [, vars] = client.mutation.mock.calls[0];
      expect(vars).toEqual({ input: { title: "Task", type: "task" } });
      expect(result.ok).toBe(true);
    });

    it("executes deleteNib with correct variables", async () => {
      const client = createMockClient({ data: { deleteNib: true } });
      const dispatcher = new MutationDispatcher(client);

      await dispatcher.execute(deleteNib("nibs-abc1"));

      const [, vars] = client.mutation.mock.calls[0];
      expect(vars).toEqual({ id: "nibs-abc1" });
    });

    it("executes archiveNib with correct variables", async () => {
      const client = createMockClient({ data: { archiveNib: true } });
      const dispatcher = new MutationDispatcher(client);

      await dispatcher.execute(archiveNib("nibs-abc1"));

      const [, vars] = client.mutation.mock.calls[0];
      expect(vars).toEqual({ id: "nibs-abc1" });
    });

    it("executes setParent with correct variables", async () => {
      const client = createMockClient({ data: { setParent: { id: "nibs-abc1" } } });
      const dispatcher = new MutationDispatcher(client);

      await dispatcher.execute(setParent("nibs-abc1", "nibs-parent1"));

      const [, vars] = client.mutation.mock.calls[0];
      expect(vars).toEqual({ id: "nibs-abc1", parentId: "nibs-parent1" });
    });

    it("executes reorderNib with correct variables", async () => {
      const client = createMockClient({ data: { reorderNib: { id: "nibs-abc1" } } });
      const dispatcher = new MutationDispatcher(client);

      await dispatcher.execute(reorderNib("nibs-abc1", { afterId: "nibs-xyz9" }));

      const [, vars] = client.mutation.mock.calls[0];
      expect(vars).toEqual({ id: "nibs-abc1", afterId: "nibs-xyz9" });
    });

    it("executes reorderNib with the ordering scope when one is given", async () => {
      const client = createMockClient({ data: { reorderNib: { id: "nibs-abc1" } } });
      const dispatcher = new MutationDispatcher(client);

      await dispatcher.execute(
        reorderNib("nibs-abc1", { afterId: "nibs-xyz9", scope: "MILESTONE" }),
      );

      const [, vars] = client.mutation.mock.calls[0];
      expect(vars).toEqual({ id: "nibs-abc1", afterId: "nibs-xyz9", scope: "MILESTONE" });
    });

    it("omits the scope KEY entirely when the command carries none", async () => {
      // What keeps every pre-existing sibling-order caller sending exactly what
      // it sent before. Key absence is the assertion because `toHaveProperty`
      // counts an explicitly-undefined own key as present; on the wire itself
      // undefined and absent are the same thing, since urql's serializer drops
      // the key. The value that is NOT the same is an explicit `null`: `scope`
      // is non-null on the schema, and gqlgen refuses a null at the boundary
      // (`OrderScope.UnmarshalGQL`, during argument coercion) rather than
      // falling back to PARENT. That refusal lands before the resolver body
      // runs, so a null never reaches nib lookup and writes nothing.
      const client = createMockClient({ data: { reorderNib: { id: "nibs-abc1" } } });
      const dispatcher = new MutationDispatcher(client);

      await dispatcher.execute(reorderNib("nibs-abc1", { afterId: "nibs-xyz9" }));

      const [, vars] = client.mutation.mock.calls[0];
      expect(vars).not.toHaveProperty("scope");
    });
  });

  describe("error handling", () => {
    it("toasts on error and returns failure result", async () => {
      const client = createMockClient({ error: { message: "Something went wrong" } });
      const dispatcher = new MutationDispatcher(client);

      const result = await dispatcher.execute(deleteNib("nibs-abc1"));

      expect(mockToastError).toHaveBeenCalledWith("Something went wrong");
      expect(result).toEqual({ ok: false, error: "Something went wrong" });
    });

    it("lifts the GraphQL extensions.code onto the failure result", async () => {
      const client = createMockClient({
        error: {
          message: "[GraphQL] etag mismatch: provided a, current is b",
          graphQLErrors: [{ extensions: { code: "ETAG_MISMATCH" } }],
        },
      });
      const dispatcher = new MutationDispatcher(client);

      const result = await dispatcher.execute(updateNib("nibs-abc1", { title: "x" }, "a"));

      expect(result.ok).toBe(false);
      expect(result.errorCode).toBe("ETAG_MISMATCH");
    });

    it("leaves errorCode undefined when the server tagged no code", async () => {
      const client = createMockClient({ error: { message: "disk full" } });
      const dispatcher = new MutationDispatcher(client);

      const result = await dispatcher.execute(updateNib("nibs-abc1", { title: "x" }));

      expect(result.errorCode).toBeUndefined();
    });
  });

  describe("toast suppression (suppressToast)", () => {
    it("toasts on error by default", async () => {
      const client = createMockClient({ error: { message: "boom" } });
      const dispatcher = new MutationDispatcher(client);

      const result = await dispatcher.execute(updateNib("nibs-abc1", { title: "x" }));

      expect(mockToastError).toHaveBeenCalledWith("boom");
      // The failure is still reported to the caller either way.
      expect(result).toMatchObject({ ok: false, error: "boom" });
    });

    it("suppresses the error toast when suppressToast is set (caller owns messaging)", async () => {
      const client = createMockClient({
        error: {
          message: "[GraphQL] etag mismatch: provided a, current is b",
          graphQLErrors: [{ extensions: { code: "ETAG_MISMATCH" } }],
        },
      });
      const dispatcher = new MutationDispatcher(client);

      const result = await dispatcher.execute(
        updateNib("nibs-abc1", { title: "x" }, "a"),
        { suppressToast: true },
      );

      // No raw toast races the caller's inline resolver...
      expect(mockToastError).not.toHaveBeenCalled();
      // ...but the failure + structured code are still returned so save() can route it.
      expect(result).toMatchObject({ ok: false, errorCode: "ETAG_MISMATCH" });
    });

    it("does not suppress toasts for other legs of a batch (opt-in is per-call, not global)", async () => {
      const client = createMockClient({ error: { message: "batch-boom" } });
      const dispatcher = new MutationDispatcher(client);

      // A batch WITHOUT the option toasts each failed leg as before.
      await dispatcher.execute(batch([deleteNib("a"), deleteNib("b")]));

      expect(mockToastError).toHaveBeenCalledTimes(2);
    });
  });

  describe("additionalTypenames", () => {
    it("passes additionalTypenames for delete-nib", async () => {
      const client = createMockClient({ data: { deleteNib: true } });
      const dispatcher = new MutationDispatcher(client);

      await dispatcher.execute(deleteNib("nibs-abc1"));

      const [, , opts] = client.mutation.mock.calls[0];
      expect(opts).toEqual({ additionalTypenames: ["Nib"] });
    });

    it("passes additionalTypenames for archive-nib", async () => {
      const client = createMockClient({ data: { archiveNib: true } });
      const dispatcher = new MutationDispatcher(client);

      await dispatcher.execute(archiveNib("nibs-abc1"));

      const [, , opts] = client.mutation.mock.calls[0];
      expect(opts).toEqual({ additionalTypenames: ["Nib"] });
    });

    it("passes additionalTypenames for set-parent", async () => {
      const client = createMockClient({ data: { setParent: { id: "nibs-abc1" } } });
      const dispatcher = new MutationDispatcher(client);

      await dispatcher.execute(setParent("nibs-abc1", "nibs-parent1"));

      const [, , opts] = client.mutation.mock.calls[0];
      expect(opts).toEqual({ additionalTypenames: ["Nib"] });
    });

    it("passes additionalTypenames for reorder-nib", async () => {
      const client = createMockClient({ data: { reorderNib: { id: "nibs-abc1" } } });
      const dispatcher = new MutationDispatcher(client);

      await dispatcher.execute(reorderNib("nibs-abc1", { afterId: "target" }));

      const [, , opts] = client.mutation.mock.calls[0];
      expect(opts).toEqual({ additionalTypenames: ["Nib"] });
    });

    it("does NOT pass additionalTypenames for update-nib", async () => {
      const client = createMockClient({ data: { updateNib: { id: "nibs-abc1" } } });
      const dispatcher = new MutationDispatcher(client);

      await dispatcher.execute(updateNib("nibs-abc1", { title: "x" }));

      const [, , opts] = client.mutation.mock.calls[0];
      expect(opts).toBeUndefined();
    });

    it("passes additionalTypenames for create-nib (new item invalidates list)", async () => {
      const client = createMockClient({ data: { createNib: { id: "nibs-new1" } } });
      const dispatcher = new MutationDispatcher(client);

      await dispatcher.execute(createNib({ title: "x", type: "task" }));

      const [, , opts] = client.mutation.mock.calls[0];
      expect(opts).toEqual({ additionalTypenames: ["Nib"] });
    });
  });

  describe("batch execution", () => {
    it("executes all commands concurrently and aggregates results", async () => {
      const client = createMockClient({ data: { deleteNib: true } });
      const dispatcher = new MutationDispatcher(client);

      const result = await dispatcher.execute(
        batch([deleteNib("a"), deleteNib("b"), deleteNib("c")])
      );

      expect(client.mutation).toHaveBeenCalledTimes(3);
      expect(result).toEqual({
        ok: true,
        results: [
          { ok: true, data: { deleteNib: true } },
          { ok: true, data: { deleteNib: true } },
          { ok: true, data: { deleteNib: true } },
        ],
        successes: 3,
        failures: 0,
      });
    });

    it("reports partial failure in batch results", async () => {
      let callCount = 0;
      const client = {
        mutation: vi.fn().mockImplementation(() => {
          callCount++;
          return {
            toPromise: vi.fn().mockResolvedValue(
              callCount === 2
                ? { error: { message: "fail" } }
                : { data: { deleteNib: true } }
            ),
          };
        }),
      } as any;
      const dispatcher = new MutationDispatcher(client);

      const result = await dispatcher.execute(
        batch([deleteNib("a"), deleteNib("b"), deleteNib("c")])
      );

      expect(result.ok).toBe(false);
      expect((result as any).successes).toBe(2);
      expect((result as any).failures).toBe(1);
    });
  });

  describe("sequence execution", () => {
    it("executes steps serially with chaining", async () => {
      const callOrder: string[] = [];
      const client = {
        mutation: vi.fn().mockImplementation((_doc: any, vars: any) => {
          callOrder.push(vars.id);
          return {
            toPromise: vi.fn().mockResolvedValue({
              data: { reorderNib: { id: vars.id } },
            }),
          };
        }),
      } as any;
      const dispatcher = new MutationDispatcher(client);

      const cmd = sequence([
        reorderNib("a", { afterId: "target" }),
        (prev: CommandResult) => reorderNib("b", { afterId: prev.data?.reorderNib?.id }),
      ]);

      const result = await dispatcher.execute(cmd);

      // Steps should execute in order
      expect(callOrder).toEqual(["a", "b"]);
      expect(result.ok).toBe(true);
      expect((result as any).results).toHaveLength(2);
    });

    it("stops on first failure in sequence", async () => {
      let callCount = 0;
      const client = {
        mutation: vi.fn().mockImplementation(() => {
          callCount++;
          return {
            toPromise: vi.fn().mockResolvedValue(
              callCount === 1
                ? { error: { message: "fail" } }
                : { data: { reorderNib: { id: "x" } } }
            ),
          };
        }),
      } as any;
      const dispatcher = new MutationDispatcher(client);

      const cmd = sequence([
        reorderNib("a", { afterId: "target" }),
        reorderNib("b", { afterId: "a" }),
      ]);

      const result = await dispatcher.execute(cmd);

      expect(result.ok).toBe(false);
      expect((result as any).stoppedAt).toBe(0);
      expect((result as any).results).toHaveLength(1);
      // Second step should NOT have been called
      expect(client.mutation).toHaveBeenCalledTimes(1);
    });
  });
});
