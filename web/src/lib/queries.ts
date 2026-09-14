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
 * The store's config, pushed when it changes.
 *
 * Keep its selection identical to CONFIG_QUERY's: the app renders from
 * whichever of the two answered last.
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
 * The milestones a nib can be assigned to, in planned order.
 *
 * Do not derive this from the table's rows: the active filter narrows those.
 * `status` feeds `milestoneAcceptsAssignment`.
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
      area
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

// One-shot fetch for the conflict fallback. Selects the fields `toNibSnapshot`
// reads; keep the two in sync.
//
// Do not merge it into NIB_DETAIL_QUERY: urql keys results on query text plus
// variables, so a shared document would push a `{ nib: null }` answer into
// App's `detailStore` and drop the user's unsaved edits.
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
      area
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
      area
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

// Typeahead for relationship-id tokens. `search` matches id fragments as well as
// text. A separate operation from TREE_TABLE_QUERY so urql keeps their results
// apart.
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
        area
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
