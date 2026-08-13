import {
  Client,
  cacheExchange,
  fetchExchange,
  subscriptionExchange,
} from "@urql/svelte";
import { createClient as createWsClient, type ClientOptions } from "graphql-ws";

/**
 * Derive a WebSocket URL from an HTTP origin.
 * http: → ws://, https: → wss://, appends /graphql path.
 */
export function getWebSocketUrl(origin: string): string {
  const trimmed = origin.replace(/\/+$/, "");
  const wsUrl = trimmed.replace(/^http/, "ws");
  return `${wsUrl}/graphql`;
}

/** Socket up/down callbacks, wired to the connection-recovery core. */
export interface LiveSocketHooks {
  onConnected?: () => void;
  onClosed?: () => void;
}

/** A urql client plus the handle needed to force its socket to re-establish. */
export interface LiveClient {
  client: Client;
  /**
   * Drop the socket and re-establish every active subscription. Needed because
   * a page restored from the back/forward cache can resume believing its socket
   * is live when the browser closed it on freeze — so waiting for the client to
   * notice never happens.
   */
  reconnect(): void;
}

/**
 * graphql-ws options for the live subscription socket.
 *
 * Both retry settings are deliberate and load-bearing (nibs-1seo). The defaults
 * are `retryAttempts: 5` and a `shouldRetry` of "only CloseEvents" — and the
 * latter classifies ANY non-CloseEvent connection problem as fatal, stopping
 * reconnection outright rather than after five tries. A bfcache freeze produces
 * exactly such a problem, which is how the live subscription went permanently
 * deaf with nothing in the UI to say so.
 *
 * `retryWait` is intentionally left at its default (randomized exponential
 * backoff) — there is no reason to hand-roll one.
 */
export function wsClientOptions(url: string, hooks: LiveSocketHooks): ClientOptions {
  return {
    url,
    retryAttempts: Infinity,
    shouldRetry: () => true,
    on: {
      connected: () => hooks.onConnected?.(),
      closed: () => hooks.onClosed?.(),
    },
  };
}

export function createClient(hooks: LiveSocketHooks = {}): LiveClient {
  const wsClient = createWsClient(
    wsClientOptions(getWebSocketUrl(window.location.origin), hooks),
  );

  const client = new Client({
    url: "/graphql",
    exchanges: [
      cacheExchange,
      fetchExchange,
      subscriptionExchange({
        forwardSubscription(request) {
          const input = { ...request, query: request.query || "" };
          return {
            subscribe(sink) {
              const unsubscribe = wsClient.subscribe(input, sink);
              return { unsubscribe };
            },
          };
        },
      }),
    ],
  });

  return { client, reconnect: () => wsClient.terminate() };
}
