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
});

describe("serializeQuery — relationship + existence tokens (phase 5)", () => {
  it("emits a relationship-id scalar as field:id", () => {
    expect(serializeQuery({ filter: { blockingId: "tnib-9" } })).toBe("blocking:tnib-9");
  });

  it("uses hyphenated field-names for blocked-by / mentioned-by", () => {
    expect(serializeQuery({ filter: { blockedById: "tnib-1" } })).toBe("blocked-by:tnib-1");
    expect(serializeQuery({ filter: { mentionedById: "tnib-2" } })).toBe("mentioned-by:tnib-2");
  });

  it("emits an existence boolean as its fixed token", () => {
    expect(serializeQuery({ filter: { hasParent: true } })).toBe("has:parent");
    expect(serializeQuery({ filter: { isBlocked: true } })).toBe("is:blocked");
    // false is a SET value on the tri-state field, so it emits the no: spelling
    // rather than being omitted.
    expect(serializeQuery({ filter: { hasBlockedBy: false } })).toBe("no:blocked-by");
  });

  it("omits an existence field that is unset, but emits `no:` for an explicit false", () => {
    // The field is tri-state: undefined means "do not filter", false means
    // "filter for absence". Only the first is omitted — collapsing them would
    // reinstate the silent no-op the backend filter model just removed.
    expect(serializeQuery({ filter: {} })).toBe("");
    expect(serializeQuery({ filter: { hasParent: undefined } })).toBe("");
    expect(serializeQuery({ filter: { hasParent: false } })).toBe("no:parent");
  });

  it("places rel/existence tokens AFTER metadata and BEFORE free-text search", () => {
    const filter: NibFilter = { status: ["todo"], parentId: "x", search: "login" };
    expect(serializeQuery({ filter })).toBe("status:todo parent:x login");
  });

  it("orders rel/existence tokens by dimension: parent, blocking, blocked-by (+is:blocked), mentions, mentioned-by", () => {
    // Deliberately provide them out of canonical order to prove the fixed order wins.
    const filter: NibFilter = {
      mentionedById: "m2",
      isBlocked: true,
      blockingId: "b1",
      mentionsId: "m1",
      parentId: "p1",
    };
    expect(serializeQuery({ filter })).toBe(
      "parent:p1 blocking:b1 is:blocked mentions:m1 mentioned-by:m2",
    );
  });
});

describe("serializeQuery — hierarchy relationship tokens", () => {
  it("emits each hierarchy scalar as its own token", () => {
    expect(serializeQuery({ filter: { ancestorId: "tnib-1" } })).toBe("ancestor:tnib-1");
    expect(serializeQuery({ filter: { descendantId: "tnib-2" } })).toBe("descendant:tnib-2");
    expect(serializeQuery({ filter: { siblingId: "tnib-3" } })).toBe("sibling:tnib-3");
  });

  it("groups the hierarchy tokens with parent, before the blocking dimension", () => {
    // Placement in REL_TOKEN_ORDER is what makes the round-trip identity hold, so
    // pin it: the four hierarchy tokens form one contiguous block after parent's
    // has/no pair, and blocking still follows them. Supplied out of order on
    // purpose so only the fixed order can produce this string.
    const filter: NibFilter = {
      blockingId: "b1",
      siblingId: "s1",
      descendantId: "d1",
      ancestorId: "a1",
      hasParent: true,
      parentId: "p1",
    };
    expect(serializeQuery({ filter })).toBe(
      "parent:p1 has:parent ancestor:a1 descendant:d1 sibling:s1 blocking:b1",
    );
  });

  it("places the hierarchy tokens after metadata and before free-text search", () => {
    const filter: NibFilter = { status: ["todo"], ancestorId: "a1", search: "login" };
    expect(serializeQuery({ filter })).toBe("status:todo ancestor:a1 login");
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
    // a negated rel/existence token is parked as invalid (negation is metadata-only)
    // and preserved verbatim, so the user can see and edit what was rejected
    "-parent:tnib-1",
    "-ancestor:tnib-1",
    "-is:blocked",
    "type:bug login -ancestor:tnib-1",
    // relationship-id scalars (incl. hyphenated field-names)
    "parent:tnib-1",
    // hierarchy scalars — no has/no spellings exist for these
    "ancestor:tnib-1",
    "descendant:tnib-1",
    "sibling:tnib-1",
    "blocking:tnib-1",
    "blocked-by:tnib-1",
    "mentions:tnib-1",
    "mentioned-by:tnib-1",
    // existence/state booleans (incl. hyphenated + is:blocked)
    "has:parent",
    "no:parent",
    "has:blocking",
    "no:blocking",
    "has:blocked-by",
    "no:blocked-by",
    "is:blocked",
    // metadata + rel/existence + search interleaved, in canonical order
    "type:bug parent:tnib-1 has:blocking is:blocked mentions:tnib-2 login",
    // rel/existence "monster": every rel token in canonical dimension order
    // has: and no: for one dimension are two spellings of one tri-state field, so
    // a canonical string carries at most one of each pair — the old grammar let
    // you write "has:parent no:parent", which was self-contradictory. One of each
    // spelling appears below so both directions round-trip.
    "parent:tnib-1 has:parent blocking:tnib-2 no:blocking blocked-by:tnib-3 has:blocked-by is:blocked mentions:tnib-4 mentioned-by:tnib-5",
    // the same monster with the three hierarchy tokens in their canonical slot,
    // which only round-trips if REL_TOKEN_ORDER groups them right after parent
    "parent:tnib-1 has:parent ancestor:tnib-6 descendant:tnib-7 sibling:tnib-8 blocking:tnib-2 no:blocking blocked-by:tnib-3 has:blocked-by is:blocked mentions:tnib-4 mentioned-by:tnib-5",
    // hierarchy tokens interleaved with metadata + search, in canonical order
    "type:epic ancestor:tnib-1 descendant:tnib-2 sibling:tnib-3 login",
    // full monster: every field positive + negative, search, then two invalids
    "type:bug -type:task priority:high -priority:low status:todo -status:completed estimate:m -estimate:xl tags:auth -tags:wip login words status:banana -priority:pink",
  ];

  for (const s of canonical) {
    it(`round-trips ${JSON.stringify(s)}`, () => {
      expect(rt(s)).toBe(s);
    });
  }
});
