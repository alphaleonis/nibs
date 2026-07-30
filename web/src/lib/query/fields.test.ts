import { describe, it, expect } from "vitest";
import { collapseToTokens, fieldSpec } from "./fields";
import type { FieldSpec } from "./fields";

describe("collapseToTokens — a group collapses wherever its members all appear", () => {
  // A hand-built spec, not one of FIELD_SPECS: the two live groups are
  // duplicate-free today (and filter.test.ts pins both arrays verbatim, so a
  // duplicate would fail there first), which means the only way to exercise the
  // repeated-member case is to declare one. The check must stand on its own
  // terms rather than inheriting a precondition from whoever writes the next group.
  const spec: FieldSpec = {
    name: "status",
    filterKey: "status",
    excludeKey: "excludeStatus",
    values: ["draft", "todo", "in-progress", "deferred", "completed", "scrapped"],
    groups: new Map([["closed", ["deferred", "completed", "scrapped", "completed"]]]),
  };

  it("collapses the group's real member set when a member is repeated", () => {
    expect(collapseToTokens(spec, ["deferred", "completed", "scrapped"])).toEqual(["closed"]);
  });

  // The whole point of subset collapse: the extra value must SURVIVE alongside
  // the group name. Emitting just `closed` here would silently widen a filter
  // that also asked for drafts into one that excludes them.
  it("keeps values outside the group instead of swallowing them", () => {
    expect(collapseToTokens(spec, ["deferred", "completed", "scrapped", "draft"])).toEqual([
      "draft",
      "closed",
    ]);
  });

  it("leaves a partial group spelled out", () => {
    expect(collapseToTokens(spec, ["deferred", "completed"])).toEqual(["deferred", "completed"]);
  });
});

describe("collapseToTokens — against the live status spec", () => {
  const status = fieldSpec("status")!;

  it("collapses each live group", () => {
    expect(collapseToTokens(status, ["draft", "todo", "in-progress"])).toEqual(["open"]);
    expect(collapseToTokens(status, ["deferred", "completed", "scrapped"])).toEqual(["closed"]);
  });

  // The reported case: `open` must survive a list that also names a closed status.
  it("collapses open and keeps a trailing closed status", () => {
    expect(collapseToTokens(status, ["draft", "todo", "in-progress", "deferred"])).toEqual([
      "open",
      "deferred",
    ]);
  });

  it("collapses both groups when every status is present", () => {
    expect(collapseToTokens(status, ["draft", "todo", "in-progress", "deferred", "completed", "scrapped"])).toEqual([
      "open",
      "closed",
    ]);
  });

  // Ordering is by the LOWEST declaration index a token covers, so `open`
  // (draft, index 0) precedes `completed` (index 4) regardless of input order.
  it("orders tokens by the lowest index each covers, whatever order they arrive in", () => {
    expect(collapseToTokens(status, ["completed", "in-progress", "draft", "todo"])).toEqual([
      "open",
      "completed",
    ]);
  });

  it("deduplicates", () => {
    expect(collapseToTokens(status, ["todo", "todo", "draft"])).toEqual(["draft", "todo"]);
  });
});

describe("collapseToTokens — fields without groups", () => {
  it("never collapses an enum that declares no groups", () => {
    const type = fieldSpec("type")!;
    expect(collapseToTokens(type, ["milestone", "epic", "bug", "feature", "task", "research"])).toEqual([
      "milestone",
      "epic",
      "bug",
      "feature",
      "task",
      "research",
    ]);
  });

  it("orders tags alphabetically, matching the free-form ordering rule", () => {
    const tags = fieldSpec("tags")!;
    expect(collapseToTokens(tags, ["zeta", "alpha", "mid"])).toEqual(["alpha", "mid", "zeta"]);
  });
});
