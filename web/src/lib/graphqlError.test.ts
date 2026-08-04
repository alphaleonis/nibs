import { describe, it, expect } from "vitest";
import { graphqlErrorCode, graphqlErrorMessage } from "./graphqlError";

// Both accessors take `unknown` because the read path never holds a typed
// CombinedError. The rows that matter are therefore the malformed inputs: a
// throw here would take down the list render for an error it was asked to
// classify, and the app declares no <svelte:boundary> to catch it.
//
// A top-level non-object ("a string") returns early and never reaches the loop,
// so it is the malformed-`graphQLErrors` rows — a non-iterable value, a null
// entry — that exercise the narrowing.
describe("graphqlErrorCode", () => {
  const combined = {
    message: "[GraphQL] no nib with id \"zz\"",
    graphQLErrors: [{ message: "no nib with id \"zz\"", extensions: { code: "NOT_FOUND" } }],
  };

  it.each([
    ["a coded GraphQL error", combined, "NOT_FOUND"],
    ["the first coded error of several", {
      graphQLErrors: [{ extensions: {} }, { extensions: { code: "NOT_FOUND" } }],
    }, "NOT_FOUND"],
    ["an uncoded GraphQL error", { graphQLErrors: [{ message: "boom" }] }, undefined],
    ["a non-string code", { graphQLErrors: [{ extensions: { code: 404 } }] }, undefined],
    ["a network error with no graphQLErrors", { message: "Network error" }, undefined],
    ["undefined", undefined, undefined],
    ["a string", "boom", undefined],
    ["a non-iterable graphQLErrors", { graphQLErrors: {} }, undefined],
    ["a null entry", { graphQLErrors: [null, { extensions: { code: "NOT_FOUND" } }] }, "NOT_FOUND"],
    ["a non-object entry", { graphQLErrors: ["boom"] }, undefined],
  ])("%s", (_name, error, want) => {
    expect(graphqlErrorCode(error)).toBe(want);
  });
});

describe("graphqlErrorMessage", () => {
  // The prefix is the point: urql's aggregate message reads
  // "[GraphQL] <server message>", and anything rendered inline must show the
  // server's own wording without that transport detail.
  it("prefers the GraphQL error's message over urql's prefixed aggregate", () => {
    expect(
      graphqlErrorMessage({
        message: "[GraphQL] parentId filter: no nib with id \"zz\"",
        graphQLErrors: [{ message: "parentId filter: no nib with id \"zz\"" }],
      })
    ).toBe("parentId filter: no nib with id \"zz\"");
  });

  it.each([
    ["falls back to the aggregate message", { message: "Network error" }, "Network error"],
    ["skips an empty GraphQL message", {
      message: "Network error",
      graphQLErrors: [{ message: "" }],
    }, "Network error"],
    ["returns an empty string for a non-error", undefined, ""],
    ["falls back past a non-iterable graphQLErrors", {
      message: "Network error",
      graphQLErrors: {},
    }, "Network error"],
    ["skips a null entry", {
      graphQLErrors: [null, { message: "no nib with id \"zz\"" }],
    }, "no nib with id \"zz\""],
  ])("%s", (_name, error, want) => {
    expect(graphqlErrorMessage(error)).toBe(want);
  });
});
