import type { Client } from "@urql/core";
import { SEARCH_NIBS_QUERY } from "./queries";
import type { NibSuggestion } from "./query/relComplete";

/** Fetch candidate nibs for a relationship-id token fragment, optionally narrowed
 *  to the nib types that token accepts. Toolbar accepts one as a prop so tests
 *  can inject a fake. */
export type SearchNibsFn = (
  fragment: string,
  types?: readonly string[],
) => Promise<NibSuggestion[]>;

/** Max candidate rows offered in the relationship-id typeahead. */
export const NIB_SEARCH_LIMIT = 8;

/**
 * Build a `SearchNibsFn` from a urql client. Network-only, capped at
 * NIB_SEARCH_LIMIT; a GraphQL error resolves to an empty list.
 *
 * `types` goes on the wire, so the cap below trims a list the server already
 * narrowed — nothing here filters by type.
 */
export function createNibSearch(client: Client): SearchNibsFn {
  return async (fragment, types) => {
    const result = await client
      .query(
        SEARCH_NIBS_QUERY,
        // Copied: the generated variables type is mutable, the constraint readonly.
        { search: fragment, types: types ? [...types] : undefined },
        { requestPolicy: "network-only" },
      )
      .toPromise();
    if (result.error) {
      console.warn("nib search query error:", result.error);
      return [];
    }
    const nibs = result.data?.nibs ?? [];
    return nibs
      .slice(0, NIB_SEARCH_LIMIT)
      .map((n) => ({ id: n.id, title: n.title, type: n.type, status: n.status }));
  };
}
