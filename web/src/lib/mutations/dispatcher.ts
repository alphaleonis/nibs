import { toast } from "svelte-sonner";
import type { Client, CombinedError } from "@urql/core";
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
  ExecuteOptions,
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

/**
 * Lift the first string `extensions.code` off a urql CombinedError's GraphQL
 * errors (e.g. "ETAG_MISMATCH", set by the backend error presenter). Returns
 * undefined when no GraphQL error carried a string code — the caller then falls
 * back to substring-matching the message.
 */
function errorCodeOf(error: CombinedError): string | undefined {
  for (const gqlErr of error.graphQLErrors ?? []) {
    const code = gqlErr.extensions?.code;
    if (typeof code === "string") return code;
  }
  return undefined;
}

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

  async execute(cmd: LeafCommand, opts?: ExecuteOptions): Promise<CommandResult>;
  async execute(cmd: BatchCommand, opts?: ExecuteOptions): Promise<BatchResult>;
  async execute(cmd: SequenceCommand, opts?: ExecuteOptions): Promise<SequenceResult>;
  // Union overload so callers holding an un-narrowed AnyCommand (e.g. the
  // MutationStore pass-through) can dispatch without picking a concrete kind.
  async execute(cmd: AnyCommand, opts?: ExecuteOptions): Promise<AnyResult>;
  async execute(cmd: AnyCommand, opts?: ExecuteOptions): Promise<AnyResult> {
    // suppressToast is threaded into every leaf (including a batch/sequence's
    // legs) but only ever set by the opted-in caller — default false keeps the
    // "toast on error" behavior for everyone else.
    const suppressToast = opts?.suppressToast ?? false;
    switch (cmd.kind) {
      case "batch":
        return this.#executeBatch(cmd.commands, suppressToast);
      case "sequence":
        return this.#executeSequence(cmd.steps, suppressToast);
      default:
        return this.#executeLeaf(cmd, suppressToast);
    }
  }

  async #executeLeaf(cmd: LeafCommand, suppressToast = false): Promise<CommandResult> {
    const doc = getMutationDoc(cmd.kind);
    const vars = getVariables(cmd);
    const opts = INVALIDATING_KINDS.has(cmd.kind)
      ? { additionalTypenames: ["Nib"] }
      : undefined;

    const res = await this.#client.mutation(doc, vars, opts).toPromise();

    if (res.error) {
      // The caller can opt to OWN the messaging for this call (e.g. save()
      // routing a 409 into the inline resolver); otherwise toast the error here.
      if (!suppressToast) toast.error(res.error.message);
      return { ok: false, error: res.error.message, errorCode: errorCodeOf(res.error) };
    }
    return { ok: true, data: res.data };
  }

  async #executeBatch(commands: LeafCommand[], suppressToast = false): Promise<BatchResult> {
    const results = await Promise.allSettled(
      commands.map((cmd) => this.#executeLeaf(cmd, suppressToast))
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

  async #executeSequence(steps: SequenceStep[], suppressToast = false): Promise<SequenceResult> {
    const results: CommandResult[] = [];
    let prevResult: CommandResult = { ok: true };

    for (let i = 0; i < steps.length; i++) {
      const step = steps[i];
      const cmd = typeof step === "function" ? step(prevResult) : step;
      const result = await this.#executeLeaf(cmd, suppressToast);
      results.push(result);

      if (!result.ok) {
        return { ok: false, results, stoppedAt: i };
      }
      prevResult = result;
    }

    return { ok: true, results };
  }
}
