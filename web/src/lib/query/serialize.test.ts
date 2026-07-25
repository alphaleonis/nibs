import { describe, it, expect } from "vitest";
import { parseQuery, serializeQuery } from "./index";
import type { NibFilter } from "../types";

describe("serializeQuery", () => {
  it("emits a status token for a single status value", () => {
    expect(serializeQuery({ status: ["todo"] })).toBe("status:todo");
  });

  it("emits one status token per value, in order", () => {
    expect(serializeQuery({ status: ["todo", "in-progress"] })).toBe("status:todo status:in-progress");
  });

  it("emits the free-text search last, after status tokens", () => {
    expect(serializeQuery({ status: ["todo"], search: "login" })).toBe("status:todo login");
  });

  it("emits bare search when there is no status", () => {
    expect(serializeQuery({ search: "login flow" })).toBe("login flow");
  });

  it("returns an empty string for an empty filter", () => {
    expect(serializeQuery({})).toBe("");
  });

  it("ignores non-box fields (type/priority/etc.)", () => {
    const filter: NibFilter = { status: ["todo"], search: "login", type: ["bug"], priority: ["high"] };
    expect(serializeQuery(filter)).toBe("status:todo login");
  });
});

describe("round-trip identity for canonical inputs", () => {
  const canonical = [
    "status:todo",
    "status:todo status:in-progress",
    "status:todo login",
    "status:todo login flow",
    "login flow",
    "",
  ];

  for (const s of canonical) {
    it(`serializeQuery(parseQuery(${JSON.stringify(s)})) === ${JSON.stringify(s)}`, () => {
      expect(serializeQuery(parseQuery(s))).toBe(s);
    });
  }
});
