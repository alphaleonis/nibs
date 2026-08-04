/**
 * Reading the structured parts of a GraphQL error, for both the mutation path
 * (which holds a typed urql `CombinedError`) and the read path (where the query
 * store's `error` arrives as `unknown`).
 *
 * The server tags the failures a client must route STRUCTURALLY with a stable
 * `extensions.code` — "ETAG_MISMATCH", "NOT_FOUND" — and leaves every other
 * error uncoded (see `etagErrorPresenter` in cmd/serve.go). Reading the code is
 * therefore the difference between "the user asked for something that isn't
 * there" and "something broke", and the two want opposite presentation: an
 * inline, calm explanation versus a real error.
 *
 * Both accessors take `unknown` and narrow defensively rather than requiring a
 * `CombinedError`, because the read path never has that type in hand.
 */

/** The shape read off one entry of a CombinedError's `graphQLErrors`. */
interface GraphQLErrorLike {
  message?: unknown;
  extensions?: Record<string, unknown> | null;
}

/** The shape read off a urql CombinedError. */
interface CombinedErrorLike {
  message?: unknown;
  // `unknown`, not an array type: the accessors are handed whatever the caller's
  // error object carries, and declaring an array here would put the guarantee in
  // the type system where nothing at runtime enforces it.
  graphQLErrors?: unknown;
}

function asCombinedErrorLike(error: unknown): CombinedErrorLike | undefined {
  return error && typeof error === "object" ? (error as CombinedErrorLike) : undefined;
}

/**
 * The error's GraphQL errors, as far as they can be trusted: a non-array
 * `graphQLErrors` and null entries are dropped rather than iterated.
 *
 * urql's own CombinedError constructor builds a real array of real entries, so
 * nothing in the app reaches the fallback today. It exists because both
 * accessors run inside a Svelte `$derived` on the render path and the app
 * declares no `<svelte:boundary>`: a `TypeError` raised while classifying an
 * error would replace the list with a blank page instead of with that error.
 */
function graphQLErrorsOf(error: unknown): readonly GraphQLErrorLike[] {
  const errs = asCombinedErrorLike(error)?.graphQLErrors;
  if (!Array.isArray(errs)) return [];
  return errs.filter((e): e is GraphQLErrorLike => e != null && typeof e === "object");
}

/**
 * The first string `extensions.code` carried by any of the error's GraphQL
 * errors, or undefined when none carried one.
 *
 * Undefined is the normal case, not a failure: it means the server did not
 * classify this failure, so the caller must treat it as a generic error rather
 * than guessing from the message.
 */
export function graphqlErrorCode(error: unknown): string | undefined {
  for (const gqlErr of graphQLErrorsOf(error)) {
    const code = gqlErr.extensions?.code;
    if (typeof code === "string") return code;
  }
  return undefined;
}

/**
 * The server's own message for the error, preferring the first GraphQL error's
 * message over the CombinedError's.
 *
 * The two differ: urql prefixes its aggregate message with "[GraphQL] ", which
 * is a transport detail no user should read. Anything shown inline in the UI —
 * as opposed to logged — should come from here.
 */
export function graphqlErrorMessage(error: unknown): string {
  for (const gqlErr of graphQLErrorsOf(error)) {
    if (typeof gqlErr.message === "string" && gqlErr.message !== "") return gqlErr.message;
  }
  const combined = asCombinedErrorLike(error);
  return typeof combined?.message === "string" ? combined.message : "";
}
