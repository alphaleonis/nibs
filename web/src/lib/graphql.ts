import {
  Client,
  cacheExchange,
  fetchExchange,
  subscriptionExchange,
} from "@urql/svelte";
import {
  createClient as createWsClient,
  type Client as WsClient,
  type ClientOptions,
} from "graphql-ws";

/** Derive the `/graphql` WebSocket URL from an HTTP(S) origin. */
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
   * Drop the socket and re-establish every active subscription. A page restored
   * from the back/forward cache can hold a socket the browser already closed.
   */
  reconnect(): void;
}

/** Client ping interval. graphql-ws defaults to 0, which sends no pings. */
export const KEEP_ALIVE_MS = 10_000;

/** How long a ping may go unanswered before the socket is terminated. */
export const PONG_TIMEOUT_MS = 5_000;

/**
 * graphql-ws options for the live subscription socket.
 *
 * Retries without limit on any error: the default `shouldRetry` treats every
 * non-CloseEvent error as fatal, and a bfcache freeze produces one.
 *
 * graphql-ws does nothing when a pong never arrives, so the ping/pong handlers
 * terminate the socket after PONG_TIMEOUT_MS. A socket can die without a close
 * frame (offline, sleep), and a missing pong is the only sign. Use `terminate`,
 * which emits the close event immediately; `close()` waits on a closing
 * handshake with an unresponsive peer.
 */
export function wsClientOptions(
  url: string,
  hooks: LiveSocketHooks,
  terminate: () => void,
): ClientOptions {
  let pongTimeout: ReturnType<typeof setTimeout> | null = null;

  /**
   * Clear the pending pong timeout. Call on every connect and close, or a
   * timeout from a dead socket terminates its replacement.
   */
  const disarm = () => {
    if (pongTimeout !== null) {
      clearTimeout(pongTimeout);
      pongTimeout = null;
    }
  };

  return {
    url,
    retryAttempts: Infinity,
    shouldRetry: () => true,
    keepAlive: KEEP_ALIVE_MS,
    on: {
      connected: () => {
        disarm();
        hooks.onConnected?.();
      },
      closed: () => {
        disarm();
        hooks.onClosed?.();
      },
      ping: (received) => {
        // `received` is a server ping, which graphql-ws answers itself. Only our
        // own ping starts a countdown, because only its pong clears one.
        if (received) return;
        disarm();
        pongTimeout = setTimeout(() => {
          pongTimeout = null;
          terminate();
        }, PONG_TIMEOUT_MS);
      },
      pong: (received) => {
        if (received) disarm();
      },
    },
  };
}

export function createClient(hooks: LiveSocketHooks = {}): LiveClient {
  // Late-bound: the pong timeout terminates this client, and can only fire once a
  // socket exists, after createWsClient has returned.
  let wsClient: WsClient;
  wsClient = createWsClient(
    wsClientOptions(getWebSocketUrl(window.location.origin), hooks, () => wsClient.terminate()),
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
