import { setContext, getContext } from "svelte";
import type { Client } from "@urql/core";
import { MutationDispatcher } from "./dispatcher";
import type { AnyCommand, AnyResult, LeafCommand, BatchCommand, SequenceCommand, CommandResult, BatchResult, SequenceResult, ExecuteOptions } from "./types";

const MUTATION_STORE_KEY = "nibs:mutations";

/** Extract all known nib IDs from a command. */
function extractIds(cmd: AnyCommand): string[] {
  switch (cmd.kind) {
    case "create-nib":
      // No ID known until the mutation completes
      return [];
    case "update-nib":
    case "delete-nib":
    case "archive-nib":
    case "set-parent":
    case "reorder-nib":
      return [cmd.id];
    case "batch":
      return cmd.commands.flatMap((c) => extractIds(c));
    case "sequence":
      return cmd.steps.flatMap((step) => {
        if (typeof step === "function") return [];
        return extractIds(step);
      });
  }
}

export class MutationStore {
  #dispatcher: MutationDispatcher;
  #inflight = $state(new Set<string>());
  #pendingCount = $state(0);

  constructor(dispatcher: MutationDispatcher) {
    this.#dispatcher = dispatcher;
  }

  get pending(): boolean {
    return this.#pendingCount > 0;
  }

  isMutating(id: string): boolean {
    return this.#inflight.has(id);
  }

  async execute(cmd: LeafCommand, opts?: ExecuteOptions): Promise<CommandResult>;
  async execute(cmd: BatchCommand, opts?: ExecuteOptions): Promise<BatchResult>;
  async execute(cmd: SequenceCommand, opts?: ExecuteOptions): Promise<SequenceResult>;
  async execute(cmd: AnyCommand, opts?: ExecuteOptions): Promise<AnyResult> {
    const ids = extractIds(cmd);

    // Add IDs to in-flight set
    for (const id of ids) {
      this.#inflight.add(id);
    }
    // Reassign to trigger reactivity
    this.#inflight = new Set(this.#inflight);
    this.#pendingCount++;

    try {
      return await this.#dispatcher.execute(cmd, opts);
    } finally {
      // Remove IDs from in-flight set
      for (const id of ids) {
        this.#inflight.delete(id);
      }
      // Reassign to trigger reactivity (Svelte 5 $state tracks by reference)
      this.#inflight = new Set(this.#inflight);
      this.#pendingCount--;
    }
  }
}

/** Create a MutationStore, wire it to the urql Client, and set it in Svelte context. */
export function initMutationStore(client: Client): MutationStore {
  const dispatcher = new MutationDispatcher(client);
  const store = new MutationStore(dispatcher);
  setContext(MUTATION_STORE_KEY, store);
  return store;
}

/** Retrieve the MutationStore from Svelte context. */
export function getMutationStore(): MutationStore {
  const store = getContext<MutationStore>(MUTATION_STORE_KEY);
  if (!store) {
    throw new Error("getMutationStore() called outside provider — call initMutationStore() in a parent component");
  }
  return store;
}
