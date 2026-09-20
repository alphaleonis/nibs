import type { CreateNibInput as GeneratedCreateNibInput, OrderScope, UpdateNibInput as GeneratedUpdateNibInput } from "../gql/graphql";

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
  /** A declared area path; omitted or "" leaves it unset. */
  area?: string;
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
  /** The milestone whose queue the nib joins (not `parent`). Null or "" clears;
   *  omitted leaves it unchanged. */
  milestone?: string | null;
  /** A declared area path. Null or "" clears; omitted leaves it unchanged. */
  area?: string | null;
  addBlocking?: string[];
  removeBlocking?: string[];
  addBlockedBy?: string[];
  removeBlockedBy?: string[];
  documents?: string[];
  addDocuments?: string[];
  removeDocuments?: string[];
};

// Compile-time guards: these hand-written inputs and the generated ones must have
// EQUAL key sets, as for NibFilter in ../types.ts. Inputs reach urql as variables
// (and `assignmentFor` in ordering/dropPlan.ts uses a computed key), so no
// excess-property check catches a key the server lacks.
//
// `ifMatch` is excluded from the generated update keys: here it lives on
// UpdateNibCommand, and the dispatcher merges it into the input.
type GeneratedUpdateKeys = Exclude<keyof GeneratedUpdateNibInput, "ifMatch">;

type _UpdateKeysExistOnGenerated = keyof UpdateNibInput extends GeneratedUpdateKeys ? true : never;
const _updateKeysCheck: _UpdateKeysExistOnGenerated = true;
void _updateKeysCheck;

type _GeneratedUpdateKeysExistOnClient = GeneratedUpdateKeys extends keyof UpdateNibInput ? true : never;
const _generatedUpdateKeysCheck: _GeneratedUpdateKeysExistOnClient = true;
void _generatedUpdateKeysCheck;

// The create input has no command-level key, so do not add an Exclude<> here.
type _CreateKeysExistOnGenerated = keyof CreateNibInput extends keyof GeneratedCreateNibInput ? true : never;
const _createKeysCheck: _CreateKeysExistOnGenerated = true;
void _createKeysCheck;

type _GeneratedCreateKeysExistOnClient = keyof GeneratedCreateNibInput extends keyof CreateNibInput ? true : never;
const _generatedCreateKeysCheck: _GeneratedCreateKeysExistOnClient = true;
void _generatedCreateKeysCheck;

export type CreateNibCommand = { kind: "create-nib"; input: CreateNibInput };
export type UpdateNibCommand = { kind: "update-nib"; id: string; input: UpdateNibInput; ifMatch?: string };
export type DeleteNibCommand = { kind: "delete-nib"; id: string };
export type ArchiveNibCommand = { kind: "archive-nib"; id: string };
export type SetParentCommand = { kind: "set-parent"; id: string; parentId: string | null };
export type ReorderNibCommand = { kind: "reorder-nib"; id: string; afterId?: string; beforeId?: string; first?: boolean; parentId?: string; scope?: OrderScope };

/**
 * Declare one area at `path`, which is the FULL path: the last segment names the
 * new node and everything before it names an already-declared parent.
 *
 * `description` and `color` are absent rather than "" when the caller set
 * neither — the server reads "" as a value someone emptied, so the two cannot be
 * collapsed. Keep them optional here so the distinction survives into
 * `getVariables`.
 */
export type AddAreaCommand = { kind: "add-area"; path: string; description?: string; color?: string };

/**
 * Edit the declared area at `path`. Every field but `path` is optional, and an
 * absent one leaves that key exactly as the store declares it while "" CLEARS
 * it — so an unset field must stay off the command rather than becoming "".
 *
 * `newName` is the node's own SEGMENT, never a path.
 */
export type UpdateAreaCommand = {
  kind: "update-area";
  path: string;
  newName?: string;
  description?: string;
  color?: string;
};

/**
 * What to do with the nibs assigned at or below an area being retired. The two
 * are mutually exclusive, so they are a UNION rather than two optional fields —
 * a caller cannot name both without failing to compile.
 *
 * `unassign` is `true` alone: the server reads `false` as "no disposition"
 * rather than as a contradiction, so a `false` here would silently mean
 * something other than what it looks like.
 */
export type AreaDisposition = { moveTo: string } | { unassign: true };

/**
 * Retire the declared area at `path`, together with its whole subtree.
 *
 * Refused while any nib is assigned at or below the node unless a disposition
 * says what to do with those members instead — and equally refused when a
 * disposition names an area nothing is assigned to, so an absent disposition
 * is not a safe default to send.
 */
export type RemoveAreaCommand = {
  kind: "remove-area";
  path: string;
  moveTo?: string;
  unassign?: true;
};

export type LeafCommand =
  | CreateNibCommand
  | UpdateNibCommand
  | DeleteNibCommand
  | ArchiveNibCommand
  | SetParentCommand
  | ReorderNibCommand
  | AddAreaCommand
  | UpdateAreaCommand
  | RemoveAreaCommand;

// --- Result types ---

export interface CommandResult {
  ok: boolean;
  data?: any;
  error?: string;
  /** The failure's GraphQL `extensions.code` (e.g. "ETAG_MISMATCH"), if tagged.
   *  Classify on this rather than matching `error`. */
  errorCode?: string;
}

/** Options for `MutationStore.execute`, passed on to `MutationDispatcher`. */
export interface ExecuteOptions {
  /** Skip the error toast on every failed leaf, batch and sequence legs included,
   *  so the caller owns the messaging. */
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
