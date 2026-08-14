import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { Kind, print } from "graphql";
import { NIB_CHANGED_SUBSCRIPTION } from "./queries";
import { getWebSocketUrl } from "./graphql";

// Mock graphql-ws so createClient doesn't open real WebSockets
vi.mock("graphql-ws", () => ({
  createClient: vi.fn(() => ({
    dispose: vi.fn(),
  })),
}));

describe("NIB_CHANGED_SUBSCRIPTION", () => {
  it("is a subscription operation selecting type, nibId, and nib fields", () => {
    // Verify it's a valid document with a subscription operation
    const doc = NIB_CHANGED_SUBSCRIPTION;
    expect(doc.kind).toBe(Kind.DOCUMENT);

    const opDef = doc.definitions.find(
      (d) => d.kind === Kind.OPERATION_DEFINITION,
    );
    expect(opDef).toBeDefined();
    expect(opDef!.kind).toBe(Kind.OPERATION_DEFINITION);
    if (opDef!.kind !== Kind.OPERATION_DEFINITION) throw new Error("unreachable");
    expect(opDef!.operation).toBe("subscription");

    // Verify the printed query includes the expected structure
    const query = print(doc);
    expect(query).toContain("subscription");
    expect(query).toContain("nibChanged");
    expect(query).toContain("type");
    expect(query).toContain("nibId");
    expect(query).toContain("nib");

    // Verify tree-table fields are selected on the nib
    for (const field of [
      "id",
      "title",
      "status",
      "type",
      "priority",
      "estimate",
      "tags",
      "updatedAt",
      "parentId",
      "blockingIds",
      "blockedByIds",
    ]) {
      expect(query).toContain(field);
    }
  });

  it("accepts an optional id variable for filtering", () => {
    const query = print(NIB_CHANGED_SUBSCRIPTION);
    // The subscription should accept $id: ID (optional)
    expect(query).toContain("$id: ID");
    expect(query).toContain("nibChanged(id: $id)");
  });
});

describe("getWebSocketUrl", () => {
  it("returns ws:// for http: origins", () => {
    expect(getWebSocketUrl("http://localhost:3000")).toBe(
      "ws://localhost:3000/graphql",
    );
  });

  it("returns wss:// for https: origins", () => {
    expect(getWebSocketUrl("https://example.com")).toBe(
      "wss://example.com/graphql",
    );
  });

  it("handles origins with trailing slash", () => {
    expect(getWebSocketUrl("http://localhost:8080/")).toBe(
      "ws://localhost:8080/graphql",
    );
  });
});

describe("createClient", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("creates a client that includes subscription exchange via graphql-ws", async () => {
    const { createClient: createGqlWsClient } = await import("graphql-ws");
    const { createClient } = await import("./graphql");

    const { client } = createClient();

    // Verify the urql client was created and has subscription support
    expect(client).toBeDefined();
    expect(typeof client.subscription).toBe("function");

    // Verify graphql-ws createClient was called (subscription exchange wiring)
    expect(createGqlWsClient).toHaveBeenCalled();
  });

  it("configures graphql-ws with reconnection options", async () => {
    const { createClient: createGqlWsClient } = await import("graphql-ws");
    const { createClient } = await import("./graphql");

    createClient();

    expect(createGqlWsClient).toHaveBeenCalledWith(
      expect.objectContaining({
        retryAttempts: expect.anything(),
      }),
    );
  });
});

// The socket must survive the events that killed it in the field (nibs-1seo).
// These assert the OPTIONS rather than a live socket, because the two defaults
// that caused the outage are both plain configuration.
describe("wsClientOptions", () => {
  it("retries indefinitely rather than giving up after graphql-ws's default 5", async () => {
    const { wsClientOptions } = await import("./graphql");
    expect(wsClientOptions("ws://x/graphql", {}, () => {}).retryAttempts).toBe(Infinity);
  });

  // graphql-ws's default shouldRetry is "only CloseEvents": ANY non-CloseEvent
  // connection problem is fatal and stops reconnection outright. The reported
  // failure was exactly that — `CombinedError: [Network] undefined` — so the
  // client never retried at all.
  it("treats a non-CloseEvent connection problem as retryable", async () => {
    const { wsClientOptions } = await import("./graphql");
    const opts = wsClientOptions("ws://x/graphql", {}, () => {});
    expect(opts.shouldRetry?.(new Error("[Network] undefined"))).toBe(true);
  });

  it("reports socket up and down to the hooks", async () => {
    const { wsClientOptions } = await import("./graphql");
    const onConnected = vi.fn();
    const onClosed = vi.fn();
    const opts = wsClientOptions("ws://x/graphql", { onConnected, onClosed }, () => {});

    opts.on?.connected?.({} as never, undefined, false);
    opts.on?.closed?.({});

    expect(onConnected).toHaveBeenCalledTimes(1);
    expect(onClosed).toHaveBeenCalledTimes(1);
  });
});

// The liveness probe (nibs-bcif). A socket can die without a close frame — go
// offline, or sleep a laptop — and then nothing the client waits on ever fires.
// Writing a ping is the only thing that turns that silence into an event, and
// graphql-ws deliberately does NOTHING when a pong fails to arrive, so the
// timeout that acts on the silence is ours to arm.
describe("wsClientOptions liveness probe", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  async function probe() {
    const { wsClientOptions, KEEP_ALIVE_MS, PONG_TIMEOUT_MS } = await import("./graphql");
    const terminate = vi.fn();
    const onClosed = vi.fn();
    return {
      opts: wsClientOptions("ws://x/graphql", { onClosed }, terminate),
      terminate,
      onClosed,
      KEEP_ALIVE_MS,
      PONG_TIMEOUT_MS,
    };
  }

  it("pings often enough to notice death, rarely enough to leave an idle tab alone", async () => {
    const { opts, KEEP_ALIVE_MS } = await probe();
    expect(opts.keepAlive).toBe(KEEP_ALIVE_MS);
    // graphql-ws's default is 0 — disabled — which is the bug.
    expect(opts.keepAlive).toBeGreaterThan(0);
    expect(opts.keepAlive).toBeGreaterThanOrEqual(5_000);
  });

  it("terminates a socket that never answers our ping", async () => {
    const { opts, terminate, PONG_TIMEOUT_MS } = await probe();

    opts.on?.ping?.(false, undefined);
    expect(terminate).not.toHaveBeenCalled();

    vi.advanceTimersByTime(PONG_TIMEOUT_MS);
    expect(terminate).toHaveBeenCalledTimes(1);
  });

  it("leaves a socket alone when the pong comes back", async () => {
    const { opts, terminate, PONG_TIMEOUT_MS } = await probe();

    opts.on?.ping?.(false, undefined);
    opts.on?.pong?.(true, undefined);

    vi.advanceTimersByTime(PONG_TIMEOUT_MS * 10);
    expect(terminate).not.toHaveBeenCalled();
  });

  // A ping FROM the server says nothing about whether our writes land — the
  // client auto-pongs it. Arming on it would start a countdown no pong of ours
  // can ever clear, killing a healthy socket every server keep-alive.
  it("ignores a ping sent by the server", async () => {
    const { opts, terminate, PONG_TIMEOUT_MS } = await probe();

    opts.on?.ping?.(true, undefined);

    vi.advanceTimersByTime(PONG_TIMEOUT_MS * 10);
    expect(terminate).not.toHaveBeenCalled();
  });

  // The pending timeout belongs to the socket that was open when the ping went
  // out. Once that socket is gone, firing it would terminate whatever
  // replaced it — turning one dead connection into an endless kill loop.
  it("retires a pending timeout when the socket goes away", async () => {
    const { opts, terminate, onClosed, PONG_TIMEOUT_MS } = await probe();

    opts.on?.ping?.(false, undefined);
    opts.on?.closed?.({});

    vi.advanceTimersByTime(PONG_TIMEOUT_MS * 10);
    expect(terminate).not.toHaveBeenCalled();
    expect(onClosed).toHaveBeenCalledTimes(1);
  });

  it("retires a pending timeout when a new socket comes up", async () => {
    const { opts, terminate, PONG_TIMEOUT_MS } = await probe();

    opts.on?.ping?.(false, undefined);
    opts.on?.connected?.({} as never, undefined, true);

    vi.advanceTimersByTime(PONG_TIMEOUT_MS * 10);
    expect(terminate).not.toHaveBeenCalled();
  });
});
