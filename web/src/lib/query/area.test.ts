import { describe, it, expect } from "vitest";
import { AREA_FIELD, isRefusedArea, recognizeArea } from "./area";
import type { AreaMatch } from "./area";
import {
  createAreaVocabulary,
  EMPTY_AREAS,
  LOADING_AREAS,
  UNAVAILABLE_AREAS,
} from "../areas";
import type { AreaNode, AreaVocabulary } from "../areas";

// `webhooks` is a SECOND ROOT whose path is a string prefix of `web`'s. It is in
// every fixture here on purpose: the closure `area:web` asks for is over the
// declared tree, and a string test would sweep it in.
const declared: AreaNode[] = [
  { path: "web", name: "web", description: "", color: "", depth: 0 },
  { path: "web/dashboard", name: "dashboard", description: "", color: "", depth: 1 },
  { path: "webhooks", name: "webhooks", description: "", color: "", depth: 0 },
];
const READY = createAreaVocabulary(declared);

describe("recognizeArea — the token shape, before any vocabulary", () => {
  // The whole shape axis. Recognition is pure: it decides WHICH tokens are area
  // tokens and what value each carries, and never whether that value is declared.
  const cases: { name: string; token: string; expected: AreaMatch | undefined }[] = [
    { name: "a plain path", token: "area:web", expected: { kind: "area", value: "web" } },
    {
      name: "a nested path",
      token: "area:web/dashboard",
      expected: { kind: "area", value: "web/dashboard" },
    },
    {
      name: "the field name case-insensitively",
      token: "AREA:web",
      expected: { kind: "area", value: "web" },
    },
    // Area names are only forbidden a `/` and outer whitespace (config.validateAreaNodes),
    // so these characters are part of a path rather than grammar. The value is the
    // whole post-colon run: scalar, never comma-split.
    {
      name: "a comma inside the value, taken whole",
      token: "area:a,b",
      expected: { kind: "area", value: "a,b" },
    },
    {
      name: "a second colon inside the value",
      token: "area:a:b",
      expected: { kind: "area", value: "a:b" },
    },
    {
      name: "the value's own case",
      token: "area:Web",
      expected: { kind: "area", value: "Web" },
    },
    // Negation has no `excludeArea` to write and must not reach free text, where
    // Bleve would read it as a MUST-NOT over an unindexed field and match everything.
    { name: "a negated token, parked", token: "-area:web", expected: { kind: "invalid", token: "-area:web" } },
    {
      name: "a negated token's field name, normalized",
      token: "-AREA:web",
      expected: { kind: "invalid", token: "-area:web" },
    },
    // Not area tokens at all — the caller routes these onward.
    { name: "an empty value", token: "area:", expected: undefined },
    { name: "a negated empty value", token: "-area:", expected: undefined },
    { name: "no colon", token: "area", expected: undefined },
    { name: "no field name", token: ":web", expected: undefined },
    { name: "a different field", token: "arena:web", expected: undefined },
    { name: "a field this one is a prefix of", token: "areas:web", expected: undefined },
    { name: "a relationship token", token: "parent:tnib-1", expected: undefined },
    { name: "a bare word", token: "web", expected: undefined },
  ];

  for (const { name, token, expected } of cases) {
    it(`${expected === undefined ? "declines" : "recognizes"} ${name} (${JSON.stringify(token)})`, () => {
      expect(recognizeArea(token)).toEqual(expected);
    });
  }

  it("names the field the same way every consumer spells it", () => {
    expect(AREA_FIELD).toBe("area");
  });
});

describe("isRefusedArea — the vocabulary axis", () => {
  // The other half of the input space: one question, asked of every vocabulary a
  // session can hold plus the caller that has none. Only a vocabulary that has
  // ANSWERED may refuse.
  const vocabularies: { name: string; areas: AreaVocabulary | undefined; refuses: boolean }[] = [
    { name: "a declared vocabulary", areas: READY, refuses: true },
    { name: "a project that declares none", areas: EMPTY_AREAS, refuses: true },
    { name: "a pre-load vocabulary", areas: LOADING_AREAS, refuses: false },
    { name: "a failed config query", areas: UNAVAILABLE_AREAS, refuses: false },
    { name: "no vocabulary at all", areas: undefined, refuses: false },
  ];

  for (const { name, areas, refuses } of vocabularies) {
    it(`${refuses ? "refuses" : "keeps"} an undeclared path with ${name}`, () => {
      expect(isRefusedArea("retired", areas)).toBe(refuses);
    });
  }

  it("never refuses a declared path", () => {
    for (const path of ["web", "web/dashboard", "webhooks"]) {
      expect(isRefusedArea(path, READY)).toBe(false);
    }
  });

  it("refuses a path that differs only in case, as the server would", () => {
    // config.findArea compares segment names byte-for-byte, so folding case here
    // would accept a value the store does not declare and the query would fail.
    expect(isRefusedArea("Web", READY)).toBe(true);
  });

  it("refuses a path that is only a string prefix of a declared one", () => {
    expect(isRefusedArea("we", READY)).toBe(true);
  });
});

describe("the token carries one path — closure is the server's", () => {
  // `area:web` selects web AND web/dashboard AND NOT webhooks. The selecting is
  // filterByAreaWithin's (internal/graph/filters.go); what this side owes is a
  // token that reaches it carrying the ancestor path unchanged, and a client-side
  // notion of "within" that agrees about the same three paths.
  it("does not widen the value to the subtree", () => {
    expect(recognizeArea("area:web")).toEqual({ kind: "area", value: "web" });
  });

  it("agrees with the server about what is within web", () => {
    expect(READY.subtreeOf("web").map((n) => n.path)).toEqual(["web", "web/dashboard"]);
  });
});
