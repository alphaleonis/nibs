import { describe, it, expect } from "vitest";
import { SelectionState } from "./selection.svelte";
import { getActionTargetIds } from "./actionTarget";

// Real SelectionState so priority resolution behaves exactly as in production.
// The helper is the single bucket-safe target resolver shared by the keyboard
// shortcuts and the row context menu (nibs-oxaq).

describe("getActionTargetIds", () => {
  it("returns the multi-select set when hasMultiSelect", () => {
    const s = new SelectionState();
    s.select("a");
    s.toggleSelect("b");
    expect(s.hasMultiSelect).toBe(true);
    expect(getActionTargetIds(s, null).sort()).toEqual(["a", "b"]);
  });

  it("filters bucket ids out of the multi-select set (defense-in-depth)", () => {
    const s = new SelectionState();
    s.select("a");
    s.toggleSelect("b");
    // Inject a bucket id directly — SelectionState's writers normally reject it
    // (nibs-mn0t), so this forces the helper's own filter to be exercised.
    s.selectedIds = new Set(["a", "__no_milestone__", "b"]);
    expect(s.hasMultiSelect).toBe(true);
    expect(getActionTargetIds(s, "ctx").sort()).toEqual(["a", "b"]);
  });

  it("returns the focused row when there is no multi-select", () => {
    const s = new SelectionState();
    s.focus("focused");
    expect(getActionTargetIds(s, null)).toEqual(["focused"]);
  });

  it("does NOT return a bucket focused id — falls through to the context target", () => {
    const s = new SelectionState();
    s.focus("__no_epic__"); // focus() admits any id, including a bucket
    expect(getActionTargetIds(s, "ctx")).toEqual(["ctx"]);
  });

  it("does NOT return a bucket focused id when there is no context target", () => {
    const s = new SelectionState();
    s.focus("__no_epic__");
    expect(getActionTargetIds(s, null)).toEqual([]);
  });

  it("returns the context target when there is no multi-select and no focus", () => {
    const s = new SelectionState();
    expect(getActionTargetIds(s, "ctx")).toEqual(["ctx"]);
  });

  it("does NOT return a bucket context target", () => {
    const s = new SelectionState();
    expect(getActionTargetIds(s, "__no_feature_or_bug__")).toEqual([]);
  });

  it("returns an empty array when nothing is targetable", () => {
    const s = new SelectionState();
    expect(getActionTargetIds(s, null)).toEqual([]);
  });

  it("prefers the real focused row over the context target", () => {
    const s = new SelectionState();
    s.focus("focused");
    expect(getActionTargetIds(s, "ctx")).toEqual(["focused"]);
  });
});
