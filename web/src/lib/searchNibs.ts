import type { Client } from "@urql/core";
import { SEARCH_NIBS_QUERY } from "./queries";
import type { NibSuggestion } from "./query/relComplete";

/** Fetch candidate nibs for a relationship-id token fragment. Injected into the
 *  Toolbar so tests can supply a fake instead of a real urql client. */
export type SearchNibsFn = (fragment: string) => Promise<NibSuggestion[]>;

/** Max candidate rows offered in the relationship-id typeahead. */
export const NIB_SEARCH_LIMIT = 8;

/**
 * Build a `SearchNibsFn` from a urql client. Runs the lean `SEARCH_NIBS_QUERY`
 * against the existing search resolver (network-only so suggestions reflect the
 * current store, not a stale cache), maps the hits to `NibSuggestion`, and caps
 * the list. A transport/GraphQL error resolves to an empty list — the typeahead
 * degrades to "no suggestions" rather than throwing under the cursor.
 */
export function createNibSearch(client: Client): SearchNibsFn {
  return async (fragment) => {
    const result = await client
      .query(SEARCH_NIBS_QUERY, { search: fragment }, { requestPolicy: "network-only" })
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
