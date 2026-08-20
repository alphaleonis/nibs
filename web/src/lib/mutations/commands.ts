import type {
  CreateNibInput,
  UpdateNibInput,
  CreateNibCommand,
  UpdateNibCommand,
  DeleteNibCommand,
  ArchiveNibCommand,
  SetParentCommand,
  ReorderNibCommand,
  CommandResult,
  LeafCommand,
  BatchCommand,
  SequenceCommand,
  SequenceStep,
} from "./types";

// --- Leaf factories ---

export function createNib(input: CreateNibInput): CreateNibCommand {
  return { kind: "create-nib", input };
}

export function updateNib(id: string, input: UpdateNibInput, ifMatch?: string): UpdateNibCommand {
  const cmd: UpdateNibCommand = { kind: "update-nib", id, input };
  if (ifMatch !== undefined) {
    cmd.ifMatch = ifMatch;
  }
  return cmd;
}

export function deleteNib(id: string): DeleteNibCommand {
  return { kind: "delete-nib", id };
}

export function archiveNib(id: string): ArchiveNibCommand {
  return { kind: "archive-nib", id };
}

export function setParent(id: string, parentId: string | null): SetParentCommand {
  return { kind: "set-parent", id, parentId };
}

export function reorderNib(
  id: string,
  opts: { afterId?: string; beforeId?: string; first?: boolean; parentId?: string | null },
): ReorderNibCommand {
  const cmd: ReorderNibCommand = { kind: "reorder-nib", id };
  if (opts.afterId !== undefined) cmd.afterId = opts.afterId;
  if (opts.beforeId !== undefined) cmd.beforeId = opts.beforeId;
  if (opts.first !== undefined) cmd.first = opts.first;
  // Use "" for root-level (null → ""), since GraphQL null is indistinguishable
  // from "not provided" in the Go resolver (*string nil for both cases).
  if (opts.parentId !== undefined) cmd.parentId = opts.parentId ?? "";
  return cmd;
}

// --- Composition factories ---

export function batch(commands: LeafCommand[]): BatchCommand {
  return { kind: "batch", commands };
}

export function sequence(steps: SequenceStep[]): SequenceCommand {
  return { kind: "sequence", steps };
}

// --- Domain-level compositions ---

export function reorderChain(
  ids: string[],
  targetId: string,
  zone: "before" | "after",
): SequenceCommand {
  const steps: SequenceStep[] = ids.map((id, i) => {
    if (i === 0) {
      return zone === "before"
        ? reorderNib(id, { beforeId: targetId })
        : reorderNib(id, { afterId: targetId });
    }
    // Subsequent items chain afterId from the previous result's id
    return (prev: CommandResult) => reorderNib(id, { afterId: prev.data?.reorderNib?.id });
  });
  return sequence(steps);
}

/**
 * Reparent items and reorder them relative to a target sibling.
 * Each reorderNib call includes parentId for an atomic reparent+reorder.
 */
export function reparentAndReorder(
  ids: string[],
  newParentId: string | null,
  targetId: string,
  zone: "before" | "after",
): SequenceCommand {
  const steps: SequenceStep[] = ids.map((id, i) => {
    if (i === 0) {
      return zone === "before"
        ? reorderNib(id, { beforeId: targetId, parentId: newParentId })
        : reorderNib(id, { afterId: targetId, parentId: newParentId });
    }
    return (prev: CommandResult) =>
      reorderNib(id, { afterId: prev.data?.reorderNib?.id, parentId: newParentId });
  });
  return sequence(steps);
}

export function deleteBatch(ids: string[]): BatchCommand {
  return batch(ids.map((id) => deleteNib(id)));
}

export function archiveBatch(ids: string[]): BatchCommand {
  return batch(ids.map((id) => archiveNib(id)));
}

/** Resolves a nib's current etag, or undefined when the caller has not loaded
 *  one for it. Undefined means "send no ifMatch": an absent guard is an
 *  unguarded write, while a made-up one is a write that can only fail. */
export type EtagResolver = (id: string) => string | undefined;

export function setStatusBatch(
  ids: string[],
  status: string,
  etagOf?: EtagResolver,
): BatchCommand {
  return batch(ids.map((id) => updateNib(id, { status }, etagOf?.(id))));
}

export function setPriorityBatch(
  ids: string[],
  priority: string,
  etagOf?: EtagResolver,
): BatchCommand {
  return batch(ids.map((id) => updateNib(id, { priority }, etagOf?.(id))));
}

export function reparentBatch(ids: string[], parentId: string): BatchCommand {
  return batch(ids.map((id) => setParent(id, parentId)));
}
