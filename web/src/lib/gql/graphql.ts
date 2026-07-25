/* eslint-disable */
/** Internal type. DO NOT USE DIRECTLY. */
type Exact<T extends { [key: string]: unknown }> = { [K in keyof T]: T[K] };
/** Internal type. DO NOT USE DIRECTLY. */
export type Incremental<T> = T | { [P in keyof T]?: P extends ' $fragmentName' | '__typename' ? T[P] : never };
import { TypedDocumentNode as DocumentNode } from '@graphql-typed-document-node/core';
/**
 * Structured body modifications applied atomically.
 * Operations are applied in order: all replacements sequentially, then append.
 * If any operation fails, the entire mutation fails (transactional).
 */
export type BodyModification = {
  /**
   * Text to append after all replacements.
   * Appended with blank line separator.
   */
  append?: string | null | undefined;
  /**
   * Text replacements applied sequentially in array order.
   * Each old text must match exactly once at the time it's applied.
   */
  replace?: Array<ReplaceOperation> | null | undefined;
};

/** Input for creating a new nib */
export type CreateNibInput = {
  /** Insert after this sibling nib ID (mutually exclusive with beforeId, first) */
  afterId?: string | null | undefined;
  /** Insert before this sibling nib ID (mutually exclusive with afterId, first) */
  beforeId?: string | null | undefined;
  /** Nib IDs that are blocking this nib */
  blockedBy?: Array<string> | null | undefined;
  /** Nib IDs this nib is blocking */
  blocking?: Array<string> | null | undefined;
  /** Markdown body content */
  body?: string | null | undefined;
  /** Linked document paths (repo-root-relative) */
  documents?: Array<string> | null | undefined;
  /** Estimate size (s, m, l, xl) */
  estimate?: string | null | undefined;
  /** Insert before all siblings (mutually exclusive with afterId, beforeId) */
  first?: boolean | null | undefined;
  /** Parent nib ID (validated against type hierarchy) */
  parent?: string | null | undefined;
  /** Custom ID prefix (overrides config prefix for this nib) */
  prefix?: string | null | undefined;
  /** Priority level (defaults to 'normal') */
  priority?: string | null | undefined;
  /** Status (defaults to 'todo') */
  status?: string | null | undefined;
  /** Tags for categorization */
  tags?: Array<string> | null | undefined;
  /** Nib title (required) */
  title: string;
  /** Nib type (defaults to 'task') */
  type?: string | null | undefined;
};

/** Filter options for querying nibs */
export type NibFilter = {
  /** Include only nibs blocked by this specific nib ID (via blocked_by field) */
  blockedById?: string | null | undefined;
  /** Include only nibs that are blocking this specific nib ID */
  blockingId?: string | null | undefined;
  /** Include only nibs with these estimates (OR logic) */
  estimate?: Array<string> | null | undefined;
  /** Exclude nibs with these estimates */
  excludeEstimate?: Array<string> | null | undefined;
  /** Exclude nibs with these priorities */
  excludePriority?: Array<string> | null | undefined;
  /** Exclude nibs with these statuses */
  excludeStatus?: Array<string> | null | undefined;
  /** Exclude nibs with any of these tags */
  excludeTags?: Array<string> | null | undefined;
  /** Exclude nibs with these types */
  excludeType?: Array<string> | null | undefined;
  /** Include only nibs that have explicit blocked-by entries */
  hasBlockedBy?: boolean | null | undefined;
  /** Include only nibs that are blocking other nibs */
  hasBlocking?: boolean | null | undefined;
  /** Include only nibs with a parent */
  hasParent?: boolean | null | undefined;
  /** Include only nibs that are blocked by others (via incoming blocking links or blocked_by field) */
  isBlocked?: boolean | null | undefined;
  /** Include only nibs mentioned in the given nib's body */
  mentionedById?: string | null | undefined;
  /** Include only nibs that mention this specific nib ID in their body */
  mentionsId?: string | null | undefined;
  /** Exclude nibs that have explicit blocked-by entries */
  noBlockedBy?: boolean | null | undefined;
  /** Exclude nibs that are blocking other nibs */
  noBlocking?: boolean | null | undefined;
  /** Exclude nibs that have a parent */
  noParent?: boolean | null | undefined;
  /** Include only nibs with this specific parent ID */
  parentId?: string | null | undefined;
  /** Include only nibs with these priorities (OR logic) */
  priority?: Array<string> | null | undefined;
  /**
   * Full-text search across slug, title, and body using Bleve query syntax.
   *
   * Single-token queries that look like a nib ID or ID fragment also match
   * directly by ID: a substring of the short ID (at least 2 characters), a
   * prefix of the full ID (starting with the configured prefix), or an exact
   * full ID, case-insensitive, surrounding whitespace trimmed.
   * When no sort is given, ID matches are returned first, followed by
   * full-text hits in relevance order; an explicit sort overrides this
   * ordering.
   *
   * Examples:
   * - "login" - exact term match
   * - "login~" - fuzzy match (1 edit distance)
   * - "login~2" - fuzzy match (2 edit distance)
   * - "log*" - wildcard prefix
   * - "\"user login\"" - exact phrase
   * - "user AND login" - both terms required
   * - "user OR login" - either term
   * - "slug:auth" - search only slug field
   * - "title:login" - search only title field
   * - "body:auth" - search only body field
   * - "5a8k" - also matches nibs whose ID contains "5a8k"
   */
  search?: string | null | undefined;
  /** Include only nibs with these statuses (OR logic) */
  status?: Array<string> | null | undefined;
  /** Include only nibs with any of these tags (OR logic) */
  tags?: Array<string> | null | undefined;
  /** Include only nibs with these types (OR logic) */
  type?: Array<string> | null | undefined;
};

/** A single text replacement operation. */
export type ReplaceOperation = {
  /** Replacement text (can be empty to delete the matched text) */
  new: string;
  /** Text to find (must occur exactly once, cannot be empty) */
  old: string;
};

/** Input for updating an existing nib */
export type UpdateNibInput = {
  /** Add nibs to blocked-by list (validates cycles and existence) */
  addBlockedBy?: Array<string> | null | undefined;
  /** Add nibs to blocking list (validates cycles and existence) */
  addBlocking?: Array<string> | null | undefined;
  /** Add document paths to existing list */
  addDocuments?: Array<string> | null | undefined;
  /** Add tags to existing list */
  addTags?: Array<string> | null | undefined;
  /** New body content (full replacement, mutually exclusive with bodyMod) */
  body?: string | null | undefined;
  /** Structured body modifications (mutually exclusive with body) */
  bodyMod?: BodyModification | null | undefined;
  /** Replace all documents (nil preserves existing, mutually exclusive with addDocuments/removeDocuments) */
  documents?: Array<string> | null | undefined;
  /**
   * New estimate size (s, m, l, xl). Explicit null clears the estimate; omit to
   * leave it unchanged.
   */
  estimate?: string | null | undefined;
  /** ETag for optimistic concurrency control (optional) */
  ifMatch?: string | null | undefined;
  /**
   * Set the parent nib ID (validated against the type hierarchy). Explicit null
   * OR empty string clears the parent (moves the nib to root); omit to leave it
   * unchanged.
   */
  parent?: string | null | undefined;
  /**
   * New priority. Explicit null clears the priority; omit to leave it unchanged.
   * Note: a cleared priority reads back as the effective default "normal" (the
   * data model treats empty as normal), so the clear is not observable on read.
   */
  priority?: string | null | undefined;
  /** Remove nibs from blocked-by list */
  removeBlockedBy?: Array<string> | null | undefined;
  /** Remove nibs from blocking list */
  removeBlocking?: Array<string> | null | undefined;
  /** Remove document paths from existing list */
  removeDocuments?: Array<string> | null | undefined;
  /** Remove tags from existing list */
  removeTags?: Array<string> | null | undefined;
  /** New status. Not a clearable field: null/omit both leave the status unchanged. */
  status?: string | null | undefined;
  /**
   * Replace all tags. An empty list clears all tags; omit (null) to leave the
   * existing tags unchanged. Mutually exclusive with addTags/removeTags.
   */
  tags?: Array<string> | null | undefined;
  /**
   * New title. Not a clearable field: null/omit both leave the title unchanged
   * (a title is required, so it is never cleared).
   */
  title?: string | null | undefined;
  /** New type. Not a clearable field: null/omit both leave the type unchanged. */
  type?: string | null | undefined;
};

export type ConfigQueryVariables = Exact<{ [key: string]: never; }>;


export type ConfigQuery = { config: { projectName: string, prefix: string } };

export type UpdateStatusQueryVariables = Exact<{ [key: string]: never; }>;


export type UpdateStatusQuery = { updateStatus: { current: string, latest: string, updateAvailable: boolean } };

export type NibDetailQueryVariables = Exact<{
  id: string | number;
}>;


export type NibDetailQuery = { nib: { id: string, title: string, status: string, type: string, priority: string, estimate: string, tags: Array<string>, body: string, documents: Array<string>, etag: string, parent: { id: string, title: string, type: string, status: string } | null, children: Array<{ id: string, title: string, type: string, status: string }>, blocking: Array<{ id: string, title: string, type: string, status: string }>, blockedBy: Array<{ id: string, title: string, type: string, status: string }>, mentions: Array<{ id: string, title: string, type: string, status: string }>, mentionedBy: Array<{ id: string, title: string, type: string, status: string }> } | null };

export type NibConflictSnapshotQueryVariables = Exact<{
  id: string | number;
}>;


export type NibConflictSnapshotQuery = { nib: { id: string, title: string, status: string, type: string, priority: string, estimate: string, tags: Array<string>, body: string, etag: string } | null };

export type UpdateNibMutationVariables = Exact<{
  id: string | number;
  input: UpdateNibInput;
}>;


export type UpdateNibMutation = { updateNib: { id: string, title: string, status: string, type: string, priority: string, estimate: string, tags: Array<string>, etag: string } };

export type DeleteNibMutationVariables = Exact<{
  id: string | number;
}>;


export type DeleteNibMutation = { deleteNib: boolean };

export type ArchiveNibMutationVariables = Exact<{
  id: string | number;
}>;


export type ArchiveNibMutation = { archiveNib: boolean };

export type CreateNibMutationVariables = Exact<{
  input: CreateNibInput;
}>;


export type CreateNibMutation = { createNib: { id: string, title: string, status: string, type: string, priority: string, estimate: string, tags: Array<string>, body: string, etag: string, parentId: string | null, order: string } };

export type SetParentMutationVariables = Exact<{
  id: string | number;
  parentId?: string | null | undefined;
}>;


export type SetParentMutation = { setParent: { id: string, title: string, status: string, type: string, priority: string, estimate: string, tags: Array<string>, etag: string, parentId: string | null } };

export type ReorderNibMutationVariables = Exact<{
  id: string | number;
  afterId?: string | number | null | undefined;
  beforeId?: string | number | null | undefined;
  first?: boolean | null | undefined;
  parentId?: string | null | undefined;
}>;


export type ReorderNibMutation = { reorderNib: { id: string, title: string, status: string, type: string, priority: string, estimate: string, tags: Array<string>, etag: string, parentId: string | null, order: string } };

export type TreeTableQueryVariables = Exact<{
  filter?: NibFilter | null | undefined;
}>;


export type TreeTableQuery = { nibs: Array<{ id: string, title: string, status: string, type: string, priority: string, estimate: string, tags: Array<string>, createdAt: string, updatedAt: string, parentId: string | null, blockingIds: Array<string>, blockedByIds: Array<string> }> };

export type SearchNibsQueryVariables = Exact<{
  search: string;
}>;


export type SearchNibsQuery = { nibs: Array<{ id: string, title: string, type: string, status: string }> };

export type NibChangedSubscriptionVariables = Exact<{
  id?: string | number | null | undefined;
}>;


export type NibChangedSubscription = { nibChanged: { type: string, nibId: string, nib: { id: string, title: string, status: string, type: string, priority: string, estimate: string, tags: Array<string>, body: string, etag: string, updatedAt: string, parentId: string | null, blockingIds: Array<string>, blockedByIds: Array<string> } | null } };


export const ConfigDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"query","name":{"kind":"Name","value":"Config"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"config"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"projectName"}},{"kind":"Field","name":{"kind":"Name","value":"prefix"}}]}}]}}]} as unknown as DocumentNode<ConfigQuery, ConfigQueryVariables>;
export const UpdateStatusDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"query","name":{"kind":"Name","value":"UpdateStatus"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"updateStatus"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"current"}},{"kind":"Field","name":{"kind":"Name","value":"latest"}},{"kind":"Field","name":{"kind":"Name","value":"updateAvailable"}}]}}]}}]} as unknown as DocumentNode<UpdateStatusQuery, UpdateStatusQueryVariables>;
export const NibDetailDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"query","name":{"kind":"Name","value":"NibDetail"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"nib"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"status"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"priority"}},{"kind":"Field","name":{"kind":"Name","value":"estimate"}},{"kind":"Field","name":{"kind":"Name","value":"tags"}},{"kind":"Field","name":{"kind":"Name","value":"body"}},{"kind":"Field","name":{"kind":"Name","value":"documents"}},{"kind":"Field","name":{"kind":"Name","value":"etag"}},{"kind":"Field","name":{"kind":"Name","value":"parent"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"status"}}]}},{"kind":"Field","name":{"kind":"Name","value":"children"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"sort"},"value":{"kind":"ObjectValue","fields":[{"kind":"ObjectField","name":{"kind":"Name","value":"field"},"value":{"kind":"EnumValue","value":"ORDER"}},{"kind":"ObjectField","name":{"kind":"Name","value":"direction"},"value":{"kind":"EnumValue","value":"ASC"}}]}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"status"}}]}},{"kind":"Field","name":{"kind":"Name","value":"blocking"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"status"}}]}},{"kind":"Field","name":{"kind":"Name","value":"blockedBy"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"status"}}]}},{"kind":"Field","name":{"kind":"Name","value":"mentions"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"status"}}]}},{"kind":"Field","name":{"kind":"Name","value":"mentionedBy"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"status"}}]}}]}}]}}]} as unknown as DocumentNode<NibDetailQuery, NibDetailQueryVariables>;
export const NibConflictSnapshotDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"query","name":{"kind":"Name","value":"NibConflictSnapshot"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"nib"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"status"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"priority"}},{"kind":"Field","name":{"kind":"Name","value":"estimate"}},{"kind":"Field","name":{"kind":"Name","value":"tags"}},{"kind":"Field","name":{"kind":"Name","value":"body"}},{"kind":"Field","name":{"kind":"Name","value":"etag"}}]}}]}}]} as unknown as DocumentNode<NibConflictSnapshotQuery, NibConflictSnapshotQueryVariables>;
export const UpdateNibDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"UpdateNib"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"input"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"UpdateNibInput"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"updateNib"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"input"},"value":{"kind":"Variable","name":{"kind":"Name","value":"input"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"status"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"priority"}},{"kind":"Field","name":{"kind":"Name","value":"estimate"}},{"kind":"Field","name":{"kind":"Name","value":"tags"}},{"kind":"Field","name":{"kind":"Name","value":"etag"}}]}}]}}]} as unknown as DocumentNode<UpdateNibMutation, UpdateNibMutationVariables>;
export const DeleteNibDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"DeleteNib"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"deleteNib"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}}]}]}}]} as unknown as DocumentNode<DeleteNibMutation, DeleteNibMutationVariables>;
export const ArchiveNibDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ArchiveNib"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"archiveNib"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}}]}]}}]} as unknown as DocumentNode<ArchiveNibMutation, ArchiveNibMutationVariables>;
export const CreateNibDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"CreateNib"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"input"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"CreateNibInput"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"createNib"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"input"},"value":{"kind":"Variable","name":{"kind":"Name","value":"input"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"status"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"priority"}},{"kind":"Field","name":{"kind":"Name","value":"estimate"}},{"kind":"Field","name":{"kind":"Name","value":"tags"}},{"kind":"Field","name":{"kind":"Name","value":"body"}},{"kind":"Field","name":{"kind":"Name","value":"etag"}},{"kind":"Field","name":{"kind":"Name","value":"parentId"}},{"kind":"Field","name":{"kind":"Name","value":"order"}}]}}]}}]} as unknown as DocumentNode<CreateNibMutation, CreateNibMutationVariables>;
export const SetParentDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"SetParent"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"parentId"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"setParent"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"parentId"},"value":{"kind":"Variable","name":{"kind":"Name","value":"parentId"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"status"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"priority"}},{"kind":"Field","name":{"kind":"Name","value":"estimate"}},{"kind":"Field","name":{"kind":"Name","value":"tags"}},{"kind":"Field","name":{"kind":"Name","value":"etag"}},{"kind":"Field","name":{"kind":"Name","value":"parentId"}}]}}]}}]} as unknown as DocumentNode<SetParentMutation, SetParentMutationVariables>;
export const ReorderNibDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ReorderNib"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"afterId"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"ID"}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"beforeId"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"ID"}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"first"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"parentId"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"reorderNib"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"afterId"},"value":{"kind":"Variable","name":{"kind":"Name","value":"afterId"}}},{"kind":"Argument","name":{"kind":"Name","value":"beforeId"},"value":{"kind":"Variable","name":{"kind":"Name","value":"beforeId"}}},{"kind":"Argument","name":{"kind":"Name","value":"first"},"value":{"kind":"Variable","name":{"kind":"Name","value":"first"}}},{"kind":"Argument","name":{"kind":"Name","value":"parentId"},"value":{"kind":"Variable","name":{"kind":"Name","value":"parentId"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"status"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"priority"}},{"kind":"Field","name":{"kind":"Name","value":"estimate"}},{"kind":"Field","name":{"kind":"Name","value":"tags"}},{"kind":"Field","name":{"kind":"Name","value":"etag"}},{"kind":"Field","name":{"kind":"Name","value":"parentId"}},{"kind":"Field","name":{"kind":"Name","value":"order"}}]}}]}}]} as unknown as DocumentNode<ReorderNibMutation, ReorderNibMutationVariables>;
export const TreeTableDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"query","name":{"kind":"Name","value":"TreeTable"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"filter"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"NibFilter"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"nibs"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"filter"},"value":{"kind":"Variable","name":{"kind":"Name","value":"filter"}}},{"kind":"Argument","name":{"kind":"Name","value":"sort"},"value":{"kind":"ObjectValue","fields":[{"kind":"ObjectField","name":{"kind":"Name","value":"field"},"value":{"kind":"EnumValue","value":"ORDER"}},{"kind":"ObjectField","name":{"kind":"Name","value":"direction"},"value":{"kind":"EnumValue","value":"ASC"}}]}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"status"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"priority"}},{"kind":"Field","name":{"kind":"Name","value":"estimate"}},{"kind":"Field","name":{"kind":"Name","value":"tags"}},{"kind":"Field","name":{"kind":"Name","value":"createdAt"}},{"kind":"Field","name":{"kind":"Name","value":"updatedAt"}},{"kind":"Field","name":{"kind":"Name","value":"parentId"}},{"kind":"Field","name":{"kind":"Name","value":"blockingIds"}},{"kind":"Field","name":{"kind":"Name","value":"blockedByIds"}}]}}]}}]} as unknown as DocumentNode<TreeTableQuery, TreeTableQueryVariables>;
export const SearchNibsDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"query","name":{"kind":"Name","value":"SearchNibs"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"search"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"nibs"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"filter"},"value":{"kind":"ObjectValue","fields":[{"kind":"ObjectField","name":{"kind":"Name","value":"search"},"value":{"kind":"Variable","name":{"kind":"Name","value":"search"}}}]}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"status"}}]}}]}}]} as unknown as DocumentNode<SearchNibsQuery, SearchNibsQueryVariables>;
export const NibChangedDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"NibChanged"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"ID"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"nibChanged"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"nibId"}},{"kind":"Field","name":{"kind":"Name","value":"nib"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"status"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"priority"}},{"kind":"Field","name":{"kind":"Name","value":"estimate"}},{"kind":"Field","name":{"kind":"Name","value":"tags"}},{"kind":"Field","name":{"kind":"Name","value":"body"}},{"kind":"Field","name":{"kind":"Name","value":"etag"}},{"kind":"Field","name":{"kind":"Name","value":"updatedAt"}},{"kind":"Field","name":{"kind":"Name","value":"parentId"}},{"kind":"Field","name":{"kind":"Name","value":"blockingIds"}},{"kind":"Field","name":{"kind":"Name","value":"blockedByIds"}}]}}]}}]}}]} as unknown as DocumentNode<NibChangedSubscription, NibChangedSubscriptionVariables>;