import { describe, it, expect, vi, afterEach } from "vitest";
import type { Client } from "@urql/core";
import { createNibSearch, NIB_SEARCH_LIMIT } from "./searchNibs";
import { SEARCH_NIBS_QUERY } from "./queries";

// A fake urql `Client` whose `query(...).toPromise()` resolves to a fixed
// OperationResult. Only the `.query` method createNibSearch touches is stubbed;
// the cast keeps the rest of the Client surface out of the test.
function fakeClient(result: unknown) {
  const query = vi.fn().mockReturnValue({
    toPromise: () => Promise.resolve(result),
  });
  return { client: { query } as unknown as Client, query };
}

// A nib hit as the SEARCH_NIBS_QUERY resolver returns it (extra fields included
// to prove the mapping selects only the four NibSuggestion keys).
function hit(over: Record<string, unknown> = {}) {
  return {
    id: "tnib-abc1",
    title: "Fix login bug",
    type: "bug",
    status: "in-progress",
    // A field the query does not select — must be dropped by the mapping.
    body: "should not appear",
    ...over,
  };
}

describe("createNibSearch", () => {
  afterEach(() => vi.restoreAllMocks());

  it("maps each hit to a NibSuggestion (id/title/type/status only)", async () => {
    const { client } = fakeClient({
      data: {
        nibs: [
          hit({ id: "tnib-xyz9", title: "Login flow", type: "feature", status: "todo" }),
        ],
      },
    });

    const results = await createNibSearch(client)("log");

    expect(results).toEqual([
      { id: "tnib-xyz9", title: "Login flow", type: "feature", status: "todo" },
    ]);
    // The unselected `body` field is dropped — the mapping is an explicit pick.
    expect(results[0]).not.toHaveProperty("body");
  });

  it("caps the list at NIB_SEARCH_LIMIT (8) even when more hits come back", async () => {
    const many = Array.from({ length: 20 }, (_, i) => hit({ id: `tnib-${i}`, title: `Nib ${i}` }));
    const { client } = fakeClient({ data: { nibs: many } });

    const results = await createNibSearch(client)("nib");

    expect(NIB_SEARCH_LIMIT).toBe(8);
    expect(results).toHaveLength(8);
    // The first 8 (in order) are kept — it is a leading slice, not a sample.
    expect(results.map((r) => r.id)).toEqual([
      "tnib-0", "tnib-1", "tnib-2", "tnib-3", "tnib-4", "tnib-5", "tnib-6", "tnib-7",
    ]);
  });

  it("resolves to an empty list (no throw) when the result carries an error", async () => {
    // Silence the expected diagnostic so the test output stays clean.
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const { client } = fakeClient({ error: new Error("transport boom") });

    const search = createNibSearch(client);
    await expect(search("log")).resolves.toEqual([]);
    expect(warn).toHaveBeenCalled();
  });

  it("queries network-only, passing the fragment as the `search` variable", async () => {
    const { client, query } = fakeClient({ data: { nibs: [] } });

    await createNibSearch(client)("parent-frag");

    expect(query).toHaveBeenCalledTimes(1);
    expect(query).toHaveBeenCalledWith(
      SEARCH_NIBS_QUERY,
      { search: "parent-frag" },
      { requestPolicy: "network-only" },
    );
  });

  it("treats a missing `data.nibs` as no hits (empty list)", async () => {
    const { client } = fakeClient({ data: {} });

    await expect(createNibSearch(client)("x")).resolves.toEqual([]);
  });
});
