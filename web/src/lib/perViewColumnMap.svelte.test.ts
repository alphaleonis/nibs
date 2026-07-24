import { describe, it, expect, vi } from "vitest";
import { PerViewColumnMap } from "./perViewColumnMap.svelte";
import type { ViewLevel } from "./types";

// A REPLACE-combinator instance (visibility/order semantics): the stored value
// is used whole; the default is a fresh copy when nothing is stored.
function makeReplaceMap(requestSave = vi.fn()) {
  const dflt = ["id", "title"];
  return new PerViewColumnMap<string[]>({
    storageKey: "columnVisibility",
    defaultValue: [...dflt],
    resolve: (stored, d) => stored ?? [...d],
    saveMode: "auto",
    requestSave,
  });
}

// A MERGE-combinator instance (widths semantics): the stored partial overlays
// the full default record. Keys are a finite literal union, mirroring ColumnKey
// so the spread combinator type-checks the same way the real widths instance does.
type WKey = "id" | "title" | "state";
function makeMergeMap(requestSave = vi.fn()) {
  const dflt: Record<WKey, number> = { id: 100, title: 400, state: 120 };
  return new PerViewColumnMap<Partial<Record<WKey, number>>, Record<WKey, number>>({
    storageKey: "columnWidths",
    defaultValue: { ...dflt },
    resolve: (stored, d) => ({ ...d, ...(stored ?? {}) }),
    saveMode: "flush",
    requestSave,
  });
}

describe("PerViewColumnMap", () => {
  describe("resolve — merge vs replace", () => {
    it("REPLACE returns a fresh copy of the default when a level is unset", () => {
      const map = makeReplaceMap();
      const resolved = map.resolve("none" as ViewLevel);
      expect(resolved).toEqual(["id", "title"]);
    });

    it("REPLACE returns the stored value whole (no default merged in)", () => {
      const map = makeReplaceMap();
      map.setLevel("milestones" as ViewLevel, ["id", "state"]);
      // Only the stored keys — the default ["id","title"] is NOT merged.
      expect(map.resolve("milestones" as ViewLevel)).toEqual(["id", "state"]);
    });

    it("MERGE overlays the stored partial over the FULL default record", () => {
      const map = makeMergeMap();
      map.setLevel("milestones" as ViewLevel, { id: 200 });
      const resolved = map.resolve("milestones" as ViewLevel);
      // id overridden; every other default key survives.
      expect(resolved).toEqual({ id: 200, title: 400, state: 120 });
    });

    it("MERGE returns the full default when a level is unset", () => {
      const map = makeMergeMap();
      expect(map.resolve("epics" as ViewLevel)).toEqual({ id: 100, title: 400, state: 120 });
    });

    it("REPLACE resolve does not hand out the shared default array (mutating it is isolated)", () => {
      const map = makeReplaceMap();
      const a = map.resolve("none" as ViewLevel);
      a.push("state");
      const b = map.resolve("none" as ViewLevel);
      expect(b).toEqual(["id", "title"]);
    });
  });

  describe("updateLevel — immutability", () => {
    it("rebuilds the outer map rather than mutating it in place", () => {
      const map = makeMergeMap();
      map.setLevel("none" as ViewLevel, { id: 100 });
      const before = map.serialize();
      map.updateLevel("none" as ViewLevel, (cur) => ({ ...(cur ?? {}), title: 500 }));
      const after = map.serialize();
      // A new outer object reference — the previous snapshot is untouched.
      expect(after).not.toBe(before);
      expect(before).toEqual({ none: { id: 100 } });
      expect(after).toEqual({ none: { id: 100, title: 500 } });
    });

    it("passes the current stored value (undefined when unset) to the updater", () => {
      const map = makeMergeMap();
      const seen: unknown[] = [];
      map.updateLevel("flat" as ViewLevel, (cur) => {
        seen.push(cur);
        return { id: 111 };
      });
      map.updateLevel("flat" as ViewLevel, (cur) => {
        seen.push(cur);
        return { ...(cur ?? {}), title: 222 };
      });
      expect(seen[0]).toBeUndefined();
      expect(seen[1]).toEqual({ id: 111 });
    });
  });

  describe("hydrate / serialize", () => {
    it("hydrate(undefined) yields an empty map", () => {
      const map = makeReplaceMap();
      map.hydrate(undefined);
      expect(map.serialize()).toEqual({});
    });

    it("hydrate seeds the map and peek reads a level's raw stored value", () => {
      const map = makeReplaceMap();
      map.hydrate({ epics: ["id"] });
      expect(map.serialize()).toEqual({ epics: ["id"] });
      expect(map.peek("epics" as ViewLevel)).toEqual(["id"]);
      expect(map.peek("none" as ViewLevel)).toBeUndefined();
    });
  });

  describe("flush", () => {
    it("flush() calls requestSave once", () => {
      const requestSave = vi.fn();
      const map = makeMergeMap(requestSave);
      map.flush();
      expect(requestSave).toHaveBeenCalledTimes(1);
    });
  });
});
