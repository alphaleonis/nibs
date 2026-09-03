/* eslint-disable */
import * as types from './graphql';
import { TypedDocumentNode as DocumentNode } from '@graphql-typed-document-node/core';

/**
 * Map of all GraphQL operations in the project.
 *
 * This map has several performance disadvantages:
 * 1. It is not tree-shakeable, so it will include all operations in the project.
 * 2. It is not minifiable, so the string of a GraphQL query will be multiple times inside the bundle.
 * 3. It does not support dead code elimination, so it will add unused operations.
 *
 * Therefore it is highly recommended to use the babel or swc plugin for production.
 * Learn more about it here: https://the-guild.dev/graphql/codegen/plugins/presets/preset-client#reducing-bundle-size
 */
type Documents = {
    "\n  query Config {\n    config {\n      projectName\n      prefix\n      areas {\n        path\n        name\n        description\n        color\n        depth\n      }\n    }\n  }\n": typeof types.ConfigDocument,
    "\n  query UpdateStatus {\n    updateStatus {\n      current\n      latest\n      updateAvailable\n    }\n  }\n": typeof types.UpdateStatusDocument,
    "\n  query NibDetail($id: ID!) {\n    nib(id: $id) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      body\n      documents\n      etag\n      parent {\n        id\n        title\n        type\n        status\n      }\n      children(sort: { field: ORDER, direction: ASC }) {\n        id\n        title\n        type\n        status\n      }\n      blocking {\n        id\n        title\n        type\n        status\n      }\n      blockedBy {\n        id\n        title\n        type\n        status\n      }\n      mentions {\n        id\n        title\n        type\n        status\n      }\n      mentionedBy {\n        id\n        title\n        type\n        status\n      }\n    }\n  }\n": typeof types.NibDetailDocument,
    "\n  query NibConflictSnapshot($id: ID!) {\n    nib(id: $id) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      body\n      etag\n    }\n  }\n": typeof types.NibConflictSnapshotDocument,
    "\n  mutation UpdateNib($id: ID!, $input: UpdateNibInput!) {\n    updateNib(id: $id, input: $input) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      etag\n    }\n  }\n": typeof types.UpdateNibDocument,
    "\n  mutation DeleteNib($id: ID!) {\n    deleteNib(id: $id)\n  }\n": typeof types.DeleteNibDocument,
    "\n  mutation ArchiveNib($id: ID!) {\n    archiveNib(id: $id)\n  }\n": typeof types.ArchiveNibDocument,
    "\n  mutation CreateNib($input: CreateNibInput!) {\n    createNib(input: $input) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      body\n      etag\n      parentId\n      order\n    }\n  }\n": typeof types.CreateNibDocument,
    "\n  mutation SetParent($id: ID!, $parentId: String) {\n    setParent(id: $id, parentId: $parentId) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      etag\n      parentId\n    }\n  }\n": typeof types.SetParentDocument,
    "\n  mutation ReorderNib($id: ID!, $afterId: ID, $beforeId: ID, $first: Boolean, $parentId: String, $scope: OrderScope) {\n    reorderNib(id: $id, afterId: $afterId, beforeId: $beforeId, first: $first, parentId: $parentId, scope: $scope) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      etag\n      parentId\n      order\n    }\n  }\n": typeof types.ReorderNibDocument,
    "\n  query TreeTable($filter: NibFilter) {\n    nibs(filter: $filter, sort: { field: ORDER, direction: ASC }) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      createdAt\n      updatedAt\n      parentId\n      milestone\n      milestoneOrder\n      area\n      blockingIds\n      blockedByIds\n      etag\n    }\n  }\n": typeof types.TreeTableDocument,
    "\n  query SearchNibs($search: String!) {\n    nibs(filter: { search: $search }) {\n      id\n      title\n      type\n      status\n    }\n  }\n": typeof types.SearchNibsDocument,
    "\n  subscription NibChanged($id: ID) {\n    nibChanged(id: $id) {\n      type\n      nibId\n      nib {\n        id\n        title\n        status\n        type\n        priority\n        estimate\n        tags\n        body\n        etag\n        updatedAt\n        parentId\n        blockingIds\n        blockedByIds\n      }\n    }\n  }\n": typeof types.NibChangedDocument,
};
const documents: Documents = {
    "\n  query Config {\n    config {\n      projectName\n      prefix\n      areas {\n        path\n        name\n        description\n        color\n        depth\n      }\n    }\n  }\n": types.ConfigDocument,
    "\n  query UpdateStatus {\n    updateStatus {\n      current\n      latest\n      updateAvailable\n    }\n  }\n": types.UpdateStatusDocument,
    "\n  query NibDetail($id: ID!) {\n    nib(id: $id) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      body\n      documents\n      etag\n      parent {\n        id\n        title\n        type\n        status\n      }\n      children(sort: { field: ORDER, direction: ASC }) {\n        id\n        title\n        type\n        status\n      }\n      blocking {\n        id\n        title\n        type\n        status\n      }\n      blockedBy {\n        id\n        title\n        type\n        status\n      }\n      mentions {\n        id\n        title\n        type\n        status\n      }\n      mentionedBy {\n        id\n        title\n        type\n        status\n      }\n    }\n  }\n": types.NibDetailDocument,
    "\n  query NibConflictSnapshot($id: ID!) {\n    nib(id: $id) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      body\n      etag\n    }\n  }\n": types.NibConflictSnapshotDocument,
    "\n  mutation UpdateNib($id: ID!, $input: UpdateNibInput!) {\n    updateNib(id: $id, input: $input) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      etag\n    }\n  }\n": types.UpdateNibDocument,
    "\n  mutation DeleteNib($id: ID!) {\n    deleteNib(id: $id)\n  }\n": types.DeleteNibDocument,
    "\n  mutation ArchiveNib($id: ID!) {\n    archiveNib(id: $id)\n  }\n": types.ArchiveNibDocument,
    "\n  mutation CreateNib($input: CreateNibInput!) {\n    createNib(input: $input) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      body\n      etag\n      parentId\n      order\n    }\n  }\n": types.CreateNibDocument,
    "\n  mutation SetParent($id: ID!, $parentId: String) {\n    setParent(id: $id, parentId: $parentId) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      etag\n      parentId\n    }\n  }\n": types.SetParentDocument,
    "\n  mutation ReorderNib($id: ID!, $afterId: ID, $beforeId: ID, $first: Boolean, $parentId: String, $scope: OrderScope) {\n    reorderNib(id: $id, afterId: $afterId, beforeId: $beforeId, first: $first, parentId: $parentId, scope: $scope) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      etag\n      parentId\n      order\n    }\n  }\n": types.ReorderNibDocument,
    "\n  query TreeTable($filter: NibFilter) {\n    nibs(filter: $filter, sort: { field: ORDER, direction: ASC }) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      createdAt\n      updatedAt\n      parentId\n      milestone\n      milestoneOrder\n      area\n      blockingIds\n      blockedByIds\n      etag\n    }\n  }\n": types.TreeTableDocument,
    "\n  query SearchNibs($search: String!) {\n    nibs(filter: { search: $search }) {\n      id\n      title\n      type\n      status\n    }\n  }\n": types.SearchNibsDocument,
    "\n  subscription NibChanged($id: ID) {\n    nibChanged(id: $id) {\n      type\n      nibId\n      nib {\n        id\n        title\n        status\n        type\n        priority\n        estimate\n        tags\n        body\n        etag\n        updatedAt\n        parentId\n        blockingIds\n        blockedByIds\n      }\n    }\n  }\n": types.NibChangedDocument,
};

/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 *
 *
 * @example
 * ```ts
 * const query = graphql(`query GetUser($id: ID!) { user(id: $id) { name } }`);
 * ```
 *
 * The query argument is unknown!
 * Please regenerate the types.
 */
export function graphql(source: string): unknown;

/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query Config {\n    config {\n      projectName\n      prefix\n      areas {\n        path\n        name\n        description\n        color\n        depth\n      }\n    }\n  }\n"): (typeof documents)["\n  query Config {\n    config {\n      projectName\n      prefix\n      areas {\n        path\n        name\n        description\n        color\n        depth\n      }\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query UpdateStatus {\n    updateStatus {\n      current\n      latest\n      updateAvailable\n    }\n  }\n"): (typeof documents)["\n  query UpdateStatus {\n    updateStatus {\n      current\n      latest\n      updateAvailable\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query NibDetail($id: ID!) {\n    nib(id: $id) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      body\n      documents\n      etag\n      parent {\n        id\n        title\n        type\n        status\n      }\n      children(sort: { field: ORDER, direction: ASC }) {\n        id\n        title\n        type\n        status\n      }\n      blocking {\n        id\n        title\n        type\n        status\n      }\n      blockedBy {\n        id\n        title\n        type\n        status\n      }\n      mentions {\n        id\n        title\n        type\n        status\n      }\n      mentionedBy {\n        id\n        title\n        type\n        status\n      }\n    }\n  }\n"): (typeof documents)["\n  query NibDetail($id: ID!) {\n    nib(id: $id) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      body\n      documents\n      etag\n      parent {\n        id\n        title\n        type\n        status\n      }\n      children(sort: { field: ORDER, direction: ASC }) {\n        id\n        title\n        type\n        status\n      }\n      blocking {\n        id\n        title\n        type\n        status\n      }\n      blockedBy {\n        id\n        title\n        type\n        status\n      }\n      mentions {\n        id\n        title\n        type\n        status\n      }\n      mentionedBy {\n        id\n        title\n        type\n        status\n      }\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query NibConflictSnapshot($id: ID!) {\n    nib(id: $id) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      body\n      etag\n    }\n  }\n"): (typeof documents)["\n  query NibConflictSnapshot($id: ID!) {\n    nib(id: $id) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      body\n      etag\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation UpdateNib($id: ID!, $input: UpdateNibInput!) {\n    updateNib(id: $id, input: $input) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      etag\n    }\n  }\n"): (typeof documents)["\n  mutation UpdateNib($id: ID!, $input: UpdateNibInput!) {\n    updateNib(id: $id, input: $input) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      etag\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation DeleteNib($id: ID!) {\n    deleteNib(id: $id)\n  }\n"): (typeof documents)["\n  mutation DeleteNib($id: ID!) {\n    deleteNib(id: $id)\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation ArchiveNib($id: ID!) {\n    archiveNib(id: $id)\n  }\n"): (typeof documents)["\n  mutation ArchiveNib($id: ID!) {\n    archiveNib(id: $id)\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation CreateNib($input: CreateNibInput!) {\n    createNib(input: $input) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      body\n      etag\n      parentId\n      order\n    }\n  }\n"): (typeof documents)["\n  mutation CreateNib($input: CreateNibInput!) {\n    createNib(input: $input) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      body\n      etag\n      parentId\n      order\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation SetParent($id: ID!, $parentId: String) {\n    setParent(id: $id, parentId: $parentId) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      etag\n      parentId\n    }\n  }\n"): (typeof documents)["\n  mutation SetParent($id: ID!, $parentId: String) {\n    setParent(id: $id, parentId: $parentId) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      etag\n      parentId\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  mutation ReorderNib($id: ID!, $afterId: ID, $beforeId: ID, $first: Boolean, $parentId: String, $scope: OrderScope) {\n    reorderNib(id: $id, afterId: $afterId, beforeId: $beforeId, first: $first, parentId: $parentId, scope: $scope) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      etag\n      parentId\n      order\n    }\n  }\n"): (typeof documents)["\n  mutation ReorderNib($id: ID!, $afterId: ID, $beforeId: ID, $first: Boolean, $parentId: String, $scope: OrderScope) {\n    reorderNib(id: $id, afterId: $afterId, beforeId: $beforeId, first: $first, parentId: $parentId, scope: $scope) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      etag\n      parentId\n      order\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query TreeTable($filter: NibFilter) {\n    nibs(filter: $filter, sort: { field: ORDER, direction: ASC }) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      createdAt\n      updatedAt\n      parentId\n      milestone\n      milestoneOrder\n      area\n      blockingIds\n      blockedByIds\n      etag\n    }\n  }\n"): (typeof documents)["\n  query TreeTable($filter: NibFilter) {\n    nibs(filter: $filter, sort: { field: ORDER, direction: ASC }) {\n      id\n      title\n      status\n      type\n      priority\n      estimate\n      tags\n      createdAt\n      updatedAt\n      parentId\n      milestone\n      milestoneOrder\n      area\n      blockingIds\n      blockedByIds\n      etag\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  query SearchNibs($search: String!) {\n    nibs(filter: { search: $search }) {\n      id\n      title\n      type\n      status\n    }\n  }\n"): (typeof documents)["\n  query SearchNibs($search: String!) {\n    nibs(filter: { search: $search }) {\n      id\n      title\n      type\n      status\n    }\n  }\n"];
/**
 * The graphql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function graphql(source: "\n  subscription NibChanged($id: ID) {\n    nibChanged(id: $id) {\n      type\n      nibId\n      nib {\n        id\n        title\n        status\n        type\n        priority\n        estimate\n        tags\n        body\n        etag\n        updatedAt\n        parentId\n        blockingIds\n        blockedByIds\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription NibChanged($id: ID) {\n    nibChanged(id: $id) {\n      type\n      nibId\n      nib {\n        id\n        title\n        status\n        type\n        priority\n        estimate\n        tags\n        body\n        etag\n        updatedAt\n        parentId\n        blockingIds\n        blockedByIds\n      }\n    }\n  }\n"];

export function graphql(source: string) {
  return (documents as any)[source] ?? {};
}

export type DocumentType<TDocumentNode extends DocumentNode<any, any>> = TDocumentNode extends DocumentNode<  infer TType,  any>  ? TType  : never;