import type { OrderScope, UpdateNibInput as GeneratedUpdateNibInput } from "../gql/graphql";

// --- Leaf command types ---

export type CreateNibInput = {
  title: string;
  type?: string;
  status?: string;
  priority?: string;
  estimate?: string;
  tags?: string[];
  body?: string;
  parent?: string;
  blocking?: string[];
  blockedBy?: string[];
  documents?: string[];
  prefix?: string;
  afterId?: string;
  beforeId?: string;
  first?: boolean;
};

export type UpdateNibInput = {
  title?: string;
  status?: string;
  type?: string;
  priority?: string | null;
  estimate?: string | null;
  tags?: string[];
  addTags?: string[];
  removeTags?: string[];
  body?: string;
  bodyMod?: { replace?: { old: string; new: string }[]; append?: string };
  parent?: string | null;
  /** The scheduling axis: the milestone whose queue the nib joins. Null or ""
   *  clears the assignment; omitted leaves it unchanged. Distinct from `parent`
   *  — a milestone accepts no children, so joining its queue is this write. */
  milestone?: string | null;
  /** The ownership axis: the declared area path the nib belongs to. Null or ""
   *  clears the assignment; omitted leaves it unchanged. Unlike `milestone` it
   *  names no nib, so nothing is resolved and no queue moves. */
  area?: string | null;
  addBlocking?: string[];
  removeBlocking?: string[];
  addBlockedBy?: string[];
  removeBlockedBy?: string[];
  documents?: string[];
  addDocuments?: string[];
  removeDocuments?: string[];
};

// Compile-time guard binding the hand-written UpdateNibInput above to the
// codegen'd one, so the two key sets cannot drift — the same pair, and for the
// same reason, as NibFilter's in ../types.ts.
//
// The input reaches the wire as a variable rather than an object literal, so
// TypeScript's excess-property check never runs on it; and `assignmentFor` in
// ordering/dropPlan.ts builds one with a COMPUTED key, which is not checked at
// all. Without this pair a client-side key the server has no argument for
// type-checks, ships, and is silently ignored.
//
// BOTH directions are required: a one-way `extends` is satisfied by extra
// properties, so it would miss exactly that case.
//
// `ifMatch` is the one deliberate difference, and it is excluded on the
// generated side rather than added here: it is command-level in this layer —
// UpdateNibCommand carries it beside `input`, and the dispatcher merges the two.
type GeneratedUpdateKeys = Exclude<keyof GeneratedUpdateNibInput, "ifMatch">;

type _UpdateKeysExistOnGenerated = keyof UpdateNibInput extends GeneratedUpdateKeys ? true : never;
const _updateKeysCheck: _UpdateKeysExistOnGenerated = true;
void _updateKeysCheck;

type _GeneratedUpdateKeysExistOnClient = GeneratedUpdateKeys extends keyof UpdateNibInput ? true : never;
const _generatedUpdateKeysCheck: _GeneratedUpdateKeysExistOnClient = true;
void _generatedUpdateKeysCheck;

export type CreateNibCommand = { kind: "create-nib"; input: CreateNibInput };
export type UpdateNibCommand = { kind: "update-nib"; id: string; input: UpdateNibInput; ifMatch?: string };
export type DeleteNibCommand = { kind: "delete-nib"; id: string };
export type ArchiveNibCommand = { kind: "archive-nib"; id: string };
export type SetParentCommand = { kind: "set-parent"; id: string; parentId: string | null };
export type ReorderNibCommand = { kind: "reorder-nib"; id: string; afterId?: string; beforeId?: string; first?: boolean; parentId?: string; scope?: OrderScope };

export type LeafCommand =
  | CreateNibCommand
  | UpdateNibCommand
  | DeleteNibCommand
  | ArchiveNibCommand
  | SetParentCommand
  | ReorderNibCommand;

// --- Result types ---

export interface CommandResult {
  ok: boolean;
  data?: any;
  error?: string;
  /** Machine-readable GraphQL error code lifted from the failed leaf's
   *  `extensions.code` (e.g. "ETAG_MISMATCH"), when the server tagged one.
   *  Classifiers should prefer this over substring-matching `error`. */
  errorCode?: string;
}

/** Options threaded through `MutationStore.execute` → `MutationDispatcher`. */
export interface ExecuteOptions {
  /** Suppress the default `toast.error(...)` on a failed leaf mutation so the
   *  CALLER owns messaging for this call (e.g. `save()` routes a 409 into the
   *  inline conflict resolver instead of a racing raw toast). Defaults to
   *  false — every other caller keeps the "toast on error" behavior, including
   *  the individual legs of a batch/sequence. */
  suppressToast?: boolean;
}

export interface BatchResult {
  ok: boolean;
  results: CommandResult[];
  successes: number;
  failures: number;
}

export interface SequenceResult {
  ok: boolean;
  results: CommandResult[];
  stoppedAt?: number;
}

export type AnyResult = CommandResult | BatchResult | SequenceResult;

// --- Composite command types ---

export type SequenceStep = LeafCommand | ((prev: CommandResult) => LeafCommand);
export type BatchCommand = { kind: "batch"; commands: LeafCommand[] };
export type SequenceCommand = { kind: "sequence"; steps: SequenceStep[] };
export type CompositeCommand = BatchCommand | SequenceCommand;
export type AnyCommand = LeafCommand | CompositeCommand;
