import { describe, it, expect } from "vitest";
import { STATUSES, STATUS_WORKFLOW, STATUS_WORKFLOW_ORDER } from "./constants";

describe("STATUS_WORKFLOW", () => {
  it("lists the transition flow, not the sort order", () => {
    // Spelled out rather than compared against STATUS_WORKFLOW_ORDER, which
    // would only prove the derivation copies whatever the literal says. The Go
    // side pins this same sequence (config.workflowStatusOrder) so the TUI and
    // web pickers walk one flow.
    expect(STATUS_WORKFLOW).toEqual([
      "draft",
      "todo",
      "in-progress",
      "completed",
      "deferred",
      "scrapped",
    ]);
  });

  it("offers every status exactly once", () => {
    // A picker is the only way to set a status in the web UI, so a status
    // missing here cannot be reached at all. This is what the derivation from
    // STATUSES buys: a status the flow forgets is appended, not dropped.
    expect([...STATUS_WORKFLOW].sort()).toEqual([...STATUSES].sort());
  });

  it("is a different order from STATUSES, which sorting and facets still use", () => {
    // The reason this constant exists rather than a reorder: STATUSES drives
    // the status-column sort and the facet checkboxes, so making it read as a
    // flow would silently re-sort the table.
    expect(STATUS_WORKFLOW).not.toEqual([...STATUSES]);
  });

  it("names only declared statuses in the literal sequence", () => {
    for (const s of STATUS_WORKFLOW_ORDER) {
      expect(STATUSES).toContain(s);
    }
  });
});
