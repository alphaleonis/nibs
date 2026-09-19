import type {
  CreateNibInput,
  UpdateNibInput,
  CreateNibCommand,
  UpdateNibCommand,
  DeleteNibCommand,
  ArchiveNibCommand,
  SetParentCommand,
  ReorderNibCommand,
  AddAreaCommand,
  UpdateAreaCommand,
  RemoveAreaCommand,
  AreaDisposition,
  CommandResult,
  LeafCommand,
  BatchCommand,
  SequenceCommand,
  SequenceStep,
} from "./types";
import type { OrderScope } from "../gql/graphql";

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

/**
 * Position a nib on one ordering axis; an omitted `scope` is PARENT on the
 * server. The server refuses `parentId` with `scope: MILESTONE`, and
 * `reparentAndReorder` always sends `parentId`, so a queue move must not use it.
 */
export function reorderNib(
  id: string,
  opts: { afterId?: string; beforeId?: string; first?: boolean; parentId?: string | null; scope?: OrderScope },
): ReorderNibCommand {
  const cmd: ReorderNibCommand = { kind: "reorder-nib", id };
  if (opts.afterId !== undefined) cmd.afterId = opts.afterId;
  if (opts.beforeId !== undefined) cmd.beforeId = opts.beforeId;
  if (opts.first !== undefined) cmd.first = opts.first;
  if (opts.scope !== undefined) cmd.scope = opts.scope;
  // Root level is "": the Go resolver cannot tell null from omitted.
  if (opts.parentId !== undefined) cmd.parentId = opts.parentId ?? "";
  return cmd;
}

/**
 * Declare one area. `path` is the FULL path — an undeclared parent is refused
 * rather than created along the way.
 *
 * An option the caller did not set stays ABSENT from the command, because the
 * server reads "" as "clear this key" rather than as "unset". Passing
 * `description: ""` deliberately is therefore still possible and still means
 * something different from omitting it.
 */
export function addArea(path: string, opts?: { description?: string; color?: string }): AddAreaCommand {
  const cmd: AddAreaCommand = { kind: "add-area", path };
  if (opts?.description !== undefined) cmd.description = opts.description;
  if (opts?.color !== undefined) cmd.color = opts.color;
  return cmd;
}

/**
 * Edit one declared area's name, description, color, or any combination.
 *
 * A field the caller left out stays ABSENT from the command, which is what keeps
 * a rename from clearing a description nobody touched: the server reads a key
 * sent as "" as one someone emptied. Passing "" deliberately still clears.
 */
export function updateArea(
  path: string,
  edit: { newName?: string; description?: string; color?: string },
): UpdateAreaCommand {
  const cmd: UpdateAreaCommand = { kind: "update-area", path };
  if (edit.newName !== undefined) cmd.newName = edit.newName;
  if (edit.description !== undefined) cmd.description = edit.description;
  if (edit.color !== undefined) cmd.color = edit.color;
  return cmd;
}

/**
 * Retire one declared area and its whole subtree.
 *
 * The disposition is a union, so `moveTo` and `unassign` cannot both be named.
 * Omitting it sends neither key, which is what an area with no members needs:
 * the server refuses a disposition naming an area nothing is assigned to.
 */
export function removeArea(path: string, disposition?: AreaDisposition): RemoveAreaCommand {
  const cmd: RemoveAreaCommand = { kind: "remove-area", path };
  if (disposition === undefined) return cmd;
  if ("unassign" in disposition) cmd.unassign = true;
  else cmd.moveTo = disposition.moveTo;
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

/**
 * Chain nibs after a target on the PARENT axis. Not for a queue move: with a
 * shared parent it silently reorders the sibling `order` key instead, and with
 * different parents the server refuses it (`not a sibling (different parent)`).
 */
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

/** A nib's loaded etag, or undefined to send no ifMatch. Never invent one. */
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

/**
 * Assign nibs to one milestone, or clear with "". Carries no position, so each
 * row takes the queue's default placement.
 */
export function setMilestoneBatch(
  ids: string[],
  milestone: string,
  etagOf?: EtagResolver,
): BatchCommand {
  return batch(ids.map((id) => updateNib(id, { milestone: milestone || null }, etagOf?.(id))));
}

export function reparentBatch(ids: string[], parentId: string): BatchCommand {
  return batch(ids.map((id) => setParent(id, parentId)));
}
