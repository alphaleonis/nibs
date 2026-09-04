import { graphql } from "./gql";

export const CONFIG_QUERY = graphql(`
  query Config {
    config {
      projectName
      prefix
      areas {
        path
        name
        description
        color
        depth
      }
    }
  }
`);

/**
 * The store's declared areas vocabulary, pushed when it changes.
 *
 * It selects the SAME fields as CONFIG_QUERY, and must keep doing so: the app
 * renders from whichever of the two answered last, so a field selected in only
 * one would appear or vanish depending on whether the vocabulary had been
 * edited during the session.
 */
export const CONFIG_CHANGED_SUBSCRIPTION = graphql(`
  subscription ConfigChanged {
    configChanged {
      projectName
      prefix
      areas {
        path
        name
        description
        color
        depth
      }
    }
  }
`);

/**
 * The milestones a nib can be assigned to.
 *
 * A query of its own rather than a read of the table's rows: those are
 * server-filtered by the active filter, so `type:bug` empties the list and
 * `status:todo` truncates it — and the picker would then offer a subset of the
 * store that changes as the user filters. Sorted by ORDER so the list reads in
 * the sequence the roadmap plans the waves in, not alphabetically.
 *
 * `status` is selected because the assignment door reads it
 * (`milestoneAcceptsAssignment`): a released milestone refuses open work, and
 * the picker says so rather than letting the save fail.
 */
export const MILESTONES_QUERY = graphql(`
  query Milestones {
    nibs(filter: { type: ["milestone"] }, sort: { field: ORDER, direction: ASC }) {
      id
      title
      status
    }
  }
`);

export const UPDATE_STATUS_QUERY = graphql(`
  query UpdateStatus {
    updateStatus {
      current
      latest
      updateAvailable
    }
  }
`);

export const NIB_DETAIL_QUERY = graphql(`
  query NibDetail($id: ID!) {
    nib(id: $id) {
      id
      title
      status
      type
      priority
      estimate
      milestone
      tags
      body
      documents
      etag
      parent {
        id
        title
        type
        status
      }
      children(sort: { field: ORDER, direction: ASC }) {
        id
        title
        type
        status
      }
      blocking {
        id
        title
        type
        status
      }
      blockedBy {
        id
        title
        type
        status
      }
      mentions {
        id
        title
        type
        status
      }
      mentionedBy {
        id
        title
        type
        status
      }
    }
  }
`);

// Lean, DEDICATED one-shot query for the null-remote conflict fallback.
// It selects ONLY the fields `toNibSnapshot` reads — a strict
// subset of NIB_DETAIL_QUERY — under a DISTINCT operation name. That distinctness
// is load-bearing: urql keys its result-source on (query text + variables), so a
// separate document means this network-only fetch does NOT share a source with
// App's live `detailStore` (which runs NIB_DETAIL_QUERY for the same id). If they
// shared, a `{ nib: null }` response (nib deleted in the race window) would be
// pushed into `detailStore` and trip App's missing-nib effect, silently dropping
// the user's dirty buffer. Keep this selection in lockstep with `toNibSnapshot`.
export const NIB_CONFLICT_SNAPSHOT_QUERY = graphql(`
  query NibConflictSnapshot($id: ID!) {
    nib(id: $id) {
      id
      title
      status
      type
      priority
      estimate
      milestone
      tags
      body
      etag
    }
  }
`);

export const UPDATE_NIB_MUTATION = graphql(`
  mutation UpdateNib($id: ID!, $input: UpdateNibInput!) {
    updateNib(id: $id, input: $input) {
      id
      title
      status
      type
      priority
      estimate
      milestone
      tags
      etag
    }
  }
`);

export const DELETE_NIB_MUTATION = graphql(`
  mutation DeleteNib($id: ID!) {
    deleteNib(id: $id)
  }
`);

export const ARCHIVE_NIB_MUTATION = graphql(`
  mutation ArchiveNib($id: ID!) {
    archiveNib(id: $id)
  }
`);

export const CREATE_NIB_MUTATION = graphql(`
  mutation CreateNib($input: CreateNibInput!) {
    createNib(input: $input) {
      id
      title
      status
      type
      priority
      estimate
      tags
      body
      etag
      parentId
      order
    }
  }
`);

export const SET_PARENT_MUTATION = graphql(`
  mutation SetParent($id: ID!, $parentId: String) {
    setParent(id: $id, parentId: $parentId) {
      id
      title
      status
      type
      priority
      estimate
      tags
      etag
      parentId
    }
  }
`);

export const REORDER_NIB_MUTATION = graphql(`
  mutation ReorderNib($id: ID!, $afterId: ID, $beforeId: ID, $first: Boolean, $parentId: String, $scope: OrderScope) {
    reorderNib(id: $id, afterId: $afterId, beforeId: $beforeId, first: $first, parentId: $parentId, scope: $scope) {
      id
      title
      status
      type
      priority
      estimate
      tags
      etag
      parentId
      order
    }
  }
`);

export const TREE_TABLE_QUERY = graphql(`
  query TreeTable($filter: NibFilter) {
    nibs(filter: $filter, sort: { field: ORDER, direction: ASC }) {
      id
      title
      status
      type
      priority
      estimate
      tags
      createdAt
      updatedAt
      parentId
      milestone
      milestoneOrder
      area
      blockingIds
      blockedByIds
      etag
    }
  }
`);

// Lean typeahead query for the relationship-id token completion (phase 6). Reuses
// the existing `nibs` search resolver (Bleve matches an ID fragment AND title) but
// selects only the four fields a candidate row shows, under its own operation name
// so its urql result-source stays independent of the tree-table list query.
export const SEARCH_NIBS_QUERY = graphql(`
  query SearchNibs($search: String!) {
    nibs(filter: { search: $search }) {
      id
      title
      type
      status
    }
  }
`);

export const NIB_CHANGED_SUBSCRIPTION = graphql(`
  subscription NibChanged($id: ID) {
    nibChanged(id: $id) {
      type
      nibId
      nib {
        id
        title
        status
        type
        priority
        estimate
        milestone
        tags
        body
        etag
        updatedAt
        parentId
        blockingIds
        blockedByIds
      }
    }
  }
`);
