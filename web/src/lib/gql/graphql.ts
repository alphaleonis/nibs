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
  /**
   * Area path the new nib belongs to (e.g. "web/ui"). Must be a path the store's
   * config declares; an undeclared one is refused naming the declared set, and a
   * store that declares no areas refuses every value. Omit or "" to leave it
   * unset, which is always legal.
   */
  area?: string | null | undefined;
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
  /**
   * Include only nibs with this specific nib ID somewhere in their parent chain
   * (that nib's descendants at any depth). This filter excludes the nib itself.
   * On the top-level nibs query, combining it with search adds it back, because
   * that query completes the tree with every match's ancestors. A relationship
   * field does not complete the tree, so there the target stays excluded — see
   * search.
   *
   * An id naming no nib is refused with a NOT_FOUND error rather than matching
   * nothing, so a mistyped or stale id stays distinguishable from a genuine empty
   * result. An empty string is refused as a malformed argument: it names no nib
   * and never could. Unlike the not-found refusal above it carries no
   * extensions.code, so a GraphQL client sees a generic error; the CLI reports
   * VALIDATION_ERROR (exit 2). Omit the field to leave it unfiltered.
   */
  ancestorId?: string | null | undefined;
  /**
   * Include only the area's work, DOWNWARD-CLOSED over the declared tree: the nibs
   * whose `area:` is this path, plus those in every area declared beneath it. So
   * `area: "web"` selects `web` and `web/dashboard` alike, while
   * `area: "web/dashboard"` selects that leaf alone — closure runs downward and a
   * leaf never pulls in its parent.
   *
   * Closure is over the TREE and not over the strings. `webhooks` is not within
   * `web` even though one spells a prefix of the other, because they are two roots;
   * and a stored value the vocabulary no longer declares is within nothing, so
   * retiring `web/legacy` from the `areas:` block drops it out of `area: "web"`
   * rather than leaving it swept in by a filter naming its former parent. The value
   * is a declared path, never a nib id.
   *
   * A path the store does not declare is refused as a malformed argument naming
   * the declared set, rather than matching nothing: an empty answer reads as "no
   * work is in this area" for a value that names no area at all — the reading
   * `milestone` refuses an unknown id for. Like the empty-id refusals above it
   * carries no extensions.code, so a GraphQL client sees a generic error; the CLI
   * reports VALIDATION_ERROR (exit 2), the class `nibs set --area` gives the same
   * value. The empty string is refused as that same class, in its own words: it
   * names no area, and read as "unset" the branch would widen the query to the
   * whole store. Omit the field to leave it unfiltered. A store that declares no
   * `areas:` block at all says THAT instead of naming an empty allowed set, since
   * the repair there is a config edit rather than a different value.
   */
  area?: string | null | undefined;
  /**
   * Include only nibs blocked by this specific nib ID (via blocked_by field).
   *
   * An id naming no nib is refused with a NOT_FOUND error rather than matching
   * nothing, so a mistyped or stale id stays distinguishable from a genuine empty
   * result. An empty string is refused as a malformed argument: it names no nib
   * and never could. Unlike the not-found refusal above it carries no
   * extensions.code, so a GraphQL client sees a generic error; the CLI reports
   * VALIDATION_ERROR (exit 2). Omit the field to leave it unfiltered.
   *
   * Combining it with hasBlockedBy: false is refused as a malformed argument
   * (VALIDATION_ERROR, exit 2), and refused BEFORE the id is looked up, so the
   * pair is reported even when the id also names no nib: matching this field
   * requires a blocked_by entry, so no store state satisfies both halves. Unlike
   * the empty-id refusal above it carries extensions.code = "FILTER_CONTRADICTION",
   * so a GraphQL client can route it structurally rather than on message text. The
   * exception is the EMPTY string, which keeps the empty-id refusal above — the
   * same class and exit, but uncoded, reported as malformed input rather than as
   * the pair.
   */
  blockedById?: string | null | undefined;
  /**
   * Include only nibs that are blocking this specific nib ID.
   *
   * Membership is the target's stored blocked_by, whatever the candidate's status,
   * which is what makes `blockingId: X, hasBlocking: false` meaningful: it selects
   * the blockers X still lists that are no longer blocking anything — either
   * because their own status released their dependents, or because every nib that
   * listed them, X included, has itself been released. The second case is why the
   * pair can return an OPEN blocker: X completed, its todo blocker listed nowhere
   * else, and that blocker is in the answer. That pair is answered, not refused;
   * hasBlocking: true selects the ones still blocking.
   *
   * An id naming no nib is refused with a NOT_FOUND error rather than matching
   * nothing, so a mistyped or stale id stays distinguishable from a genuine empty
   * result. An empty string is refused as a malformed argument: it names no nib
   * and never could. Unlike the not-found refusal above it carries no
   * extensions.code, so a GraphQL client sees a generic error; the CLI reports
   * VALIDATION_ERROR (exit 2). Omit the field to leave it unfiltered.
   */
  blockingId?: string | null | undefined;
  /**
   * Include only nibs with this specific nib ID somewhere in their descendant
   * subtree (that nib's ancestor chain, itself excluded).
   *
   * An id naming no nib is refused with a NOT_FOUND error rather than matching
   * nothing, so a mistyped or stale id stays distinguishable from a genuine empty
   * result. An empty string is refused as a malformed argument: it names no nib
   * and never could. Unlike the not-found refusal above it carries no
   * extensions.code, so a GraphQL client sees a generic error; the CLI reports
   * VALIDATION_ERROR (exit 2). Omit the field to leave it unfiltered.
   */
  descendantId?: string | null | undefined;
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
  /**
   * Tri-state: true keeps nibs that have explicit blocked_by entries, false keeps
   * exactly those with none, null does not filter.
   *
   * Combining false with the blockedById FILTER is refused: no nib both lists a
   * given blocker and lists none. See blockedById.
   */
  hasBlockedBy?: boolean | null | undefined;
  /**
   * Tri-state: true keeps nibs that are ACTIVELY blocking others, false keeps
   * exactly the rest, null does not filter. A blocker whose status released its
   * dependents (completed, scrapped) is not actively blocking anything, so it is
   * in the false set even while other nibs still list it in their blocked_by.
   *
   * Combining false with blockingId is therefore a real query rather than a
   * contradiction — see blockingId.
   */
  hasBlocking?: boolean | null | undefined;
  /**
   * Tri-state: true keeps nibs whose parent link resolves to a nib, false keeps
   * exactly the ones with no parent, null does not filter. A nib whose parent link
   * names no nib counts as parentless, matching how the parent field, parentId and
   * siblingId treat it. Such a nib still reports its unresolvable link under
   * storedParentId, which is not a parent and does not affect this filter.
   *
   * Combining false with the parentId FILTER is refused: no nib both has a given
   * parent and has none. See parentId.
   */
  hasParent?: boolean | null | undefined;
  /** Tri-state: true keeps nibs blocked by others (via incoming blocking links or blocked_by field), false keeps exactly the unblocked ones, null does not filter */
  isBlocked?: boolean | null | undefined;
  /**
   * Include only nibs mentioned in the given nib's body.
   *
   * An id naming no nib is refused with a NOT_FOUND error rather than matching
   * nothing, so a mistyped or stale id stays distinguishable from a genuine empty
   * result. An empty string is refused as a malformed argument: it names no nib
   * and never could. Unlike the not-found refusal above it carries no
   * extensions.code, so a GraphQL client sees a generic error; the CLI reports
   * VALIDATION_ERROR (exit 2). Omit the field to leave it unfiltered.
   */
  mentionedById?: string | null | undefined;
  /**
   * Include only nibs that mention this specific nib ID in their body.
   *
   * An id naming no nib is refused with a NOT_FOUND error rather than matching
   * nothing, so a mistyped or stale id stays distinguishable from a genuine empty
   * result. An empty string is refused as a malformed argument: it names no nib
   * and never could. Unlike the not-found refusal above it carries no
   * extensions.code, so a GraphQL client sees a generic error; the CLI reports
   * VALIDATION_ERROR (exit 2). Omit the field to leave it unfiltered.
   */
  mentionsId?: string | null | undefined;
  /**
   * Include only the milestone's queue: nibs whose `milestone:` assignment
   * RESOLVES to this milestone. Resolution is the membership rule the ordering
   * engine's queue scope also groups by — the stored id must name an existing,
   * milestone-typed nib to count, so a dangling assignment matches nothing
   * (known gap, tracked as nibs-4h8f: such an assignment is dropped silently
   * rather than flagged). Resolution checks the target's type, never the
   * assignee's: a milestone-typed nib hand-edited to carry an assignment — a
   * shape the write path refuses — is in this set even though noMilestone's
   * derived reading keeps it in the backlog set. This is DIRECT assignment
   * only: the structural children of an assigned nib are planned work in the
   * derived sense noMilestone reads, but they are not in this set.
   *
   * An id naming no nib is refused with a NOT_FOUND error rather than matching
   * nothing, so a mistyped or stale id stays distinguishable from a genuine empty
   * result. An empty string is refused as a malformed argument: it names no nib
   * and never could. Unlike the not-found refusal above it carries no
   * extensions.code, so a GraphQL client sees a generic error; the CLI reports
   * VALIDATION_ERROR (exit 2). An id naming an existing NON-milestone nib is
   * refused the same way, naming the nib's actual type: no assignment can resolve
   * to it, so an empty answer would read as "this milestone has no members" for
   * an id that names no milestone — the mistake updateNib's milestone field
   * refuses with a message of the same shape. Omit the field to leave it
   * unfiltered.
   */
  milestone?: string | null | undefined;
  /**
   * Tri-state over DERIVED milestone membership: true keeps the backlog — nibs
   * with neither an own resolved assignment nor one anywhere up the structural
   * parent chain, so a child of an assigned epic is planned work and NOT in
   * this set. false keeps the complement: on data the write path accepts,
   * exactly the nibs some milestone's queue transitively contains. Null does
   * not filter.
   *
   * Milestone-typed nibs belong to no milestone themselves — a milestone is a
   * container, not a member — so they sit in the true set; combine with
   * excludeType: ["milestone"] to keep them out. A dangling or non-milestone
   * assignment schedules nothing and leaves the nib in the true set. A
   * milestone-typed nib hand-edited to carry an assignment — a shape the write
   * path refuses — also sits in the true set here while the milestone filter's
   * resolved-assignment reading places it in that milestone's queue set.
   */
  noMilestone?: boolean | null | undefined;
  /**
   * Include only nibs with this specific parent ID.
   *
   * Matches on the RESOLVED parent, the same reading the parentId field and
   * hasParent give: a nib whose link is stored in short form matches the full id
   * it resolves to, and a link naming no nib matches nothing. Filtering on the
   * raw stored spelling is not offered — storedParentId is an inspection field,
   * not a filter.
   *
   * An id naming no nib is refused with a NOT_FOUND error rather than matching
   * nothing, so a mistyped or stale id stays distinguishable from a genuine empty
   * result. An empty string is refused as a malformed argument: it names no nib
   * and never could. Unlike the not-found refusal above it carries no
   * extensions.code, so a GraphQL client sees a generic error; the CLI reports
   * VALIDATION_ERROR (exit 2). Omit the field to leave it unfiltered, or use
   * hasParent: false to select the nibs that have no parent.
   *
   * Combining it with hasParent: false is refused as a malformed argument
   * (VALIDATION_ERROR, exit 2), and refused BEFORE the id is looked up, so the
   * pair is reported even when the id also names no nib: every nib this field
   * matches has a parent, so no store state satisfies both halves and correcting
   * the id would not make the query answerable. Unlike the empty-id refusal above
   * it carries extensions.code = "FILTER_CONTRADICTION", so a GraphQL client can
   * route it structurally rather than on message text. The exception is the EMPTY
   * string, which keeps the empty-id refusal above — the same class and exit, but
   * uncoded, and its message redirects to hasParent: false, the filter that does
   * select parentless nibs. The flag surface refuses
   * `nibs list --parent X --no-parent` with the same exit status.
   */
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
   *
   * An empty string leaves it unfiltered, the opposite of the id-valued fields
   * such as parentId: "no keyword filter" is a real meaning, so `search: "$q"`
   * with an empty q is a reasonable thing to write. There is no nib whose id is
   * "", which is why the same value is a refusal there.
   *
   * On a relationship field (children, blockedBy, blocking, mentions,
   * mentionedBy) the term INTERSECTS that relationship instead of choosing the
   * nibs to consider: `children(filter: {search: "auth"})` means "the children of
   * this nib that match auth", and a match that is not a child of it is not in
   * the answer. Two consequences follow from that, both differences from the
   * top-level nibs query. A relationship field never returns a nib outside the
   * relation, so it does not add the ancestors of its matches — tree completion
   * belongs to a query over the whole store. And relevance ordering does not
   * apply: the field keeps its own order (children stay in order key order),
   * since the term selects rather than ranks.
   *
   * WHICH answer the index is asked for follows from what bounds the query, not
   * from which surface asks. A term selecting from the whole store is CAPPED: the
   * index answers with at most 1000 hits per leg (id matches and full-text hits are
   * capped separately) and that truncation IS the answer — the top hits for the
   * term. A term intersected with a set something else already bounds is UNCAPPED,
   * because a store-wide cap there would truncate the store rather than the answer,
   * dropping a genuine member that ranks below the global cutoff.
   *
   * Concretely, on the top-level nibs query: the term alone is capped, and so is
   * the term alongside any of the list and tri-state facets — status, excludeStatus,
   * type, excludeType, priority, excludePriority, estimate, excludeEstimate, tags,
   * excludeTags, hasParent, hasBlocking, isBlocked, hasBlockedBy, noMilestone — or
   * alongside area, whose value is a declared PATH and not a nib, however few nibs
   * an area holds. None of those names a nib, so the population they narrow is
   * still the store.
   * Combining the term with a field that DOES name one — parentId, ancestorId,
   * descendantId, siblingId, blockingId, blockedById, mentionsId, mentionedById,
   * milestone — makes the read uncapped. Every relationship field (children,
   * blockedBy, blocking, mentions, mentionedBy) is uncapped for the same reason:
   * the relation it names is the bound.
   *
   * So `nibs(filter: {search: q, parentId: X})` and
   * `nib(id: X) { children(filter: {search: q}) }` agree on the MATCHES. They are
   * still not the same response: the top-level query completes the tree with those
   * matches' ancestors, X included, and a relationship field does not.
   */
  search?: string | null | undefined;
  /**
   * Include only nibs sharing this specific nib's parent, or the other root nibs
   * when it has no parent (itself excluded). On the top-level nibs query,
   * combining it with search also brings in the shared parent, because that query
   * completes the tree with every match's ancestors. A relationship field does not
   * complete the tree, so there the shared parent stays out — see search.
   *
   * An id naming no nib is refused with a NOT_FOUND error rather than matching
   * nothing, so a mistyped or stale id stays distinguishable from a genuine empty
   * result. An empty string is refused as a malformed argument: it names no nib
   * and never could. Unlike the not-found refusal above it carries no
   * extensions.code, so a GraphQL client sees a generic error; the CLI reports
   * VALIDATION_ERROR (exit 2). Omit the field to leave it unfiltered.
   */
  siblingId?: string | null | undefined;
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
  /**
   * Set the area assignment — the ownership axis. The value must be a path the
   * store's config declares; an undeclared one is refused naming the declared set,
   * and a store that declares no areas refuses every value and says so. A
   * milestone-typed subject is refused (a waypoint is not work and takes no area).
   *
   * Explicit null OR empty string clears the assignment; omit to leave it
   * unchanged. Unlike milestone this names no nib, so nothing is resolved and no
   * queue moves — it is a plain path-valued scalar.
   */
  area?: string | null | undefined;
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
   * Set the milestone assignment — the scheduling axis. The target must exist
   * and be milestone-typed; a missing target or one of any other type is
   * refused naming why. Exclusivity along the parent chain is enforced: the
   * assignment is refused when any ancestor or any descendant of the nib is
   * already assigned, naming the conflicting nib. A milestone-typed subject is
   * refused (a waypoint is not work and takes no assignment).
   *
   * On assignment the nib enters the target's queue at the default placement
   * (last), and a reassignment re-enters the new queue the same way — the queue
   * key is never carried from one queue to another. Explicit null OR empty
   * string clears the assignment and the queue key with it; omit to leave both
   * unchanged. An update carrying both a parent and a milestone change is judged
   * on the state it leaves: a clear of either axis opens the way for the other,
   * and an assignment is checked against the chain the nib will sit on.
   */
  milestone?: string | null | undefined;
  /**
   * Set the parent nib ID (validated against the type hierarchy). Explicit null
   * OR empty string clears the parent (moves the nib to root); omit to leave it
   * unchanged.
   *
   * A reparent also honors assignment exclusivity: when the nib or any nib in
   * its subtree is assigned to a milestone AND the new parent or any of its
   * ancestors is too, the move is refused naming both nibs — a nib and one of
   * its ancestors are never both assigned.
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


export type TreeTableQuery = { nibs: Array<{ id: string, title: string, status: string, type: string, priority: string, estimate: string, tags: Array<string>, createdAt: string, updatedAt: string, parentId: string | null, blockingIds: Array<string>, blockedByIds: Array<string>, etag: string }> };

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
export const TreeTableDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"query","name":{"kind":"Name","value":"TreeTable"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"filter"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"NibFilter"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"nibs"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"filter"},"value":{"kind":"Variable","name":{"kind":"Name","value":"filter"}}},{"kind":"Argument","name":{"kind":"Name","value":"sort"},"value":{"kind":"ObjectValue","fields":[{"kind":"ObjectField","name":{"kind":"Name","value":"field"},"value":{"kind":"EnumValue","value":"ORDER"}},{"kind":"ObjectField","name":{"kind":"Name","value":"direction"},"value":{"kind":"EnumValue","value":"ASC"}}]}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"status"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"priority"}},{"kind":"Field","name":{"kind":"Name","value":"estimate"}},{"kind":"Field","name":{"kind":"Name","value":"tags"}},{"kind":"Field","name":{"kind":"Name","value":"createdAt"}},{"kind":"Field","name":{"kind":"Name","value":"updatedAt"}},{"kind":"Field","name":{"kind":"Name","value":"parentId"}},{"kind":"Field","name":{"kind":"Name","value":"blockingIds"}},{"kind":"Field","name":{"kind":"Name","value":"blockedByIds"}},{"kind":"Field","name":{"kind":"Name","value":"etag"}}]}}]}}]} as unknown as DocumentNode<TreeTableQuery, TreeTableQueryVariables>;
export const SearchNibsDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"query","name":{"kind":"Name","value":"SearchNibs"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"search"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"nibs"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"filter"},"value":{"kind":"ObjectValue","fields":[{"kind":"ObjectField","name":{"kind":"Name","value":"search"},"value":{"kind":"Variable","name":{"kind":"Name","value":"search"}}}]}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"status"}}]}}]}}]} as unknown as DocumentNode<SearchNibsQuery, SearchNibsQueryVariables>;
export const NibChangedDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"NibChanged"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"ID"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"nibChanged"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"nibId"}},{"kind":"Field","name":{"kind":"Name","value":"nib"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"status"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"priority"}},{"kind":"Field","name":{"kind":"Name","value":"estimate"}},{"kind":"Field","name":{"kind":"Name","value":"tags"}},{"kind":"Field","name":{"kind":"Name","value":"body"}},{"kind":"Field","name":{"kind":"Name","value":"etag"}},{"kind":"Field","name":{"kind":"Name","value":"updatedAt"}},{"kind":"Field","name":{"kind":"Name","value":"parentId"}},{"kind":"Field","name":{"kind":"Name","value":"blockingIds"}},{"kind":"Field","name":{"kind":"Name","value":"blockedByIds"}}]}}]}}]}}]} as unknown as DocumentNode<NibChangedSubscription, NibChangedSubscriptionVariables>;