import { describe, it, expect } from "vitest";
import { getCompletion } from "./index";

// Helper: complete with the caret at the end of the text.
const at = (text: string, tags: string[] = []) => getCompletion(text, text.length, tags);

describe("getCompletion — field names", () => {
  it("suggests field names for a partial token", () => {
    const c = at("ty");
    expect(c?.kind).toBe("field");
    expect(c?.items).toEqual(["type"]);
  });

  it("prefix-matches (t → type, tags)", () => {
    expect(at("t")?.items).toEqual(["type", "tags"]);
  });

  it("inserts the field with a trailing colon", () => {
    const c = at("ty");
    expect(c?.apply("type")).toEqual({ text: "type:", caret: 5 });
  });

  it("keeps the negation prefix when completing a field name", () => {
    const c = at("-ty");
    expect(c?.kind).toBe("field");
    expect(c?.apply("type")).toEqual({ text: "-type:", caret: 6 });
  });

  it("returns null when no field name matches (it is a search word)", () => {
    expect(at("zzz")).toBeNull();
  });

  it("returns null for an empty token", () => {
    expect(getCompletion("", 0)).toBeNull();
    expect(getCompletion("type:bug ", 9)).toBeNull();
  });
});

describe("getCompletion — enum values", () => {
  it("suggests all values right after the colon", () => {
    const c = at("type:");
    expect(c?.kind).toBe("value");
    expect(c?.items).toEqual(["milestone", "epic", "bug", "feature", "task", "research"]);
  });

  it("substring-filters values by the partial", () => {
    const c = at("status:in");
    expect(c?.items).toEqual(["in-progress"]);
  });

  it("inserts the value, preserving the field", () => {
    const c = at("type:bu");
    expect(c?.apply("bug")).toEqual({ text: "type:bug", caret: 8 });
  });

  it("completes the segment after the last comma and excludes already-chosen values", () => {
    const c = at("status:todo,in");
    expect(c?.items).toEqual(["in-progress"]);
    expect(c?.apply("in-progress")).toEqual({ text: "status:todo,in-progress", caret: 23 });
  });

  it("does not re-suggest a value already present earlier in the token", () => {
    const c = at("type:bug,");
    expect(c?.items).not.toContain("bug");
    expect(c?.items).toContain("feature");
  });

  it("returns null for an unknown field", () => {
    expect(at("title:fo")).toBeNull();
  });
});

describe("getCompletion — tags", () => {
  it("suggests from availableTags, substring-filtered", () => {
    const c = at("tags:fr", ["frontend", "backend"]);
    expect(c?.kind).toBe("tag");
    expect(c?.items).toEqual(["frontend"]);
  });

  it("returns null when no available tag matches", () => {
    expect(at("tags:zzz", ["frontend"])).toBeNull();
  });

  it("inserts the chosen tag", () => {
    const c = at("tags:", ["frontend", "backend"]);
    expect(c?.apply("backend")).toEqual({ text: "tags:backend", caret: 12 });
  });
});

describe("getCompletion — status group shortcuts", () => {
  it("offers the group names ahead of the concrete statuses", () => {
    const c = at("status:");
    expect(c?.kind).toBe("value");
    expect(c?.items).toEqual([
      "open",
      "closed",
      "draft",
      "todo",
      "in-progress",
      "deferred",
      "completed",
      "scrapped",
    ]);
  });

  it("substring-filters the group names like any other value", () => {
    expect(at("status:ope")?.items).toEqual(["open"]);
    expect(at("status:clos")?.items).toEqual(["closed"]);
  });

  it("inserts the chosen group name", () => {
    expect(at("status:op")?.apply("open")).toEqual({ text: "status:open", caret: 11 });
  });

  it("does not re-suggest a group already chosen in the token", () => {
    expect(at("status:open,")?.items).not.toContain("open");
  });

  it("offers no group names for a field that has none", () => {
    expect(at("type:")?.items).toEqual(["milestone", "epic", "bug", "feature", "task", "research"]);
  });
});
