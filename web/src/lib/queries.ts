import { gql } from "@urql/svelte";

export const CONFIG_QUERY = gql`
  query Config {
    config {
      projectName
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
