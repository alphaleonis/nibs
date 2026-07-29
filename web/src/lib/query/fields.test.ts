import { describe, it, expect } from "vitest";
import { matchingGroup } from "./fields";
import type { FieldSpec } from "./fields";

describe("matchingGroup — set equality regardless of how a group is declared", () => {
  // A hand-built spec, not one of FIELD_SPECS: the two live groups are
  // duplicate-free today (and filter.test.ts pins both arrays verbatim, so a
  // duplicate would fail there first), which means the only way to exercise this
  // is to declare one. The check must be set equality on its own terms rather
  // than inheriting a precondition from whoever writes the next group.
  const spec: FieldSpec = {
    name: "status",
    filterKey: "status",
    excludeKey: "excludeStatus",
    values: ["draft", "todo", "in-progress", "deferred", "completed", "scrapped"],
    groups: new Map([["closed", ["deferred", "completed", "scrapped", "completed"]]]),
  };

  it("still matches the group's real member set when a member is repeated", () => {
    expect(matchingGroup(spec, ["deferred", "completed", "scrapped"])).toBe("closed");
  });

  it("does NOT match a superset that fits only because a member repeats", () => {
    // Comparing the raw members.length (4) against the unique value count (4)
    // passes both halves of the check here, so this labeled a four-status set
    // as `closed` — a set it is not.
    expect(matchingGroup(spec, ["deferred", "completed", "scrapped", "draft"])).toBeUndefined();
  });
});
