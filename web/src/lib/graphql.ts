import {
  Client,
  cacheExchange,
  fetchExchange,
  subscriptionExchange,
} from "@urql/svelte";
import { createClient as createWsClient } from "graphql-ws";

/**
 * Derive a WebSocket URL from an HTTP origin.
 * http: → ws://, https: → wss://, appends /graphql path.
 */
export function getWebSocketUrl(origin: string): string {
  const trimmed = origin.replace(/\/+$/, "");
  const wsUrl = trimmed.replace(/^http/, "ws");
  return `${wsUrl}/graphql`;
}

export function createClient(): Client {
  const wsClient = createWsClient({
    url: getWebSocketUrl(window.location.origin),
    retryAttempts: 5,
  });

  return new Client({
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
}
