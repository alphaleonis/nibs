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
 * How often the client pings the server to prove the socket still carries
 * traffic. graphql-ws defaults this to 0 — no ping at all — which is why a
 * silently dead socket used to stay "connected" forever (nibs-bcif). Idle cost
 * is one small frame each way per interval, so this is deliberately seconds
 * rather than sub-second.
 */
export const KEEP_ALIVE_MS = 10_000;

/**
 * How long a ping may go unanswered before the socket is declared dead. Sized
 * to absorb an ordinary slow round trip, not an outage — worst-case detection
 * is one KEEP_ALIVE_MS plus this.
 */
export const PONG_TIMEOUT_MS = 5_000;

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
 *
 * `keepAlive` plus the ping/pong handlers are the liveness probe (nibs-bcif).
 * They are two halves of one mechanism: a socket can die with no close frame —
 * going offline, or a laptop sleeping — and then every event the client waits on
 * simply never arrives, so it holds an open socket nothing will ever write to.
 * A ping is the only thing that turns that silence into evidence, and graphql-ws
 * states outright that NOTHING happens automatically when a pong does not come
 * back, so the timeout that acts on it has to live here.
 *
 * `terminate` rather than `socket.close()` (which graphql-ws's own README recipe
 * uses): closing negotiates a handshake with a peer that by definition is not
 * answering, whereas terminate synthesizes the close event immediately. That is
 * the difference between the disconnected chip appearing now and appearing
 * whenever the browser gives up on the handshake.
 */
export function wsClientOptions(
  url: string,
  hooks: LiveSocketHooks,
  terminate: () => void,
): ClientOptions {
  let pongTimeout: ReturnType<typeof setTimeout> | null = null;

  /**
   * Retire the pending timeout. It belongs to the one socket that was open when
   * its ping went out; letting it outlive that socket would terminate whatever
   * reconnected in the meantime, turning one dead connection into a kill loop.
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
        // `received` means the SERVER pinged us; graphql-ws auto-pongs that and
        // it says nothing about whether our own writes land. Only our outbound
        // ping starts a countdown, because only its pong can clear one.
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
  // Late-bound on purpose: the probe's timeout has to terminate the very client
  // whose options are being built. Safe because the callback can only run once
  // a socket exists, which is after createWsClient has returned.
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
