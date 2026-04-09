import { describe, it, expect, vi, beforeEach } from "vitest";
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

    const client = createClient();

    // Verify the urql client was created and has subscription support
    expect(client).toBeDefined();
    expect(typeof client.subscription).toBe("function");

    // Verify graphql-ws createClient was called (subscription exchange wiring)
    expect(createGqlWsClient).toHaveBeenCalled();
  });

  it("configures graphql-ws with retryAttempts for reconnection", async () => {
    const { createClient: createGqlWsClient } = await import("graphql-ws");
    const { createClient } = await import("./graphql");

    createClient();

    // graphql-ws should be configured with retryAttempts option
    expect(createGqlWsClient).toHaveBeenCalledWith(
      expect.objectContaining({
        retryAttempts: expect.anything(),
      }),
    );
  });
});
