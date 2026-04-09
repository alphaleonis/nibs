import { toast } from "svelte-sonner";
import type { Client } from "@urql/core";
import {
  UPDATE_NIB_MUTATION,
  DELETE_NIB_MUTATION,
  ARCHIVE_NIB_MUTATION,
  CREATE_NIB_MUTATION,
  SET_PARENT_MUTATION,
  REORDER_NIB_MUTATION,
} from "../queries";
import type {
  AnyCommand,
  AnyResult,
  LeafCommand,
  BatchCommand,
  SequenceCommand,
  CommandResult,
  BatchResult,
  SequenceResult,
  SequenceStep,
} from "./types";

/** Maps command kind to the GraphQL mutation document. */
function getMutationDoc(kind: LeafCommand["kind"]) {
  switch (kind) {
    case "create-nib": return CREATE_NIB_MUTATION;
    case "update-nib": return UPDATE_NIB_MUTATION;
    case "delete-nib": return DELETE_NIB_MUTATION;
    case "archive-nib": return ARCHIVE_NIB_MUTATION;
    case "set-parent": return SET_PARENT_MUTATION;
    case "reorder-nib": return REORDER_NIB_MUTATION;
  }
}

/** Kinds that need cache invalidation via additionalTypenames. */
const INVALIDATING_KINDS = new Set<string>(["create-nib", "delete-nib", "archive-nib", "set-parent", "reorder-nib"]);

/** Maps a leaf command to the GraphQL variables. */
function getVariables(cmd: LeafCommand): Record<string, unknown> {
  switch (cmd.kind) {
    case "create-nib":
      return { input: cmd.input };
    case "update-nib": {
      const input: Record<string, unknown> = { ...cmd.input };
      if (cmd.ifMatch !== undefined) {
        input.ifMatch = cmd.ifMatch;
      }
      return { id: cmd.id, input };
    }
    case "delete-nib":
      return { id: cmd.id };
    case "archive-nib":
      return { id: cmd.id };
    case "set-parent":
      return { id: cmd.id, parentId: cmd.parentId };
    case "reorder-nib": {
      const vars: Record<string, unknown> = { id: cmd.id };
      if (cmd.afterId !== undefined) vars.afterId = cmd.afterId;
      if (cmd.beforeId !== undefined) vars.beforeId = cmd.beforeId;
      if (cmd.first !== undefined) vars.first = cmd.first;
      if (cmd.parentId !== undefined) vars.parentId = cmd.parentId;
      return vars;
    }
  }
}

export class MutationDispatcher {
  #client: Client;

  constructor(client: Client) {
    this.#client = client;
  }

  async execute(cmd: LeafCommand): Promise<CommandResult>;
  async execute(cmd: BatchCommand): Promise<BatchResult>;
  async execute(cmd: SequenceCommand): Promise<SequenceResult>;
  async execute(cmd: AnyCommand): Promise<AnyResult> {
    switch (cmd.kind) {
      case "batch":
        return this.#executeBatch(cmd.commands);
      case "sequence":
        return this.#executeSequence(cmd.steps);
      default:
        return this.#executeLeaf(cmd);
    }
  }

  async #executeLeaf(cmd: LeafCommand): Promise<CommandResult> {
    const doc = getMutationDoc(cmd.kind);
    const vars = getVariables(cmd);
    const opts = INVALIDATING_KINDS.has(cmd.kind)
      ? { additionalTypenames: ["Nib"] }
      : undefined;

    const res = await this.#client.mutation(doc, vars, opts).toPromise();

    if (res.error) {
      toast.error(res.error.message);
      return { ok: false, error: res.error.message };
    }
    return { ok: true, data: res.data };
  }

  async #executeBatch(commands: LeafCommand[]): Promise<BatchResult> {
    const results = await Promise.allSettled(
      commands.map((cmd) => this.#executeLeaf(cmd))
    );

    const commandResults: CommandResult[] = results.map((r) =>
      r.status === "fulfilled" ? r.value : { ok: false, error: String(r.reason) }
    );

    const successes = commandResults.filter((r) => r.ok).length;
    const failures = commandResults.length - successes;

    return {
      ok: failures === 0,
      results: commandResults,
      successes,
      failures,
    };
  }

  async #executeSequence(steps: SequenceStep[]): Promise<SequenceResult> {
    const results: CommandResult[] = [];
    let prevResult: CommandResult = { ok: true };

    for (let i = 0; i < steps.length; i++) {
      const step = steps[i];
      const cmd = typeof step === "function" ? step(prevResult) : step;
      const result = await this.#executeLeaf(cmd);
      results.push(result);

      if (!result.ok) {
        return { ok: false, results, stoppedAt: i };
      }
      prevResult = result;
    }

    return { ok: true, results };
  }
}
