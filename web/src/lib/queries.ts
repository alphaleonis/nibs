import { gql } from "@urql/svelte";

export const CONFIG_QUERY = gql`
  query Config {
    config {
      projectName
      prefix
    }
  }
`;

export const UPDATE_STATUS_QUERY = gql`
  query UpdateStatus {
    updateStatus {
      current
      latest
      updateAvailable
    }
  }
`;

export const NIB_DETAIL_QUERY = gql`
  query NibDetail($id: ID!) {
    nib(id: $id) {
      id
      title
      status
      type
      priority
      estimate
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
`;

// Lean, DEDICATED one-shot query for the null-remote conflict fallback.
// It selects ONLY the fields `toNibSnapshot` reads — a strict
// subset of NIB_DETAIL_QUERY — under a DISTINCT operation name. That distinctness
// is load-bearing: urql keys its result-source on (query text + variables), so a
// separate document means this network-only fetch does NOT share a source with
// App's live `detailStore` (which runs NIB_DETAIL_QUERY for the same id). If they
// shared, a `{ nib: null }` response (nib deleted in the race window) would be
// pushed into `detailStore` and trip App's missing-nib effect, silently dropping
// the user's dirty buffer. Keep this selection in lockstep with `toNibSnapshot`.
export const NIB_CONFLICT_SNAPSHOT_QUERY = gql`
  query NibConflictSnapshot($id: ID!) {
    nib(id: $id) {
      id
      title
      status
      type
      priority
      estimate
      tags
      body
      etag
    }
  }
`;

export const UPDATE_NIB_MUTATION = gql`
  mutation UpdateNib($id: ID!, $input: UpdateNibInput!) {
    updateNib(id: $id, input: $input) {
      id
      title
      status
      type
      priority
      estimate
      tags
      etag
    }
  }
`;

export const DELETE_NIB_MUTATION = gql`
  mutation DeleteNib($id: ID!) {
    deleteNib(id: $id)
  }
`;

export const ARCHIVE_NIB_MUTATION = gql`
  mutation ArchiveNib($id: ID!) {
    archiveNib(id: $id)
  }
`;

export const CREATE_NIB_MUTATION = gql`
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
`;

export const SET_PARENT_MUTATION = gql`
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
`;

export const REORDER_NIB_MUTATION = gql`
  mutation ReorderNib($id: ID!, $afterId: ID, $beforeId: ID, $first: Boolean, $parentId: String) {
    reorderNib(id: $id, afterId: $afterId, beforeId: $beforeId, first: $first, parentId: $parentId) {
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
`;

export const TREE_TABLE_QUERY = gql`
  query TreeTable($filter: NibFilter) {
    nibs(filter: $filter, sort: { field: ORDER, direction: ASC }) {
      id
      title
      status
      type
      priority
      estimate
      tags
      updatedAt
      parentId
      blockingIds
      blockedByIds
    }
  }
`;

export const NIB_CHANGED_SUBSCRIPTION = gql`
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
`;
