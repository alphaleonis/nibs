import { describe, it, expect } from "vitest";
import { relTokenValueContext } from "./relComplete";

// Helper: detect with the caret at the end of the text.
const at = (text: string) => relTokenValueContext(text, text.length);

describe("relTokenValueContext — recognizes a rel-id token value", () => {
  it("blocking:tni → blockingId + fragment 'tni' + replace range", () => {
    const ctx = relTokenValueContext("blocking:tni", 12);
    expect(ctx).toEqual({ field: "blockingId", name: "blocking", fragment: "tni", start: 9, end: 12 });
  });

  it("parent: (empty value) → parentId + empty fragment + zero-width range after the colon", () => {
    const ctx = relTokenValueContext("parent:", 7);
    expect(ctx).toEqual({ field: "parentId", name: "parent", fragment: "", start: 7, end: 7 });
  });

  it("mentioned-by:x (hyphenated field) → mentionedById + fragment 'x'", () => {
    const ctx = relTokenValueContext("mentioned-by:x", 14);
    expect(ctx).toEqual({ field: "mentionedById", name: "mentioned-by", fragment: "x", start: 13, end: 14 });
  });

  it("blocked-by:foo (hyphenated field) → blockedById", () => {
    expect(at("blocked-by:foo")?.field).toBe("blockedById");
  });

  it("mentions:foo → mentionsId", () => {
    expect(at("mentions:foo")?.field).toBe("mentionsId");
  });

  // The hierarchy tokens are ordinary rel-id tokens, so they get the same async
  // id/title typeahead for free — the detector reads REL_ID_FIELDS.
  it("ancestor:foo → ancestorId", () => {
    expect(at("ancestor:foo")?.field).toBe("ancestorId");
  });

  it("descendant:foo → descendantId", () => {
    expect(at("descendant:foo")?.field).toBe("descendantId");
  });

  it("sibling:tni → siblingId + fragment 'tni' + replace range", () => {
    const ctx = relTokenValueContext("sibling:tni", 11);
    expect(ctx).toEqual({ field: "siblingId", name: "sibling", fragment: "tni", start: 8, end: 11 });
  });

  // The assignment axis takes a nib id like the relationship tokens, so listing it
  // in REL_TOKEN_ORDER as an `id` entry is what gives it the same typeahead.
  it("milestone:tni → milestone + fragment 'tni' + replace range", () => {
    const ctx = relTokenValueContext("milestone:tni", 13);
    expect(ctx).toEqual({ field: "milestone", name: "milestone", fragment: "tni", start: 10, end: 13 });
  });

  it("takes the WHOLE post-colon run as the fragment even when the caret is mid-value", () => {
    // Caret between 'b' and 'c' of "abcd"; the scalar value has no comma split.
    const ctx = relTokenValueContext("parent:abcd", 9);
    expect(ctx).toEqual({ field: "parentId", name: "parent", fragment: "abcd", start: 7, end: 11 });
  });

  it("locates the token among several, not just the last", () => {
    const ctx = relTokenValueContext("type:bug parent:tn", 18);
    expect(ctx).toEqual({ field: "parentId", name: "parent", fragment: "tn", start: 16, end: 18 });
  });
});

describe("relTokenValueContext — returns null outside a rel-id value", () => {
  it("null in a metadata token (type:bug)", () => {
    expect(at("type:bug")).toBeNull();
  });

  it("null in free text", () => {
    expect(at("login")).toBeNull();
    expect(relTokenValueContext("", 0)).toBeNull();
  });

  it("null when the caret is in the field-name portion (before the colon)", () => {
    // Caret inside "parent", left of the colon.
    expect(relTokenValueContext("parent:x", 3)).toBeNull();
  });

  it("null when the caret sits exactly at the colon (field-name side)", () => {
    expect(relTokenValueContext("parent:x", 6)).toBeNull();
  });

  it("null for a negated token (negation is metadata-only)", () => {
    expect(at("-parent:x")).toBeNull();
  });

  it("null for an existence token (has:parent is not an id value)", () => {
    expect(at("has:parent")).toBeNull();
  });
});
