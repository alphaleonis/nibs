import { describe, it, expect } from "vitest";
import { parseQuery, serializeQuery } from "./index";
import type { NibFilter } from "../types";

// Round-trip a canonical string through parse → serialize.
const rt = (s: string) => serializeQuery(parseQuery(s));

describe("serializeQuery", () => {
  it("emits a single positive token for one value", () => {
    expect(serializeQuery({ filter: { status: ["todo"] } })).toBe("status:todo");
  });

  it("comma-joins multiple values within a field", () => {
    expect(serializeQuery({ filter: { status: ["todo", "in-progress"] } })).toBe("status:todo,in-progress");
  });

  it("orders enum values by declaration order, not input order (canonical-out)", () => {
    // STATUSES order is draft,todo,in-progress,... so todo precedes in-progress.
    expect(serializeQuery({ filter: { status: ["in-progress", "todo"] } })).toBe("status:todo,in-progress");
  });

  it("orders tag values alphabetically", () => {
    expect(serializeQuery({ filter: { tags: ["zebra", "apple"] } })).toBe("tags:apple,zebra");
  });

  it("emits the positive token before the negative for the same field", () => {
    expect(serializeQuery({ filter: { type: ["bug"], excludeType: ["task"] } })).toBe("type:bug -type:task");
  });

  it("emits fields in canonical order: type, priority, status, estimate, tags", () => {
    const filter: NibFilter = {
      tags: ["auth"],
      estimate: ["m"],
      status: ["todo"],
      priority: ["high"],
      type: ["bug"],
    };
    expect(serializeQuery({ filter })).toBe("type:bug priority:high status:todo estimate:m tags:auth");
  });

  it("emits search after the metadata tokens", () => {
    expect(serializeQuery({ filter: { status: ["todo"], search: "login" } })).toBe("status:todo login");
  });

  it("appends invalid tokens at the very end, after search", () => {
    expect(
      serializeQuery({ filter: { type: ["bug"], search: "login" }, invalidTokens: ["status:banana"] }),
    ).toBe("type:bug login status:banana");
  });

  it("returns an empty string for an empty filter", () => {
    expect(serializeQuery({ filter: {} })).toBe("");
  });

  it("ignores non-box NibFilter fields (relationships/existence)", () => {
    const filter: NibFilter = { status: ["todo"], hasParent: true, parentId: "x", noBlocking: true };
    expect(serializeQuery({ filter })).toBe("status:todo");
  });
});

describe("round-trip identity — serializeQuery(parseQuery(s)) === s", () => {
  const canonical = [
    "",
    "type:bug",
    "type:bug,feature",
    "-type:task",
    "type:bug -type:task",
    "priority:high",
    "status:todo,in-progress",
    "-status:completed",
    "estimate:m",
    "tags:apple,zebra",
    "-tags:wip",
    "type:bug priority:high status:todo estimate:m tags:auth",
    "type:bug login flow",
    "login flow",
    // a known-field token whose values are all empty (only commas) is kept as
    // free text, so it round-trips instead of vanishing.
    "type:,",
    "-type:,",
    // invalid tokens preserved through the round-trip
    "status:banana",
    "type:bug status:banana",
    "type:bug login status:banana",
    // full monster: every field positive + negative, search, then two invalids
    "type:bug -type:task priority:high -priority:low status:todo -status:completed estimate:m -estimate:xl tags:auth -tags:wip login words status:banana -priority:pink",
  ];

  for (const s of canonical) {
    it(`round-trips ${JSON.stringify(s)}`, () => {
      expect(rt(s)).toBe(s);
    });
  }
});
