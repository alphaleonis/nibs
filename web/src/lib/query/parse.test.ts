import { describe, it, expect } from "vitest";
import { parseQuery } from "./index";
import type { ParsedQuery } from "./index";

describe("parseQuery", () => {
  const cases: { name: string; input: string; expected: ParsedQuery }[] = [
    { name: "a lone status token → status include-list", input: "status:todo", expected: { status: ["todo"] } },
    { name: "bare words → search", input: "login page", expected: { search: "login page" } },
    { name: "mixed token + bare words → both", input: "login status:todo", expected: { status: ["todo"], search: "login" } },
    { name: "token before and after bare words", input: "status:todo login flow", expected: { status: ["todo"], search: "login flow" } },
    { name: "empty string → empty filter", input: "", expected: {} },
    { name: "whitespace only → empty filter", input: "   ", expected: {} },
    {
      name: "repeated status tokens union (lenient-in)",
      input: "status:todo status:in-progress",
      expected: { status: ["todo", "in-progress"] },
    },
    { name: "field name is case-insensitive", input: "STATUS:todo", expected: { status: ["todo"] } },
    { name: "value is lowercased", input: "status:TODO", expected: { status: ["todo"] } },
    { name: "mixed case field and value", input: "Status:In-Progress", expected: { status: ["in-progress"] } },
    { name: "collapses runs of whitespace in search", input: "login    flow", expected: { search: "login flow" } },
    {
      name: "unknown field token stays as free-text (only status is special in phase 1)",
      input: "type:bug",
      expected: { search: "type:bug" },
    },
    {
      name: "empty status value is not a token — kept as free text",
      input: "status:",
      expected: { search: "status:" },
    },
  ];

  for (const { name, input, expected } of cases) {
    it(name, () => {
      expect(parseQuery(input)).toEqual(expected);
    });
  }

  it("omits the status key entirely when no status token is present", () => {
    expect(parseQuery("login")).not.toHaveProperty("status");
  });

  it("omits the search key entirely when only a status token is present", () => {
    expect(parseQuery("status:todo")).not.toHaveProperty("search");
  });
});
