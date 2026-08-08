import { describe, it, expect, beforeEach, vi } from "vitest";
import { savePreferences, loadPreferences, parseTheme, parsePerViewMap, parseColumnOrder } from "./storage";
import { parseQuery } from "./query";
import { MIN_DETAIL_PANEL_WIDTH, MIN_DETAIL_PANEL_HEIGHT, VIEW_LEVELS, ALL_COLUMN_KEYS } from "./types";

const store: Record<string, string> = {};
const mockStorage = {
  getItem: vi.fn((key: string) => store[key] ?? null),
  setItem: vi.fn((key: string, value: string) => { store[key] = value; }),
  removeItem: vi.fn((key: string) => { delete store[key]; }),
};

Object.defineProperty(globalThis, "localStorage", { value: mockStorage, writable: true });

describe("storage", () => {
  beforeEach(() => {
    for (const key of Object.keys(store)) delete store[key];
    vi.clearAllMocks();
  });

  it("saves and loads the query string with viewLevel", () => {
    savePreferences({ query: "type:bug login", viewLevel: "epics" });
    const loaded = loadPreferences();
    // theme resolves to the default ("graphite") when not persisted
    expect(loaded).toEqual({ query: "type:bug login", viewLevel: "epics", theme: "graphite" });
  });

  it("returns defaults when localStorage is empty", () => {
    const loaded = loadPreferences();
    expect(loaded).toEqual({ query: "", viewLevel: "none", theme: "graphite" });
  });

  it("returns defaults when localStorage has corrupt JSON", () => {
    store["nibs-filter-preferences"] = "not valid json{{{";
    const loaded = loadPreferences();
    expect(loaded).toEqual({ query: "", viewLevel: "none", theme: "graphite" });
  });

  it("returns defaults when localStorage has non-object value", () => {
    store["nibs-filter-preferences"] = '"just a string"';
    const loaded = loadPreferences();
    expect(loaded).toEqual({ query: "", viewLevel: "none", theme: "graphite" });
  });

  // The query is persisted as a STRING under the `q` key (mirroring the `?q=` URL
  // param), and it round-trips verbatim through save → load — including an
  // `exclude*` / `-status:` negation, which must survive untouched (nibs-grvv).
  it("persists the query under `q` and round-trips it (including -status:)", () => {
    savePreferences({ query: "-status:completed type:bug login", viewLevel: "none" });
    expect(JSON.parse(store["nibs-filter-preferences"]).q).toBe("-status:completed type:bug login");
    expect(loadPreferences().query).toBe("-status:completed type:bug login");
  });

  it("gracefully falls back to default when old viewMode value is stored", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      q: "old",
      viewMode: "hierarchy",
    });
    const loaded = loadPreferences();
    expect(loaded).toEqual({ query: "old", viewLevel: "none", theme: "graphite" });
  });

  // Legacy migration: an older build persisted the STRUCTURED `filter: NibFilter`
  // directly (before the query moved to string form). loadPreferences serializes
  // it to the equivalent canonical query string, so a returning user keeps their
  // filter and the load never crashes on old state.
  it("migrates a legacy structured filter to the equivalent canonical query string", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: { type: ["bug"], status: ["todo"], search: "auth" },
      viewLevel: "none",
    });
    // Canonical field order: type, status, then free text.
    expect(loadPreferences().query).toBe("type:bug status:todo auth");
  });

  // Faithfulness (nibs-grvv Phase-2 note): a legacy `excludeStatus` migrates to a
  // `-status:` EXCLUSION, NOT rewritten into a status include-list. The two hide
  // the same statuses, and keeping the exclude form is exactly what lets a
  // `-status:X` negation survive a reload untouched.
  it("migrates a legacy excludeStatus faithfully to a -status: exclusion (not an include-list)", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: { excludeStatus: ["completed", "scrapped"] },
      viewLevel: "none",
    });
    const loaded = loadPreferences();
    // STATUSES order → completed before scrapped.
    expect(loaded.query).toBe("-status:completed,scrapped");
    // Re-parses to the exclusion, with no include-list synthesized.
    const parsed = parseQuery(loaded.query);
    expect(parsed.filter.excludeStatus).toEqual(["completed", "scrapped"]);
    expect(parsed.filter.status).toBeUndefined();
  });

  it("does not crash migrating a garbage (non-object) legacy filter — yields an empty query", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: "not-an-object",
      viewLevel: "none",
    });
    expect(loadPreferences().query).toBe("");
  });

  it("gracefully handles unknown viewLevel value", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      q: "",
      viewLevel: "unknown-value",
    });
    const loaded = loadPreferences();
    expect(loaded).toEqual({ query: "", viewLevel: "none", theme: "graphite" });
  });

  it("accepts the renamed 'features' viewLevel", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "features",
    });
    const loaded = loadPreferences();
    expect(loaded.viewLevel).toBe("features");
  });

  it("enforces alwaysVisible columns in stored columnVisibility", () => {
    // Store column visibility that omits 'title' (which is alwaysVisible)
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      columnVisibility: {
        milestones: ["id", "type"],
      },
    });
    const loaded = loadPreferences();
    expect(loaded.columnVisibility?.milestones).toContain("title");
    expect(loaded.columnVisibility?.milestones).toContain("id");
    expect(loaded.columnVisibility?.milestones).toContain("type");
  });

  it("does not duplicate alwaysVisible columns if already present", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      columnVisibility: {
        milestones: ["id", "title", "type"],
      },
    });
    const loaded = loadPreferences();
    const titleCount = loaded.columnVisibility?.milestones?.filter(k => k === "title").length;
    expect(titleCount).toBe(1);
  });

  it("accepts and round-trips the blocking/blockedBy columns per view level", () => {
    // Opt-in columns toggled on for a specific view level must survive a
    // save → load round-trip (they are valid keys once in ALL_COLUMN_KEYS).
    savePreferences({
      query: "",
      viewLevel: "epics",
      columnVisibility: {
        epics: ["id", "title", "blocking", "blockedBy"],
      },
    });
    const loaded = loadPreferences();
    expect(loaded.columnVisibility?.epics).toContain("blocking");
    expect(loaded.columnVisibility?.epics).toContain("blockedBy");
    expect(loaded.columnVisibility?.epics).toContain("title");
  });

  it("saves and loads column widths per view level", () => {
    savePreferences({
      query: "",
      viewLevel: "milestones",
      columnWidths: {
        milestones: { id: 120, title: 500 },
      },
    });
    const loaded = loadPreferences();
    expect(loaded.columnWidths).toEqual({
      milestones: { id: 120, title: 500 },
    });
  });

  it("strips invalid column keys from columnWidths", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      columnWidths: {
        milestones: { id: 120, bogus: 200 },
      },
    });
    const loaded = loadPreferences();
    expect(loaded.columnWidths?.milestones).toEqual({ id: 120 });
    expect(loaded.columnWidths?.milestones).not.toHaveProperty("bogus");
  });

  it("strips non-positive and non-finite width values", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "epics",
      columnWidths: {
        epics: { id: -10, title: 0, type: Infinity, status: "not a number", estimate: 80 },
      },
    });
    const loaded = loadPreferences();
    expect(loaded.columnWidths?.epics).toEqual({ estimate: 80 });
  });

  it("returns undefined columnWidths when stored value is not an object", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      columnWidths: "invalid",
    });
    const loaded = loadPreferences();
    expect(loaded.columnWidths).toBeUndefined();
  });

  it("ignores view levels with array values in columnWidths", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      columnWidths: {
        milestones: [100, 200],
      },
    });
    const loaded = loadPreferences();
    expect(loaded.columnWidths).toBeUndefined();
  });

  it("saves and loads columnOrder per view level (a permutation round-trips)", () => {
    // A non-default order for milestones must survive save → load unchanged (it is
    // already a full permutation, so nothing is appended).
    const perm = [...ALL_COLUMN_KEYS].reverse();
    savePreferences({
      query: "",
      viewLevel: "milestones",
      columnOrder: { milestones: perm },
    });
    const loaded = loadPreferences();
    expect(loaded.columnOrder?.milestones).toEqual(perm);
  });

  it("appends a missing (newly-added) column to a stored partial columnOrder on load", () => {
    // A persisted order listing only a few keys must gain the rest (canonical
    // order) so every column still renders after a new column ships.
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "epics",
      columnOrder: { epics: ["title", "id"] },
    });
    const loaded = loadPreferences();
    const rest = ALL_COLUMN_KEYS.filter((k) => k !== "title" && k !== "id");
    expect(loaded.columnOrder?.epics).toEqual(["title", "id", ...rest]);
  });

  it("drops unknown keys from a stored columnOrder on load", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "epics",
      columnOrder: { epics: ["title", "bogus", "id"] },
    });
    const loaded = loadPreferences();
    expect(loaded.columnOrder?.epics).not.toContain("bogus");
    expect(loaded.columnOrder?.epics?.[0]).toBe("title");
    expect(loaded.columnOrder?.epics?.[1]).toBe("id");
  });

  it("returns undefined columnOrder when nothing is persisted", () => {
    savePreferences({ query: "",viewLevel: "milestones" });
    const loaded = loadPreferences();
    expect(loaded.columnOrder).toBeUndefined();
  });

  it("drops a non-array columnOrder level (garbage) so the map collapses to undefined", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "epics",
      columnOrder: { epics: "not-an-array" },
    });
    const loaded = loadPreferences();
    expect(loaded.columnOrder).toBeUndefined();
  });

  it("saves and loads detailPanelWidth", () => {
    savePreferences({
      query: "",
      viewLevel: "milestones",
      detailPanelWidth: 500,
    });
    const loaded = loadPreferences();
    expect(loaded.detailPanelWidth).toBe(500);
  });

  it("returns undefined detailPanelWidth when not set", () => {
    savePreferences({ query: "",viewLevel: "milestones" });
    const loaded = loadPreferences();
    expect(loaded.detailPanelWidth).toBeUndefined();
  });

  it("clamps detailPanelWidth to MIN/MAX bounds on load", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      detailPanelWidth: 50,
    });
    expect(loadPreferences().detailPanelWidth).toBe(MIN_DETAIL_PANEL_WIDTH);

    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      detailPanelWidth: 9999,
    });
    expect(loadPreferences().detailPanelWidth).toBe(9999);
  });

  it("strips invalid detailPanelWidth values", () => {
    const cases: [string, unknown][] = [
      ["negative", -100],
      ["zero", 0],
      ["Infinity", Infinity],
      ["NaN", NaN],
      ["string", "400"],
      ["null", null],
      ["object", { width: 400 }],
    ];
    for (const [label, value] of cases) {
      store["nibs-filter-preferences"] = JSON.stringify({
        filter: {},
        viewLevel: "milestones",
        detailPanelWidth: value,
      });
      const loaded = loadPreferences();
      expect(loaded.detailPanelWidth, `should strip ${label}`).toBeUndefined();
    }
  });

  it("saves and loads detailPanelPosition", () => {
    savePreferences({
      query: "",
      viewLevel: "milestones",
      detailPanelPosition: "bottom",
    });
    const loaded = loadPreferences();
    expect(loaded.detailPanelPosition).toBe("bottom");
  });

  it("returns undefined detailPanelPosition when not set", () => {
    savePreferences({ query: "",viewLevel: "milestones" });
    const loaded = loadPreferences();
    expect(loaded.detailPanelPosition).toBeUndefined();
  });

  it("returns undefined detailPanelPosition for an invalid stored value", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      detailPanelPosition: "diagonal",
    });
    const loaded = loadPreferences();
    expect(loaded.detailPanelPosition).toBeUndefined();
  });

  it("saves and loads openDetailOn", () => {
    savePreferences({
      query: "",
      viewLevel: "milestones",
      openDetailOn: "double",
    });
    const loaded = loadPreferences();
    expect(loaded.openDetailOn).toBe("double");
  });

  it("returns undefined openDetailOn when not set", () => {
    savePreferences({ query: "", viewLevel: "milestones" });
    const loaded = loadPreferences();
    expect(loaded.openDetailOn).toBeUndefined();
  });

  it("returns undefined openDetailOn for an invalid stored value", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      openDetailOn: "triple",
    });
    const loaded = loadPreferences();
    expect(loaded.openDetailOn).toBeUndefined();
  });

  it("saves and loads detailPanelHeight", () => {
    savePreferences({
      query: "",
      viewLevel: "milestones",
      detailPanelHeight: 400,
    });
    const loaded = loadPreferences();
    expect(loaded.detailPanelHeight).toBe(400);
  });

  it("returns undefined detailPanelHeight when not set", () => {
    savePreferences({ query: "",viewLevel: "milestones" });
    const loaded = loadPreferences();
    expect(loaded.detailPanelHeight).toBeUndefined();
  });

  it("clamps detailPanelHeight to MIN on load but applies no MAX clamp", () => {
    // Below MIN is raised to the floor...
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      detailPanelHeight: 50,
    });
    expect(loadPreferences().detailPanelHeight).toBe(MIN_DETAIL_PANEL_HEIGHT);

    // ...but there is intentionally no upper bound — large values load unchanged.
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      detailPanelHeight: 9999,
    });
    expect(loadPreferences().detailPanelHeight).toBe(9999);
  });

  it("strips invalid detailPanelHeight values", () => {
    const cases: [string, unknown][] = [
      ["negative", -100],
      ["zero", 0],
      ["Infinity", Infinity],
      ["NaN", NaN],
      ["string", "300"],
      ["null", null],
      ["object", { height: 300 }],
    ];
    for (const [label, value] of cases) {
      store["nibs-filter-preferences"] = JSON.stringify({
        filter: {},
        viewLevel: "milestones",
        detailPanelHeight: value,
      });
      const loaded = loadPreferences();
      expect(loaded.detailPanelHeight, `should strip ${label}`).toBeUndefined();
    }
  });

  it("saves and loads blockedEmphasis", () => {
    savePreferences({ query: "",viewLevel: "none", blockedEmphasis: "pill-dim" });
    expect(loadPreferences().blockedEmphasis).toBe("pill-dim");
  });

  it("returns undefined blockedEmphasis when not set", () => {
    savePreferences({ query: "",viewLevel: "none" });
    expect(loadPreferences().blockedEmphasis).toBeUndefined();
  });

  it("returns undefined blockedEmphasis for an invalid stored value", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "none",
      blockedEmphasis: "loud",
    });
    expect(loadPreferences().blockedEmphasis).toBeUndefined();
  });

  it("saves and loads fontSize", () => {
    savePreferences({ query: "",viewLevel: "none", fontSize: "large" });
    expect(loadPreferences().fontSize).toBe("large");
  });

  it("returns undefined fontSize when not set", () => {
    savePreferences({ query: "",viewLevel: "none" });
    expect(loadPreferences().fontSize).toBeUndefined();
  });

  it("returns undefined fontSize for an invalid stored value", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "none",
      fontSize: "gigantic",
    });
    expect(loadPreferences().fontSize).toBeUndefined();
  });

  it("saves and loads previewOpen", () => {
    savePreferences({ query: "",viewLevel: "none", previewOpen: false });
    expect(loadPreferences().previewOpen).toBe(false);
  });

  it("returns undefined previewOpen when not set", () => {
    savePreferences({ query: "",viewLevel: "none" });
    expect(loadPreferences().previewOpen).toBeUndefined();
  });

  it("returns undefined previewOpen for a non-boolean stored value", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "none",
      previewOpen: "yes",
    });
    expect(loadPreferences().previewOpen).toBeUndefined();
  });

  it("saves and loads tableSort (round-trip)", () => {
    savePreferences({ query: "",viewLevel: "flat", tableSort: { field: "modified", direction: "desc" } });
    const loaded = loadPreferences();
    expect(loaded.viewLevel).toBe("flat");
    expect(loaded.tableSort).toEqual({ field: "modified", direction: "desc" });
  });

  it("accepts any widened sortable field (round-trips a non-date column sort)", () => {
    for (const field of ["title", "id", "type", "status", "estimate", "tags", "blocking", "blockedBy", "parent"]) {
      store["nibs-filter-preferences"] = JSON.stringify({
        filter: {},
        viewLevel: "flat",
        tableSort: { field, direction: "asc" },
      });
      expect(loadPreferences().tableSort).toEqual({ field, direction: "asc" });
    }
  });

  it("returns undefined tableSort when not set", () => {
    savePreferences({ query: "",viewLevel: "none" });
    expect(loadPreferences().tableSort).toBeUndefined();
  });

  it("returns undefined tableSort for an unknown/removed field (e.g. priority — not a column)", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "flat",
      tableSort: { field: "priority", direction: "asc" },
    });
    expect(loadPreferences().tableSort).toBeUndefined();
  });

  it("returns undefined tableSort for an invalid direction", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "flat",
      tableSort: { field: "created", direction: "sideways" },
    });
    expect(loadPreferences().tableSort).toBeUndefined();
  });

  it("returns undefined tableSort for a non-object stored value", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "flat",
      tableSort: "created-asc",
    });
    expect(loadPreferences().tableSort).toBeUndefined();
  });

  it("accepts the 'flat' viewLevel", () => {
    store["nibs-filter-preferences"] = JSON.stringify({ filter: {}, viewLevel: "flat" });
    expect(loadPreferences().viewLevel).toBe("flat");
  });

  it("round-trips a persisted theme", () => {
    savePreferences({ query: "",viewLevel: "none", theme: "dracula" });
    expect(loadPreferences().theme).toBe("dracula");
  });

  it("defaults theme to graphite when not persisted", () => {
    savePreferences({ query: "",viewLevel: "none" });
    expect(loadPreferences().theme).toBe("graphite");
  });

  it("falls back to graphite for an invalid stored theme", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "none",
      theme: "solarized",
    });
    expect(loadPreferences().theme).toBe("graphite");
  });

  // One-time load migration for the status column's key rename (state → status).
  // Preferences persisted before the rename stored the status column under the key
  // "state" in the per-view visibility/order arrays, the per-view widths map, and
  // the active tableSort's `field`. On load each occurrence must surface as
  // "status" so the column keeps its persisted position, width, and sort — without
  // the migration the now-unknown "state" is dropped by the validators (visibility
  // / widths) or appended out of place (order), and a "state" tableSort is rejected.
  it("migrates a persisted 'state' column key to 'status' across visibility, order, widths, and tableSort", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      columnVisibility: { milestones: ["id", "title", "state"] },
      columnOrder: { milestones: ["state", "title", "id"] },
      columnWidths: { milestones: { state: 140, title: 400 } },
      tableSort: { field: "state", direction: "desc" },
    });
    const loaded = loadPreferences();
    // Visibility: renamed element present, legacy key gone.
    expect(loaded.columnVisibility?.milestones).toContain("status");
    expect(loaded.columnVisibility?.milestones).not.toContain("state");
    // Order: keeps the persisted POSITION (status stays first, not appended last).
    expect(loaded.columnOrder?.milestones?.[0]).toBe("status");
    expect(loaded.columnOrder?.milestones).not.toContain("state");
    // Widths: value preserved under the renamed key.
    expect(loaded.columnWidths?.milestones?.status).toBe(140);
    expect(loaded.columnWidths?.milestones).not.toHaveProperty("state");
    // tableSort: field renamed and still valid (status is a sortable column).
    expect(loaded.tableSort).toEqual({ field: "status", direction: "desc" });
  });

  // Same class of rename for the estimate column (effort → estimate). Preferences
  // persisted before the rename stored the estimate column under the key "effort"
  // in the per-view visibility/order arrays, the per-view widths map, and the
  // active tableSort's `field`. On load each occurrence must surface as "estimate"
  // so the column keeps its persisted position, width, and sort.
  it("migrates a persisted 'effort' column key to 'estimate' across visibility, order, widths, and tableSort", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      columnVisibility: { milestones: ["id", "title", "effort"] },
      columnOrder: { milestones: ["effort", "title", "id"] },
      columnWidths: { milestones: { effort: 70, title: 400 } },
      tableSort: { field: "effort", direction: "desc" },
    });
    const loaded = loadPreferences();
    // Visibility: renamed element present, legacy key gone.
    expect(loaded.columnVisibility?.milestones).toContain("estimate");
    expect(loaded.columnVisibility?.milestones).not.toContain("effort");
    // Order: keeps the persisted POSITION (estimate stays first, not appended last).
    expect(loaded.columnOrder?.milestones?.[0]).toBe("estimate");
    expect(loaded.columnOrder?.milestones).not.toContain("effort");
    // Widths: value preserved under the renamed key.
    expect(loaded.columnWidths?.milestones?.estimate).toBe(70);
    expect(loaded.columnWidths?.milestones).not.toHaveProperty("effort");
    // tableSort: field renamed and still valid (estimate is a sortable column).
    expect(loaded.tableSort).toEqual({ field: "estimate", direction: "desc" });
  });

  it("leaves preferences without a 'state' key untouched by the migration", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "epics",
      columnVisibility: { epics: ["id", "title", "status"] },
      columnOrder: { epics: ["status", "title", "id"] },
      columnWidths: { epics: { status: 120, title: 400 } },
      tableSort: { field: "modified", direction: "asc" },
    });
    const loaded = loadPreferences();
    expect(loaded.columnVisibility?.epics).toEqual(expect.arrayContaining(["id", "title", "status"]));
    expect(loaded.columnOrder?.epics?.[0]).toBe("status");
    expect(loaded.columnWidths?.epics).toEqual({ status: 120, title: 400 });
    expect(loaded.tableSort).toEqual({ field: "modified", direction: "asc" });
  });
});

// The shared per-view map parser: one VIEW_LEVELS loop with an injected
// per-level validator. Replaces the two hand-parallel parseColumn* skeletons;
// the per-level validators (visibility/widths) are exercised behaviorally via
// loadPreferences above, so here we pin the generic loop contract once with a
// stand-in validator (accept positive numbers).
describe("parsePerViewMap", () => {
  const positiveNumber = (raw: unknown): number | undefined =>
    typeof raw === "number" && raw > 0 ? raw : undefined;

  const cases: { name: string; raw: unknown; expected: unknown }[] = [
    { name: "non-object raw → undefined", raw: "nope", expected: undefined },
    { name: "null raw → undefined", raw: null, expected: undefined },
    { name: "array raw → undefined (no numeric view-level keys)", raw: [1, 2], expected: undefined },
    { name: "no level passes the validator → undefined", raw: { none: -1, epics: 0 }, expected: undefined },
    { name: "keeps only the levels the validator accepts", raw: { none: 5, epics: -1, milestones: 12 }, expected: { none: 5, milestones: 12 } },
    { name: "ignores keys that are not view levels", raw: { none: 5, bogus: 9 }, expected: { none: 5 } },
  ];

  for (const { name, raw, expected } of cases) {
    it(name, () => {
      expect(parsePerViewMap(raw, positiveNumber)).toEqual(expected);
    });
  }

  it("runs the validator once per view level", () => {
    const validate = vi.fn((raw: unknown) => (typeof raw === "number" ? raw : undefined));
    parsePerViewMap({ none: 1 }, validate);
    expect(validate).toHaveBeenCalledTimes(VIEW_LEVELS.length);
  });

  it("is generic over the value type (array validator, mirroring columnVisibility)", () => {
    const stringArray = (raw: unknown): string[] | undefined =>
      Array.isArray(raw) && raw.length > 0 ? (raw as string[]) : undefined;
    expect(parsePerViewMap({ epics: ["id", "title"], none: [] }, stringArray)).toEqual({
      epics: ["id", "title"],
    });
  });
});

// parseColumnOrder is the per-level validator for the columnOrder map: it drops
// unknown/duplicate keys, preserves the persisted order of valid keys, and
// APPENDS any missing ColumnKey in canonical order (so a newly-added column still
// appears). A non-array yields undefined so the level is dropped.
describe("parseColumnOrder", () => {
  it("returns undefined for a non-array input", () => {
    expect(parseColumnOrder("nope")).toBeUndefined();
    expect(parseColumnOrder(null)).toBeUndefined();
    expect(parseColumnOrder({ a: 1 })).toBeUndefined();
    expect(parseColumnOrder(42)).toBeUndefined();
  });

  it("returns the full canonical order for an empty array (all keys appended)", () => {
    expect(parseColumnOrder([])).toEqual([...ALL_COLUMN_KEYS]);
  });

  it("preserves a persisted partial order and appends the missing keys canonically", () => {
    const rest = ALL_COLUMN_KEYS.filter((k) => k !== "status" && k !== "title");
    expect(parseColumnOrder(["status", "title"])).toEqual(["status", "title", ...rest]);
  });

  it("drops unknown keys before appending the missing ones", () => {
    const rest = ALL_COLUMN_KEYS.filter((k) => k !== "id");
    expect(parseColumnOrder(["id", "bogus", 7, null])).toEqual(["id", ...rest]);
  });

  it("de-duplicates repeated keys (keeps the first occurrence)", () => {
    const rest = ALL_COLUMN_KEYS.filter((k) => k !== "title");
    expect(parseColumnOrder(["title", "title", "title"])).toEqual(["title", ...rest]);
  });

  it("round-trips a full permutation unchanged (nothing to append)", () => {
    const perm = [...ALL_COLUMN_KEYS].reverse();
    expect(parseColumnOrder(perm)).toEqual(perm);
  });

  it("always yields every ColumnKey exactly once regardless of input", () => {
    const out = parseColumnOrder(["modified", "modified", "bogus", "id"])!;
    expect([...out].sort()).toEqual([...ALL_COLUMN_KEYS].sort());
  });
});

describe("parseTheme", () => {
  it("passes through each valid theme", () => {
    expect(parseTheme("graphite")).toBe("graphite");
    expect(parseTheme("midnight")).toBe("midnight");
    expect(parseTheme("dracula")).toBe("dracula");
  });

  it("returns the default for missing/garbage/invalid values", () => {
    expect(parseTheme(undefined)).toBe("graphite");
    expect(parseTheme(null)).toBe("graphite");
    expect(parseTheme("")).toBe("graphite");
    expect(parseTheme("nope")).toBe("graphite");
    expect(parseTheme(42)).toBe("graphite");
    expect(parseTheme({})).toBe("graphite");
  });
});
