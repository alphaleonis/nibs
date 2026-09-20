/**
 * Reads the code and message off a GraphQL error: a typed urql `CombinedError`
 * on the mutation path, or the query store's `unknown` error on the read path.
 *
 * The server tags errors a client must route with a stable `extensions.code`;
 * `etagErrorPresenter` in cmd/serve.go defines which, so do not list them here.
 * Coded errors are ones the user can act on and are explained inline; uncoded
 * errors are presented as failures.
 */

/** The shape read off one entry of a CombinedError's `graphQLErrors`. */
interface GraphQLErrorLike {
  message?: unknown;
  extensions?: Record<string, unknown> | null;
}

/** The shape read off a urql CombinedError. */
interface CombinedErrorLike {
  message?: unknown;
  // `unknown`: nothing at runtime guarantees an array.
  graphQLErrors?: unknown;
}

function asCombinedErrorLike(error: unknown): CombinedErrorLike | undefined {
  return error && typeof error === "object" ? (error as CombinedErrorLike) : undefined;
}

/**
 * The error's GraphQL errors, dropping a non-array `graphQLErrors` and null
 * entries. Must not throw: the accessors run in a render-path `$derived`, and the
 * app declares no `<svelte:boundary>`.
 */
function graphQLErrorsOf(error: unknown): readonly GraphQLErrorLike[] {
  const errs = asCombinedErrorLike(error)?.graphQLErrors;
  if (!Array.isArray(errs)) return [];
  return errs.filter((e): e is GraphQLErrorLike => e != null && typeof e === "object");
}

/**
 * The first string `extensions.code` among the error's GraphQL errors. Undefined
 * means the server did not classify the error: treat it as generic rather than
 * guessing from the message.
 */
export function graphqlErrorCode(error: unknown): string | undefined {
  for (const gqlErr of graphQLErrorsOf(error)) {
    const code = gqlErr.extensions?.code;
    if (typeof code === "string") return code;
  }
  return undefined;
}

/** urql's aggregate message leads with this; the user must never read it. */
const TRANSPORT_PREFIX = "[GraphQL] ";

function withoutTransportPrefix(message: string): string {
  return message.startsWith(TRANSPORT_PREFIX) ? message.slice(TRANSPORT_PREFIX.length) : message;
}

/**
 * The first GraphQL error's message, else the CombinedError's. Use this for text
 * shown in the UI: urql prefixes its aggregate message with "[GraphQL] ".
 *
 * A plain STRING is accepted too, because the mutation path has already reduced
 * its failure to `CommandResult.error` — urql's aggregate, prefix and all — by
 * the time a caller has it. Both routes strip the prefix, so one helper answers
 * for the read path and the write path alike.
 */
export function graphqlErrorMessage(error: unknown): string {
  if (typeof error === "string") return withoutTransportPrefix(error);
  for (const gqlErr of graphQLErrorsOf(error)) {
    if (typeof gqlErr.message === "string" && gqlErr.message !== "") return gqlErr.message;
  }
  const combined = asCombinedErrorLike(error);
  return typeof combined?.message === "string" ? withoutTransportPrefix(combined.message) : "";
}
