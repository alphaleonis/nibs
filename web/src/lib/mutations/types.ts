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
  addBlocking?: string[];
  removeBlocking?: string[];
  addBlockedBy?: string[];
  removeBlockedBy?: string[];
  documents?: string[];
  addDocuments?: string[];
  removeDocuments?: string[];
};

export type CreateNibCommand = { kind: "create-nib"; input: CreateNibInput };
export type UpdateNibCommand = { kind: "update-nib"; id: string; input: UpdateNibInput; ifMatch?: string };
export type DeleteNibCommand = { kind: "delete-nib"; id: string };
export type ArchiveNibCommand = { kind: "archive-nib"; id: string };
export type SetParentCommand = { kind: "set-parent"; id: string; parentId: string | null };
export type ReorderNibCommand = { kind: "reorder-nib"; id: string; afterId?: string; beforeId?: string; first?: boolean; parentId?: string };

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
