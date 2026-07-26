import { render, screen, within, fireEvent } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { tick } from "svelte";
import Toolbar from "./Toolbar.svelte";
import { Preferences } from "../preferences.svelte";
import { ALL_COLUMN_KEYS, DEFAULT_VISIBLE_COLUMNS } from "../types";
import { OPEN_STATUSES, OPEN_PLUS_DEFERRED_STATUSES } from "../constants";
import type { NibFilter, ViewLevel, ColumnKey } from "../types";
import type { NibSuggestion } from "../query";

// bits-ui scroll lock sets pointer-events: none on <body>, so disable the check
const user = userEvent.setup({ pointerEventsCheck: 0 });

const defaultToolbarProps = {
  filter: {} as NibFilter,
  onchange: vi.fn(),
  viewLevel: "milestones" as ViewLevel,
  onviewlevelchange: vi.fn(),
  visibleColumns: [...ALL_COLUMN_KEYS] as ColumnKey[],
  oncolumnschange: vi.fn(),
  oncreatenew: vi.fn(),
};

describe("Toolbar", () => {
  it("renders the title with the project name when projectName is provided", () => {
    render(Toolbar, { ...defaultToolbarProps, projectName: "test-project" });

    expect(screen.getByText("Nibs - test-project")).toBeInTheDocument();
  });

  it("renders the bare 'Nibs' title when no projectName is provided", () => {
    render(Toolbar, { ...defaultToolbarProps });

    const heading = screen.getByRole("heading", { level: 1 });
    expect(heading).toHaveTextContent(/^Nibs$/);
  });

  it("renders New button, keyword input, filter dropdowns, and view controls", () => {
    render(Toolbar, { ...defaultToolbarProps });

    expect(screen.getByRole("button", { name: "New item" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New item" })).toHaveTextContent("New");
    expect(screen.getByTestId("filter-keyword")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Settings" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Options" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Columns" })).toBeInTheDocument();
  });

  it("renders view selector button showing current view label", () => {
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "milestones" });
    expect(screen.getByRole("button", { name: /^Group by/ })).toHaveTextContent("Milestones");
  });

  it("shows 'Epics' label when viewLevel is epics", () => {
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "epics" });
    expect(screen.getByRole("button", { name: /^Group by/ })).toHaveTextContent("Epics");
  });

  it("shows 'Features & Bugs' label when viewLevel is features", () => {
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "features" });
    expect(screen.getByRole("button", { name: /^Group by/ })).toHaveTextContent("Features & Bugs");
  });

  it("shows 'Tree' label when viewLevel is none (the full hierarchy view)", () => {
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "none" });
    expect(screen.getByRole("button", { name: /^Group by/ })).toHaveTextContent("Tree");
  });

  it("shows 'Flat' label when viewLevel is flat", () => {
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "flat" });
    expect(screen.getByRole("button", { name: /^Group by/ })).toHaveTextContent("Flat");
  });

  it("view selector button is enabled (not disabled)", () => {
    render(Toolbar, { ...defaultToolbarProps });
    expect(screen.getByRole("button", { name: /^Group by/ })).not.toBeDisabled();
  });

  it("opens dropdown with all five view levels when the control is clicked", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    await user.click(screen.getByRole("button", { name: /^Group by/ }));

    // All five view levels should appear as radio items
    const radioItems = screen.getAllByRole("menuitemradio");
    expect(radioItems).toHaveLength(5);
    expect(screen.getByRole("menuitemradio", { name: /Tree/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitemradio", { name: /Flat/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitemradio", { name: /Milestones/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitemradio", { name: /Epics/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitemradio", { name: /Features & Bugs/i })).toBeInTheDocument();
    expect(screen.queryByRole("menuitemradio", { name: /Backlog Items/i })).not.toBeInTheDocument();
  });

  it("calls onviewlevelchange and closes dropdown when an option is clicked", async () => {
    const onviewlevelchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "milestones", onviewlevelchange });

    await user.click(screen.getByRole("button", { name: /^Group by/ }));
    await user.click(screen.getByRole("menuitemradio", { name: /Epics/i }));

    expect(onviewlevelchange).toHaveBeenCalledWith("epics");
  });

  it("calls onviewlevelchange with 'flat' when the Flat option is clicked", async () => {
    const onviewlevelchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "none", onviewlevelchange });

    await user.click(screen.getByRole("button", { name: /^Group by/ }));
    await user.click(screen.getByRole("menuitemradio", { name: /^Flat$/i }));

    expect(onviewlevelchange).toHaveBeenCalledWith("flat");
  });

  it("closes dropdown on second click of view selector button", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    const viewBtn = screen.getByRole("button", { name: /^Group by/ });
    await user.click(viewBtn);
    expect(screen.getAllByRole("menuitemradio").length).toBeGreaterThan(0);

    await user.click(viewBtn);
    expect(screen.queryAllByRole("menuitemradio")).toHaveLength(0);
  });

  it("opens type dropdown with all 6 nib types when New item is clicked", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    await user.click(screen.getByTestId("toolbar-add"));

    // All 6 nib types should appear as menu items
    const items = screen.getAllByRole("menuitem");
    expect(items).toHaveLength(6);
    expect(screen.getByTestId("toolbar-add-milestone")).toBeInTheDocument();
    expect(screen.getByTestId("toolbar-add-epic")).toBeInTheDocument();
    expect(screen.getByTestId("toolbar-add-bug")).toBeInTheDocument();
    expect(screen.getByTestId("toolbar-add-feature")).toBeInTheDocument();
    expect(screen.getByTestId("toolbar-add-task")).toBeInTheDocument();
    expect(screen.getByTestId("toolbar-add-research")).toBeInTheDocument();
  });

  it("calls oncreatenew with selected type when a type is clicked", async () => {
    const oncreatenew = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, oncreatenew });

    await user.click(screen.getByTestId("toolbar-add"));
    await user.click(screen.getByTestId("toolbar-add-bug"));

    expect(oncreatenew).toHaveBeenCalledWith("bug");
  });

  it("Columns button is enabled", () => {
    render(Toolbar, { ...defaultToolbarProps });

    expect(screen.getByRole("button", { name: "Columns" })).not.toBeDisabled();
  });

  it("gear button opens the Settings sheet revealing Appearance and Row density", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    // Old Options dropdown is retired
    expect(screen.queryByRole("button", { name: "Options" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Settings" }));

    expect(screen.getByText("Appearance")).toBeInTheDocument();
    expect(screen.getByRole("radiogroup", { name: /row density/i })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /compact/i })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /comfortable/i })).toBeInTheDocument();
  });

  it("density controls are not part of the toolbar's own controls until the sheet opens", () => {
    render(Toolbar, { ...defaultToolbarProps });

    // Nothing density-related is rendered before the sheet is opened
    expect(screen.queryByRole("radiogroup", { name: /row density/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("radio", { name: /comfortable/i })).not.toBeInTheDocument();
  });

  it("clicking a density option in the sheet calls ondensitychange (wired via handleSetDensity)", async () => {
    const ondensitychange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, rowDensity: "compact", ondensitychange });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    await user.click(screen.getByRole("radio", { name: /comfortable/i }));

    expect(ondensitychange).toHaveBeenCalledWith("comfortable");
  });

  it("gear button opens the Settings sheet revealing the Theme control", async () => {
    render(Toolbar, { ...defaultToolbarProps, theme: "graphite" });

    await user.click(screen.getByRole("button", { name: "Settings" }));

    expect(screen.getByText("Theme")).toBeInTheDocument();
    expect(screen.getByTestId("theme-select")).toHaveTextContent("Graphite");
  });

  it("clicking a theme option in the sheet calls onthemechange (callback path)", async () => {
    const onthemechange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, theme: "graphite", onthemechange });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    await user.click(screen.getByTestId("theme-select"));
    await user.click(screen.getByRole("option", { name: "Dracula" }));

    expect(onthemechange).toHaveBeenCalledWith("dracula");
  });

  it("handleSetTheme mutates prefs.theme when prefs is provided (prefs path)", async () => {
    const prefs = new Preferences();
    expect(prefs.theme).toBe("graphite");
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    await user.click(screen.getByTestId("theme-select"));
    await user.click(screen.getByRole("option", { name: "Midnight" }));

    expect(prefs.theme).toBe("midnight");
  });

  it("gear button opens the Settings sheet revealing the Font size control", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    await user.click(screen.getByRole("button", { name: "Settings" }));

    expect(screen.getByRole("radiogroup", { name: /font size/i })).toBeInTheDocument();
    const group = screen.getByRole("radiogroup", { name: /font size/i });
    expect(within(group).getByRole("radio", { name: "Small" })).toBeInTheDocument();
    expect(within(group).getByRole("radio", { name: "Medium" })).toBeInTheDocument();
    expect(within(group).getByRole("radio", { name: "Large" })).toBeInTheDocument();
  });

  it("clicking a Font size option in the sheet calls onfontsizechange (callback path)", async () => {
    const onfontsizechange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, fontSize: "medium", onfontsizechange });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    const group = screen.getByRole("radiogroup", { name: /font size/i });
    await user.click(within(group).getByRole("radio", { name: "Large" }));

    expect(onfontsizechange).toHaveBeenCalledWith("large");
  });

  it("handleSetFontSize mutates prefs.fontSize when prefs is provided (prefs path)", async () => {
    const prefs = new Preferences();
    expect(prefs.fontSize).toBe("medium");
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    const group = screen.getByRole("radiogroup", { name: /font size/i });
    await user.click(within(group).getByRole("radio", { name: "Small" }));

    expect(prefs.fontSize).toBe("small");
  });

  it("clicking a Blocked emphasis option in the sheet calls onemphasischange (callback path)", async () => {
    const onemphasischange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, blockedEmphasis: "pill", onemphasischange });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    await user.click(screen.getByRole("radio", { name: "Subtle" }));

    expect(onemphasischange).toHaveBeenCalledWith("subtle");
  });

  it("handleSetBlockedEmphasis mutates prefs.blockedEmphasis when prefs is provided (prefs path)", async () => {
    const prefs = new Preferences();
    expect(prefs.blockedEmphasis).toBe("pill");
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    await user.click(screen.getByRole("radio", { name: "Pill+dim" }));

    expect(prefs.blockedEmphasis).toBe("pill-dim");
  });

  it("keyword input emits filter with search value", async () => {
    const onchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, onchange });

    const input = screen.getByTestId("filter-keyword");
    await user.type(input, "auth");

    expect(onchange).toHaveBeenCalled();
    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0]).toMatchObject({ search: "auth" });
  });

  // The standalone hide/show-completed toggle was removed; status
  // visibility is now driven solely by the State-facet include-list + its presets.
  it("no longer renders the standalone completed toggle", () => {
    render(Toolbar, { ...defaultToolbarProps });

    expect(screen.queryByTestId("toolbar-include-completed")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /completed/i })).not.toBeInTheDocument();
  });

  // Columns dropdown tests
  it("opens Columns dropdown when Columns button is clicked", async () => {
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "epics" as ViewLevel });

    await user.click(screen.getByRole("button", { name: "Columns" }));

    // Should show checkboxes for all columns
    const items = screen.getAllByRole("menuitemcheckbox");
    const labels = items.map(item => item.textContent?.trim());
    expect(labels).toContain("ID");
    expect(labels).toContain("Parent");
    expect(labels).toContain("Type");
    expect(labels).toContain("Title");
    expect(labels).toContain("Status");
    expect(labels).toContain("Effort");
    expect(labels).toContain("Tags");
  });

  it("Title checkbox is always disabled in Columns dropdown", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    await user.click(screen.getByRole("button", { name: "Columns" }));

    const items = screen.getAllByRole("menuitemcheckbox");
    // Find the Title item
    const titleItem = items.find(item => item.textContent?.trim() === "Title");
    expect(titleItem).toBeInTheDocument();
    expect(titleItem).toHaveAttribute("data-disabled", "");
  });

  it("toggling a column checkbox calls oncolumnschange with updated columns", async () => {
    const oncolumnschange = vi.fn();
    render(Toolbar, {
      ...defaultToolbarProps,
      visibleColumns: [...ALL_COLUMN_KEYS] as ColumnKey[],
      oncolumnschange,
    });

    await user.click(screen.getByRole("button", { name: "Columns" }));

    // Find the Tags checkbox and uncheck it
    const items = screen.getAllByRole("menuitemcheckbox");
    const tagsItem = items.find(item => item.textContent?.trim() === "Tags");
    expect(tagsItem).toBeInTheDocument();
    await user.click(tagsItem!);

    expect(oncolumnschange).toHaveBeenCalledOnce();
    const callArg = oncolumnschange.mock.calls[0][0] as ColumnKey[];
    expect(callArg).not.toContain("tags");
    expect(callArg).toContain("id");
    expect(callArg).toContain("title");
  });

  it("lists Blocking and Blocked by in the Columns dropdown", async () => {
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "epics" as ViewLevel });

    await user.click(screen.getByRole("button", { name: "Columns" }));

    const items = screen.getAllByRole("menuitemcheckbox");
    const labels = items.map(item => item.textContent?.trim());
    expect(labels).toContain("Blocking");
    expect(labels).toContain("Blocked by");
  });

  it("lists Created and Modified in the Columns dropdown", async () => {
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "epics" as ViewLevel });

    await user.click(screen.getByRole("button", { name: "Columns" }));

    const items = screen.getAllByRole("menuitemcheckbox");
    const labels = items.map(item => item.textContent?.trim());
    expect(labels).toContain("Created");
    expect(labels).toContain("Modified");
  });

  it("Modified is checked and Created is unchecked when using the default-visible columns", async () => {
    render(Toolbar, {
      ...defaultToolbarProps,
      visibleColumns: [...DEFAULT_VISIBLE_COLUMNS] as ColumnKey[],
      viewLevel: "epics" as ViewLevel,
    });

    await user.click(screen.getByRole("button", { name: "Columns" }));

    const items = screen.getAllByRole("menuitemcheckbox");
    const created = items.find(item => item.textContent?.trim() === "Created");
    const modified = items.find(item => item.textContent?.trim() === "Modified");
    // modified is default-visible; created is opt-in.
    expect(modified).toHaveAttribute("aria-checked", "true");
    expect(created).toHaveAttribute("aria-checked", "false");
  });

  it("toggling Created on emits it in canonical order (before modified)", async () => {
    const oncolumnschange = vi.fn();
    render(Toolbar, {
      ...defaultToolbarProps,
      visibleColumns: [...DEFAULT_VISIBLE_COLUMNS] as ColumnKey[],
      oncolumnschange,
      viewLevel: "epics" as ViewLevel,
    });

    await user.click(screen.getByRole("button", { name: "Columns" }));

    const items = screen.getAllByRole("menuitemcheckbox");
    const createdItem = items.find(item => item.textContent?.trim() === "Created");
    expect(createdItem).toBeInTheDocument();
    await user.click(createdItem!);

    expect(oncolumnschange).toHaveBeenCalledOnce();
    const callArg = oncolumnschange.mock.calls[0][0] as ColumnKey[];
    // created (canonical index 9) sorts before modified (index 10) in ALL_COLUMN_KEYS.
    expect(callArg).toEqual(["id", "parent", "type", "title", "status", "effort", "tags", "created", "modified"]);
  });

  it("Blocking and Blocked by are unchecked when using the default-visible columns", async () => {
    render(Toolbar, {
      ...defaultToolbarProps,
      visibleColumns: [...DEFAULT_VISIBLE_COLUMNS] as ColumnKey[],
      viewLevel: "epics" as ViewLevel,
    });

    await user.click(screen.getByRole("button", { name: "Columns" }));

    const items = screen.getAllByRole("menuitemcheckbox");
    const blocking = items.find(item => item.textContent?.trim() === "Blocking");
    const blockedBy = items.find(item => item.textContent?.trim() === "Blocked by");
    expect(blocking).toHaveAttribute("aria-checked", "false");
    expect(blockedBy).toHaveAttribute("aria-checked", "false");
  });

  it("toggling Blocking on emits it appended in canonical order (after tags)", async () => {
    const oncolumnschange = vi.fn();
    render(Toolbar, {
      ...defaultToolbarProps,
      visibleColumns: [...DEFAULT_VISIBLE_COLUMNS] as ColumnKey[],
      oncolumnschange,
      viewLevel: "epics" as ViewLevel,
    });

    await user.click(screen.getByRole("button", { name: "Columns" }));

    const items = screen.getAllByRole("menuitemcheckbox");
    const blockingItem = items.find(item => item.textContent?.trim() === "Blocking");
    expect(blockingItem).toBeInTheDocument();
    await user.click(blockingItem!);

    expect(oncolumnschange).toHaveBeenCalledOnce();
    const callArg = oncolumnschange.mock.calls[0][0] as ColumnKey[];
    // blocking (canonical index 7) slots in before the default-visible modified.
    expect(callArg).toEqual(["id", "parent", "type", "title", "status", "effort", "tags", "blocking", "modified"]);
  });

  it("closes Columns dropdown on second click", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    const columnsBtn = screen.getByRole("button", { name: "Columns" });
    await user.click(columnsBtn);
    expect(screen.getAllByRole("menuitemcheckbox").length).toBeGreaterThan(0);

    await user.click(columnsBtn);
    expect(screen.queryAllByRole("menuitemcheckbox")).toHaveLength(0);
  });

  it("Parent column is shown in Columns checklist for milestones (parent is now a normal column)", async () => {
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "milestones" as ViewLevel });

    await user.click(screen.getByRole("button", { name: "Columns" }));

    const items = screen.getAllByRole("menuitemcheckbox");
    const labels = items.map(item => item.textContent?.trim());
    expect(labels).toContain("Parent");
    expect(labels).toContain("ID");
    expect(labels).toContain("Title");
  });

  it("Parent column is shown in Columns checklist when viewLevel is epics", async () => {
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "epics" as ViewLevel });

    await user.click(screen.getByRole("button", { name: "Columns" }));

    const items = screen.getAllByRole("menuitemcheckbox");
    const labels = items.map(item => item.textContent?.trim());
    expect(labels).toContain("Parent");
  });

  it("does not render standalone density toggle button", () => {
    render(Toolbar, { ...defaultToolbarProps });

    expect(screen.queryByTestId("toolbar-density")).not.toBeInTheDocument();
  });

  it("maintains canonical column order when toggling a column back on", async () => {
    const oncolumnschange = vi.fn();
    // Start with 'type' missing (columns in canonical order minus 'type')
    render(Toolbar, {
      ...defaultToolbarProps,
      visibleColumns: ["id", "parent", "title", "status", "effort", "tags"] as ColumnKey[],
      oncolumnschange,
      viewLevel: "epics" as ViewLevel,
    });

    await user.click(screen.getByRole("button", { name: "Columns" }));

    // Find the Type checkbox and check it
    const items = screen.getAllByRole("menuitemcheckbox");
    const typeItem = items.find(item => item.textContent?.trim() === "Type");
    expect(typeItem).toBeInTheDocument();
    await user.click(typeItem!);

    expect(oncolumnschange).toHaveBeenCalledOnce();
    const callArg = oncolumnschange.mock.calls[0][0] as ColumnKey[];
    // 'type' should be inserted at its canonical position (index 2), not appended at the end
    expect(callArg).toEqual(["id", "parent", "type", "title", "status", "effort", "tags"]);
  });

  it("re-sorts the visible set by the per-view columnOrder on toggle (not the canonical order)", async () => {
    const oncolumnschange = vi.fn();
    // A custom order where state/title precede id — distinct from canonical.
    const columnOrder: ColumnKey[] = ["status", "title", "id", "parent", "type", "effort", "tags", "blocking", "blockedBy", "created", "modified"];
    render(Toolbar, {
      ...defaultToolbarProps,
      visibleColumns: ["title", "status"] as ColumnKey[],
      columnOrder,
      oncolumnschange,
      viewLevel: "epics" as ViewLevel,
    });

    await user.click(screen.getByRole("button", { name: "Columns" }));

    const items = screen.getAllByRole("menuitemcheckbox");
    const idItem = items.find((item) => item.textContent?.trim() === "ID");
    expect(idItem).toBeInTheDocument();
    await user.click(idItem!);

    expect(oncolumnschange).toHaveBeenCalledOnce();
    const callArg = oncolumnschange.mock.calls[0][0] as ColumnKey[];
    // Sorted by columnOrder (state<title<id), NOT canonical (which would be id,title,state).
    expect(callArg).toEqual(["status", "title", "id"]);
  });
});

// Filter-dropdown coverage. Toolbar owns the filter-toggle logic
// (toggleArrayValue/handleToggle, mutual exclusion, per-category Clear, status
// conflict resolution, count badges), so its behavior is pinned here.
describe("Toolbar — filter dropdowns", () => {
  it("opens the Type dropdown with all 6 type checkboxes when its trigger is clicked", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    // No type checkboxes visible initially
    expect(screen.queryByRole("menuitemcheckbox", { name: "bug" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /type/i }));

    expect(screen.getByRole("menuitemcheckbox", { name: "bug" })).toBeInTheDocument();
    expect(screen.getByRole("menuitemcheckbox", { name: "feature" })).toBeInTheDocument();
    expect(screen.getByRole("menuitemcheckbox", { name: "task" })).toBeInTheDocument();
    expect(screen.getByRole("menuitemcheckbox", { name: "milestone" })).toBeInTheDocument();
    expect(screen.getByRole("menuitemcheckbox", { name: "epic" })).toBeInTheDocument();
    expect(screen.getByRole("menuitemcheckbox", { name: "research" })).toBeInTheDocument();
  });

  it("checking a type checkbox emits filter with that type", async () => {
    const onchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, onchange });

    await user.click(screen.getByRole("button", { name: /type/i }));
    await user.click(screen.getByRole("menuitemcheckbox", { name: "bug" }));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0]).toMatchObject({ type: ["bug"] });
  });

  it("checking a priority checkbox emits filter with that priority", async () => {
    const onchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, onchange });

    await user.click(screen.getByRole("button", { name: /priority/i }));
    await user.click(screen.getByRole("menuitemcheckbox", { name: "high" }));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0]).toMatchObject({ priority: ["high"] });
  });

  it("checking a state checkbox emits filter with that status", async () => {
    const onchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, onchange });

    await user.click(screen.getByRole("button", { name: /status/i }));
    await user.click(screen.getByRole("menuitemcheckbox", { name: "in-progress" }));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0]).toMatchObject({ status: ["in-progress"] });
  });

  it("checking an effort checkbox emits filter with that estimate", async () => {
    const onchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, onchange });

    await user.click(screen.getByRole("button", { name: /effort/i }));
    await user.click(screen.getByRole("menuitemcheckbox", { name: "l" }));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0]).toMatchObject({ estimate: ["l"] });
  });

  it("unchecking the last value in a category removes the filter field", async () => {
    const onchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, filter: { type: ["bug"] }, onchange });

    await user.click(screen.getByRole("button", { name: /type/i }));
    await user.click(screen.getByRole("menuitemcheckbox", { name: "bug" }));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0].type).toBeUndefined();
  });

  it("closes an open filter dropdown when its own trigger is clicked again", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    const typeBtn = screen.getByRole("button", { name: /type/i });
    await user.click(typeBtn);
    expect(screen.getByRole("menuitemcheckbox", { name: "bug" })).toBeInTheDocument();

    await user.click(typeBtn);
    expect(screen.queryByRole("menuitemcheckbox", { name: "bug" })).not.toBeInTheDocument();
  });

  it("closes an open filter dropdown when another filter trigger is opened (mutual exclusion)", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    await user.click(screen.getByRole("button", { name: /type/i }));
    expect(screen.getByRole("menuitemcheckbox", { name: "bug" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /priority/i }));
    expect(screen.queryByRole("menuitemcheckbox", { name: "bug" })).not.toBeInTheDocument();
    expect(screen.getByRole("menuitemcheckbox", { name: "high" })).toBeInTheDocument();
  });

  it("closes an open filter dropdown when Escape is pressed", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    await user.click(screen.getByRole("button", { name: /type/i }));
    expect(screen.getByRole("menuitemcheckbox", { name: "bug" })).toBeInTheDocument();

    await user.keyboard("{Escape}");
    expect(screen.queryByRole("menuitemcheckbox", { name: "bug" })).not.toBeInTheDocument();
  });

  it("clears search to undefined when the keyword input is emptied", async () => {
    const onchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, filter: { search: "old" }, onchange });

    const input = screen.getByTestId("filter-keyword");
    await user.clear(input);

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0].search).toBeUndefined();
  });

  // Inline ✕ clear button on the keyword input.
  it("does not render the keyword clear button when the input is empty", () => {
    render(Toolbar, { ...defaultToolbarProps, filter: {} });

    expect(screen.queryByTestId("filter-keyword-clear")).not.toBeInTheDocument();
  });

  it("renders the keyword clear button only when the input has text", () => {
    render(Toolbar, { ...defaultToolbarProps, filter: { search: "auth" } });

    expect(screen.getByTestId("filter-keyword-clear")).toBeInTheDocument();
    // Also queryable by its accessible name.
    expect(screen.getByRole("button", { name: "Clear keyword" })).toBeInTheDocument();
  });

  it("clicking the keyword clear button clears search and refocuses the input", async () => {
    const onchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, filter: { search: "auth" }, onchange });

    await user.click(screen.getByTestId("filter-keyword-clear"));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0].search).toBeUndefined();
    // The input regains focus so the user can keep typing a new query.
    expect(screen.getByTestId("filter-keyword")).toHaveFocus();
  });

  it("does not render the Tags trigger when availableTags is empty", () => {
    render(Toolbar, { ...defaultToolbarProps, availableTags: [] });
    expect(screen.queryByRole("button", { name: /tags/i })).not.toBeInTheDocument();
  });

  it("renders the Tags dropdown with checkboxes when availableTags has items", async () => {
    const onchange = vi.fn();
    render(Toolbar, {
      ...defaultToolbarProps,
      onchange,
      availableTags: ["frontend", "backend"],
    });

    const tagsBtn = screen.getByRole("button", { name: /tags/i });
    expect(tagsBtn).toBeInTheDocument();

    await user.click(tagsBtn);
    expect(screen.getByRole("menuitemcheckbox", { name: "frontend" })).toBeInTheDocument();
    expect(screen.getByRole("menuitemcheckbox", { name: "backend" })).toBeInTheDocument();

    await user.click(screen.getByRole("menuitemcheckbox", { name: "frontend" }));
    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0]).toMatchObject({ tags: ["frontend"] });
  });

  it("per-category Clear menu item clears the category when selections exist", async () => {
    const onchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, filter: { type: ["bug", "feature"] }, onchange });

    await user.click(screen.getByRole("button", { name: /type/i }));

    const clearItem = screen.getByRole("menuitem", { name: /clear/i });
    expect(clearItem).toBeInTheDocument();

    await user.click(clearItem);
    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0].type).toBeUndefined();
  });

  it("per-category Clear menu item is disabled when the category has no selections", async () => {
    render(Toolbar, { ...defaultToolbarProps, filter: {} });

    await user.click(screen.getByRole("button", { name: /type/i }));

    const clearItem = screen.getByRole("menuitem", { name: /clear/i });
    expect(clearItem).toBeInTheDocument();
    expect(clearItem).toHaveAttribute("data-disabled", "");
  });

  it("clicking the 'Open' preset overwrites status with the Open set", async () => {
    const onchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, onchange });

    await user.click(screen.getByRole("button", { name: /status/i }));
    await user.click(screen.getByTestId("status-preset-open"));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0].status).toEqual([...OPEN_STATUSES]);
  });

  it("clicking the 'Open + deferred' preset overwrites status with that set", async () => {
    const onchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, onchange });

    await user.click(screen.getByRole("button", { name: /status/i }));
    await user.click(screen.getByTestId("status-preset-open-deferred"));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0].status).toEqual([...OPEN_PLUS_DEFERRED_STATUSES]);
  });

  it("a preset REPLACES an existing status selection (does not merge)", async () => {
    const onchange = vi.fn();
    render(Toolbar, {
      ...defaultToolbarProps,
      filter: { status: ["completed"] },
      onchange,
    });

    await user.click(screen.getByRole("button", { name: /status/i }));
    await user.click(screen.getByTestId("status-preset-open"));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    // "completed" is gone — the preset overwrites the whole include-list.
    expect(lastCall[0].status).toEqual([...OPEN_STATUSES]);
  });

  it("a preset preserves other filter fields (only status is overwritten)", async () => {
    const onchange = vi.fn();
    render(Toolbar, {
      ...defaultToolbarProps,
      filter: { search: "auth", type: ["bug"] },
      onchange,
    });

    await user.click(screen.getByRole("button", { name: /status/i }));
    await user.click(screen.getByTestId("status-preset-open-deferred"));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0]).toMatchObject({
      search: "auth",
      type: ["bug"],
      status: [...OPEN_PLUS_DEFERRED_STATUSES],
    });
  });

  it("per-status checkboxes still toggle the include-list after presets exist", async () => {
    const onchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, filter: { status: ["todo"] }, onchange });

    await user.click(screen.getByRole("button", { name: /status/i }));
    await user.click(screen.getByRole("menuitemcheckbox", { name: "in-progress" }));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0].status).toEqual(["todo", "in-progress"]);
  });

  it("shows an active-count badge on triggers whose category has selections", () => {
    render(Toolbar, {
      ...defaultToolbarProps,
      filter: { type: ["bug", "feature"], priority: ["high"] },
    });

    // Type trigger shows count 2, Priority trigger shows count 1
    expect(screen.getByRole("button", { name: /type/i })).toHaveTextContent("2");
    expect(screen.getByRole("button", { name: /priority/i })).toHaveTextContent("1");

    // State and Effort badges are present but invisible (no active selections)
    const stateBadge = screen.getByRole("button", { name: /status/i }).querySelector("span");
    expect(stateBadge?.classList.contains("invisible")).toBe(true);

    const effortBadge = screen.getByRole("button", { name: /effort/i }).querySelector("span");
    expect(effortBadge?.classList.contains("invisible")).toBe(true);
  });
});

// Query-box ↔ NibFilter sync (phase 1 tracer: the `status:` token + free text).
// The box parses on input into the same canonical NibFilter the State dropdown
// reads/writes; the dropdown rewrites the box text while unfocused; the box is
// never rewritten under the cursor; blur canonicalizes. These use a real
// `Preferences` instance so the emitted filter round-trips back into
// `resolvedFilter` (the callback-only path never feeds the change back).
describe("Toolbar — query box status sync", () => {
  it("typing 'status:todo' parses into the filter and ticks 'todo' in the State dropdown", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    await user.type(screen.getByTestId("filter-keyword"), "status:todo");

    // Parsed into the canonical filter (status include-list, no free text).
    expect(prefs.filter.status).toEqual(["todo"]);
    expect(prefs.filter.search).toBeUndefined();

    // The State dropdown reflects it live.
    await user.click(screen.getByRole("button", { name: /status/i }));
    expect(screen.getByRole("menuitemcheckbox", { name: "todo" })).toHaveAttribute("aria-checked", "true");
  });

  it("typing mixed 'login status:todo' sets both search and status", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    await user.type(screen.getByTestId("filter-keyword"), "login status:todo");

    expect(prefs.filter.search).toBe("login");
    expect(prefs.filter.status).toEqual(["todo"]);
  });

  it("bare words populate search only (unchanged Bleve behavior)", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    await user.type(screen.getByTestId("filter-keyword"), "login flow");

    expect(prefs.filter.search).toBe("login flow");
    expect(prefs.filter.status).toBeUndefined();
  });

  it("toggling the State dropdown regenerates the box text (canonical form) while unfocused", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    // Box starts empty and is never focused in this test.
    expect((screen.getByTestId("filter-keyword") as HTMLInputElement).value).toBe("");

    await user.click(screen.getByRole("button", { name: /status/i }));
    await user.click(screen.getByRole("menuitemcheckbox", { name: "todo" }));

    expect(prefs.filter.status).toEqual(["todo"]);
    // The unfocused box is rewritten to the canonical token form.
    expect((screen.getByTestId("filter-keyword") as HTMLInputElement).value).toBe("status:todo");
  });

  it("does not rewrite the box while it is focused (no canonicalization mid-type)", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    const input = screen.getByTestId("filter-keyword") as HTMLInputElement;
    // Non-canonical text: uppercase field name and value.
    await user.type(input, "STATUS:Todo");

    // Parsed and lowercased into the filter...
    expect(prefs.filter.status).toEqual(["todo"]);
    // ...but the literal keystrokes remain visible while focused.
    expect(input.value).toBe("STATUS:Todo");
    expect(input).toHaveFocus();
  });

  it("canonicalizes the box text on blur (lowercased token, collapsed whitespace)", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    const input = screen.getByTestId("filter-keyword") as HTMLInputElement;
    await user.type(input, "STATUS:Todo   extra");
    expect(input.value).toBe("STATUS:Todo   extra");

    await user.tab(); // blur

    expect(input.value).toBe("status:todo extra");
    expect(prefs.filter.status).toEqual(["todo"]);
    expect(prefs.filter.search).toBe("extra");
  });

  it("the clear button empties the whole query — both status token and free text", async () => {
    const prefs = new Preferences();
    prefs.filter = { status: ["todo"], search: "login" };
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    // Box seeds to the canonical query for the existing filter.
    expect((screen.getByTestId("filter-keyword") as HTMLInputElement).value).toBe("status:todo login");

    await user.click(screen.getByTestId("filter-keyword-clear"));

    expect(prefs.filter.status).toBeUndefined();
    expect(prefs.filter.search).toBeUndefined();
    expect((screen.getByTestId("filter-keyword") as HTMLInputElement).value).toBe("");
  });
});

// Phase 2: the full metadata grammar (type/priority/status/estimate/tags +
// exclusions), the invalid-token sidecar + inline flag, all five dropdowns in
// two-way sync with the box, and static autocomplete. A real `Preferences`
// instance is used so an emitted filter round-trips back into `resolvedFilter`.
describe("Toolbar — query box metadata grammar", () => {
  it("typing a multi-field query sets every field and ticks each matching dropdown", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn(), availableTags: ["wip", "frontend"] });

    await user.type(screen.getByTestId("filter-keyword"), "type:bug,task -tags:wip status:todo");

    // Parsed into the canonical filter: comma OR, negation → excludeTags.
    expect(prefs.filter.type).toEqual(["bug", "task"]);
    expect(prefs.filter.excludeTags).toEqual(["wip"]);
    expect(prefs.filter.status).toEqual(["todo"]);

    // Type dropdown ticks both bug and task, not feature.
    await user.click(screen.getByRole("button", { name: /type/i }));
    expect(screen.getByRole("menuitemcheckbox", { name: "bug" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("menuitemcheckbox", { name: "task" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("menuitemcheckbox", { name: "feature" })).toHaveAttribute("aria-checked", "false");
    await user.keyboard("{Escape}");

    // State dropdown ticks todo.
    await user.click(screen.getByRole("button", { name: /status/i }));
    expect(screen.getByRole("menuitemcheckbox", { name: "todo" })).toHaveAttribute("aria-checked", "true");
  });

  it("typing a priority token ticks the Priority dropdown", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    await user.type(screen.getByTestId("filter-keyword"), "priority:high");
    expect(prefs.filter.priority).toEqual(["high"]);

    await user.click(screen.getByRole("button", { name: /priority/i }));
    expect(screen.getByRole("menuitemcheckbox", { name: "high" })).toHaveAttribute("aria-checked", "true");
  });

  it("typing an estimate token ticks the Effort dropdown", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    await user.type(screen.getByTestId("filter-keyword"), "estimate:l");
    expect(prefs.filter.estimate).toEqual(["l"]);

    await user.click(screen.getByRole("button", { name: /effort/i }));
    expect(screen.getByRole("menuitemcheckbox", { name: "l" })).toHaveAttribute("aria-checked", "true");
  });

  it("checking a Type dropdown box regenerates the query text while unfocused", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    const input = screen.getByTestId("filter-keyword") as HTMLInputElement;
    expect(input.value).toBe("");

    await user.click(screen.getByRole("button", { name: /type/i }));
    await user.click(screen.getByRole("menuitemcheckbox", { name: "bug" }));

    expect(prefs.filter.type).toEqual(["bug"]);
    // The unfocused box is rewritten to the canonical token form.
    expect(input.value).toBe("type:bug");
  });

  it("serializes fields in canonical order when several dropdowns are set", async () => {
    const prefs = new Preferences();
    prefs.filter = { type: ["bug"], priority: ["high"], status: ["todo"], search: "login" };
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    // type, priority, status, then search last.
    expect((screen.getByTestId("filter-keyword") as HTMLInputElement).value).toBe(
      "type:bug priority:high status:todo login",
    );
  });
});

describe("Toolbar — invalid token handling", () => {
  it("flags an invalid value, applies only the valid tokens, and preserves the invalid token across blur and a dropdown edit", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    const input = screen.getByTestId("filter-keyword") as HTMLInputElement;
    await user.type(input, "type:bug status:banana");

    // Only the valid token reaches the filter.
    expect(prefs.filter.type).toEqual(["bug"]);
    expect(prefs.filter.status).toBeUndefined();

    // The invalid token is flagged inline.
    expect(screen.getByTestId("filter-invalid")).toHaveTextContent("status:banana");

    // Survives blur (canonicalized, invalid appended at the end).
    await user.tab();
    expect(input.value).toBe("type:bug status:banana");
    expect(screen.getByTestId("filter-invalid")).toBeInTheDocument();

    // Survives a dropdown edit — invalid stays flagged and in the regenerated text.
    await user.click(screen.getByRole("button", { name: /priority/i }));
    await user.click(screen.getByRole("menuitemcheckbox", { name: "high" }));

    expect(prefs.filter.priority).toEqual(["high"]);
    expect(screen.getByTestId("filter-invalid")).toHaveTextContent("status:banana");
    expect(input.value).toBe("type:bug priority:high status:banana");
  });

  it("seeds the box + marker from prefs.invalidTokens (a reloaded / shared query reproduces parked tokens)", () => {
    const prefs = new Preferences();
    prefs.filter = { type: ["bug"] };
    prefs.invalidTokens = ["status:banana"];
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    // The box seeds to the canonical query INCLUDING the parked invalid token,
    // so opening a shared `?q=` link (or reloading) shows exactly what was shared.
    expect((screen.getByTestId("filter-keyword") as HTMLInputElement).value).toBe("type:bug status:banana");
    // ...and the unrecognized-token marker is present on first paint.
    expect(screen.getByTestId("filter-invalid")).toHaveTextContent("status:banana");
  });

  it("does not render the invalid marker when all tokens are valid", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    await user.type(screen.getByTestId("filter-keyword"), "type:bug");
    expect(screen.queryByTestId("filter-invalid")).not.toBeInTheDocument();
  });

  it("suppresses the invalid marker while a completion dropdown is open, then shows it once closed", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    const input = screen.getByTestId("filter-keyword") as HTMLInputElement;
    // `status:dra` is invalid ("dra" is not a status) but prefixes "draft", so the
    // completion opens. Marker + dropdown both anchor below the box, so the marker
    // is suppressed while the dropdown offers the valid value.
    await user.type(input, "status:dra");
    expect(screen.getByTestId("filter-suggestions")).toBeInTheDocument();
    expect(screen.queryByTestId("filter-invalid")).not.toBeInTheDocument();

    // Once the completion closes (Escape) the token is still invalid → marker shows.
    await user.keyboard("{Escape}");
    expect(screen.queryByTestId("filter-suggestions")).not.toBeInTheDocument();
    expect(screen.getByTestId("filter-invalid")).toHaveTextContent("status:dra");
  });

  it("clearing the box removes the invalid marker", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    await user.type(screen.getByTestId("filter-keyword"), "status:banana");
    expect(screen.getByTestId("filter-invalid")).toBeInTheDocument();

    await user.click(screen.getByTestId("filter-keyword-clear"));
    expect(screen.queryByTestId("filter-invalid")).not.toBeInTheDocument();
  });
});

// Phase 3: the syntax-highlight overlay. A colored backdrop layer renders each
// token behind a transparent-text input. This is pure presentation — parsing,
// sync and results are unchanged (covered above); here we pin that the backdrop
// mirrors the box text contiguously and colors each token by kind, with a
// red-underline on invalid values. Pixel alignment is proven by the screenshot,
// not jsdom.
describe("Toolbar — highlight overlay", () => {
  it("renders the backdrop layer behind a transparent-text input", () => {
    render(Toolbar, { ...defaultToolbarProps });

    const input = screen.getByTestId("filter-keyword");
    // The input hides its own glyphs so the colored backdrop shows through.
    expect(input).toHaveClass("text-transparent");
    // The backdrop exists and is hidden from assistive tech / pointer events.
    const backdrop = screen.getByTestId("filter-highlight");
    expect(backdrop).toHaveAttribute("aria-hidden", "true");
    expect(backdrop).toHaveClass("pointer-events-none");
    // The backdrop owns the box's raised surface fill, since the input above it
    // is transparent — without this the field loses its --popover surface.
    expect(backdrop).toHaveClass("bg-popover");
  });

  it("colors a field token: field name (link), colon (muted), value (foreground)", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    await user.type(screen.getByTestId("filter-keyword"), "type:bug");

    const backdrop = screen.getByTestId("filter-highlight");
    const field = backdrop.querySelector('[data-kind="field"]');
    const value = backdrop.querySelector('[data-kind="value"]');
    const operator = backdrop.querySelector('[data-kind="operator"]');
    expect(field).toHaveTextContent("type");
    expect(field).toHaveClass("text-link");
    expect(operator).toHaveTextContent(":");
    expect(value).toHaveTextContent("bug");
    expect(value).toHaveClass("text-foreground");
    // No invalid span for an all-valid query.
    expect(backdrop.querySelector('[data-kind="invalid"]')).toBeNull();
  });

  it("draws a red wavy underline on an invalid value (status:banana)", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    await user.type(screen.getByTestId("filter-keyword"), "status:banana");

    const backdrop = screen.getByTestId("filter-highlight");
    const invalid = backdrop.querySelector('[data-kind="invalid"]');
    expect(invalid).not.toBeNull();
    expect(invalid).toHaveTextContent("banana");
    expect(invalid).toHaveClass("underline");
    expect(invalid).toHaveClass("decoration-wavy");
    expect(invalid).toHaveClass("text-destructive");
  });

  it("mirrors the literal box text contiguously as the user types (valid + invalid + free text)", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    const input = screen.getByTestId("filter-keyword") as HTMLInputElement;
    await user.type(input, "type:bug status:banana login");

    // Typing still updates the real input value...
    expect(input.value).toBe("type:bug status:banana login");
    // ...and the backdrop reproduces it character-for-character (whitespace kept),
    // so every glyph aligns with the input.
    const backdrop = screen.getByTestId("filter-highlight");
    expect(backdrop.textContent).toBe("type:bug status:banana login");
    // The free-text word is colored as free text, not as a value.
    const freetext = [...backdrop.querySelectorAll('[data-kind="freetext"]')];
    expect(freetext.map((n) => n.textContent)).toContain("login");
  });

  it("clears the backdrop when the box is cleared", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    await user.type(screen.getByTestId("filter-keyword"), "type:bug");
    expect(screen.getByTestId("filter-highlight").textContent).toBe("type:bug");

    await user.click(screen.getByTestId("filter-keyword-clear"));
    expect(screen.getByTestId("filter-highlight").textContent).toBe("");
  });
});

// Phase 7: light token-click affordances over the overlay. A per-token hit-region
// mirrors the backdrop; clicking a token selects its range in the (native) input,
// and clicking a token's × splices it out of the query and re-filters. The input
// stays the sole editor — no contenteditable, no chips. jsdom can't verify pixel
// alignment (the screenshot does); these pin the behavior: selection offsets, the
// re-derived filter, and the whitespace-collapse on removal.
describe("Toolbar — token-click affordances", () => {
  // The token wrappers, in DOM/text order (one per non-whitespace filter token).
  function tokens(): HTMLElement[] {
    return screen.getAllByTestId("filter-token");
  }

  it("renders one hit-region per token in the interaction layer", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    await user.type(screen.getByTestId("filter-keyword"), "type:bug status:todo login");

    const toks = tokens();
    expect(toks.map((t) => t.textContent)).toEqual(["type:bug", "status:todo", "login"]);
    // The layer is a display/interaction overlay: hidden from AT (the input is the
    // accessible editor) and pointer-transparent except its token wrappers.
    const layer = screen.getByTestId("filter-tokens");
    expect(layer).toHaveAttribute("aria-hidden", "true");
    expect(layer).toHaveClass("pointer-events-none");
    // The layer's own token text MUST be transparent — it sits above the colored
    // highlight backdrop, so opaque text here would hide the field/value coloring
    // (only the invalid underline, below the baseline, would peek through).
    expect(layer).toHaveClass("text-transparent");
    expect(toks[0]).toHaveClass("pointer-events-auto");
    // The layer must reproduce the literal box text character-for-character (tokens
    // wrapped, gaps as inert text) so every glyph aligns with the input — no stray
    // whitespace text nodes from the markup. (The × icon svg contributes no text.)
    expect(layer.textContent).toBe("type:bug status:todo login");
  });

  it("the token × is excluded from the tab order (tabindex=-1)", async () => {
    // The interaction layer is aria-hidden; the × must not be tabbable, or Tab
    // from the input would land on invisible, unannounced buttons (one per token).
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    await user.type(screen.getByTestId("filter-keyword"), "type:bug");

    expect(within(tokens()[0]).getByTestId("filter-token-remove")).toHaveAttribute("tabindex", "-1");
  });

  it("removing a token closes an open completion (no stale suggestion)", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    // Type so the last token opens value completion (its apply() closes over
    // slices of the current text).
    await user.type(screen.getByTestId("filter-keyword"), "status:todo type:");
    expect(screen.getByTestId("filter-suggestions")).toBeInTheDocument();

    // Remove a DIFFERENT token via its ×; the open completion's offsets are now
    // stale, so it must be closed rather than left applicable.
    fireEvent.click(within(tokens()[0]).getByTestId("filter-token-remove"));
    await tick();

    expect(screen.queryByTestId("filter-suggestions")).not.toBeInTheDocument();
  });

  it("clicking a token selects its full range in the input and focuses it", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    const input = screen.getByTestId("filter-keyword") as HTMLInputElement;
    await user.type(input, "type:bug status:todo");

    // Click the second token → selection spans exactly its [start, end).
    fireEvent.click(tokens()[1]);

    expect(input).toHaveFocus();
    expect(input.selectionStart).toBe(9); // "type:bug ".length
    expect(input.selectionEnd).toBe(20); // end of "status:todo"
    // The selected range is exactly that token's text.
    expect(input.value.slice(input.selectionStart!, input.selectionEnd!)).toBe("status:todo");
  });

  it("clicking a token's × removes the FIRST token and drops no leading space", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    const input = screen.getByTestId("filter-keyword") as HTMLInputElement;
    await user.type(input, "type:bug status:todo login");

    fireEvent.click(within(tokens()[0]).getByTestId("filter-token-remove"));
    await tick();

    expect(input.value).toBe("status:todo login");
    // The removed token's contribution is gone from the re-derived filter.
    expect(prefs.filter.type).toBeUndefined();
    expect(prefs.filter.status).toEqual(["todo"]);
    expect(prefs.filter.search).toBe("login");
  });

  it("clicking a token's × removes a MIDDLE token leaving exactly one space", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    const input = screen.getByTestId("filter-keyword") as HTMLInputElement;
    await user.type(input, "type:bug priority:high status:todo");

    fireEvent.click(within(tokens()[1]).getByTestId("filter-token-remove"));
    await tick();

    expect(input.value).toBe("type:bug status:todo");
    expect(prefs.filter.priority).toBeUndefined();
    expect(prefs.filter.type).toEqual(["bug"]);
    expect(prefs.filter.status).toEqual(["todo"]);
  });

  it("clicking a token's × removes the LAST token leaving no trailing space", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    const input = screen.getByTestId("filter-keyword") as HTMLInputElement;
    await user.type(input, "type:bug status:todo");

    fireEvent.click(within(tokens()[1]).getByTestId("filter-token-remove"));
    await tick();

    expect(input.value).toBe("type:bug");
    expect(prefs.filter.status).toBeUndefined();
    expect(prefs.filter.type).toEqual(["bug"]);
  });

  it("clicking an invalid token's × removes it from the box and the invalid sidecar", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    const input = screen.getByTestId("filter-keyword") as HTMLInputElement;
    await user.type(input, "type:bug status:banana");
    expect(screen.getByTestId("filter-invalid")).toHaveTextContent("status:banana");

    fireEvent.click(within(tokens()[1]).getByTestId("filter-token-remove"));
    await tick();

    expect(input.value).toBe("type:bug");
    expect(prefs.invalidTokens).toEqual([]);
    expect(screen.queryByTestId("filter-invalid")).not.toBeInTheDocument();
    expect(prefs.filter.type).toEqual(["bug"]);
  });

  it("clicking a token's × does not also select the token (stopPropagation)", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    const input = screen.getByTestId("filter-keyword") as HTMLInputElement;
    await user.type(input, "type:bug status:todo");
    // Collapse any prior selection so a stray select would be observable.
    input.setSelectionRange(input.value.length, input.value.length);

    fireEvent.click(within(tokens()[0]).getByTestId("filter-token-remove"));
    await tick();

    // The token was removed (not merely selected).
    expect(input.value).toBe("status:todo");
  });
});

describe("Toolbar — static autocomplete", () => {
  it("suggests field names for a partial token and inserts one on click", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    const input = screen.getByTestId("filter-keyword") as HTMLInputElement;
    await user.type(input, "ty");

    const suggestions = screen.getByTestId("filter-suggestions");
    expect(within(suggestions).getByText("type")).toBeInTheDocument();

    await user.click(within(suggestions).getByText("type"));
    expect(input.value).toBe("type:");
  });

  it("suggests enum values after a field colon and inserts via keyboard (ArrowDown+Enter)", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    const input = screen.getByTestId("filter-keyword") as HTMLInputElement;
    await user.type(input, "type:bu");

    expect(within(screen.getByTestId("filter-suggestions")).getByText("bug")).toBeInTheDocument();

    await user.type(input, "{ArrowDown}{Enter}");
    expect(input.value).toBe("type:bug");
    expect(prefs.filter.type).toEqual(["bug"]);
  });

  it("suggests existing tags for a tags: token and inserts on click", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn(), availableTags: ["frontend", "backend"] });

    const input = screen.getByTestId("filter-keyword") as HTMLInputElement;
    await user.type(input, "tags:fr");

    const suggestions = screen.getByTestId("filter-suggestions");
    expect(within(suggestions).getByText("frontend")).toBeInTheDocument();

    await user.click(within(suggestions).getByText("frontend"));
    expect(input.value).toBe("tags:frontend");
    expect(prefs.filter.tags).toEqual(["frontend"]);
  });

  it("shows no suggestions for an unknown field", async () => {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn() });

    await user.type(screen.getByTestId("filter-keyword"), "title:fo");
    expect(screen.queryByTestId("filter-suggestions")).not.toBeInTheDocument();
  });
});

// Phase 6: async ID/title typeahead for relationship-id token values. The caret
// inside `parent:<here>` / `blocking:<here>` / … fires a DEBOUNCED search against
// an injected `searchNibs`; candidate nibs render as rich rows (type + title + id
// + status); selecting one inserts its id. The debounce + stale-response guard
// live in Toolbar state, so these are component tests with fake timers. A synthetic
// input event (value + caret set directly) drives typing without coupling userEvent
// to the fake clock.
describe("Toolbar — relationship-id async typeahead", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  const nib = (over: Partial<NibSuggestion> = {}): NibSuggestion => ({
    id: "tnib-abc1",
    title: "Fix login bug",
    type: "bug",
    status: "in-progress",
    ...over,
  });

  // Set the input value + caret and dispatch a synthetic input event, then flush.
  async function typeInto(input: HTMLInputElement, value: string, caret = value.length) {
    input.value = value;
    input.setSelectionRange(caret, caret);
    await fireEvent.input(input);
    await tick();
  }

  const flush = async () => {
    await Promise.resolve();
    await tick();
  };

  function setup(searchNibs: (fragment: string) => Promise<NibSuggestion[]>) {
    const prefs = new Preferences();
    render(Toolbar, { prefs, oncreatenew: vi.fn(), searchNibs });
    const input = screen.getByTestId("filter-keyword") as HTMLInputElement;
    fireEvent.focus(input);
    return { prefs, input };
  }

  it("fires ONE debounced query with the current fragment; rapid keystrokes coalesce", async () => {
    const searchNibs = vi.fn().mockResolvedValue([nib()]);
    const { input } = setup(searchNibs);

    await typeInto(input, "parent:t");
    await typeInto(input, "parent:tn");
    // Still inside the debounce window — no query yet.
    expect(searchNibs).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(200);

    // Exactly one query, for the LAST fragment (the earlier keystroke coalesced).
    expect(searchNibs).toHaveBeenCalledTimes(1);
    expect(searchNibs).toHaveBeenCalledWith("tn");
  });

  it("does not query until the debounce delay elapses", async () => {
    const searchNibs = vi.fn().mockResolvedValue([nib()]);
    const { input } = setup(searchNibs);

    await typeInto(input, "parent:tn");
    await vi.advanceTimersByTimeAsync(199);
    expect(searchNibs).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(1);
    expect(searchNibs).toHaveBeenCalledOnce();
  });

  it("does not query for an empty value (parent: with nothing typed)", async () => {
    const searchNibs = vi.fn().mockResolvedValue([nib()]);
    const { input } = setup(searchNibs);

    await typeInto(input, "parent:");
    await vi.advanceTimersByTimeAsync(500);

    expect(searchNibs).not.toHaveBeenCalled();
    expect(screen.queryByTestId("filter-suggestions")).not.toBeInTheDocument();
  });

  it("renders rich candidate rows: type + title + id + status", async () => {
    const searchNibs = vi
      .fn()
      .mockResolvedValue([nib({ id: "tnib-xyz9", title: "Login flow", type: "feature", status: "todo" })]);
    const { input } = setup(searchNibs);

    await typeInto(input, "parent:log");
    await vi.advanceTimersByTimeAsync(200);
    await flush();

    const list = screen.getByTestId("filter-suggestions");
    expect(within(list).getByText("Login flow")).toBeInTheDocument();
    expect(within(list).getByText("tnib-xyz9")).toBeInTheDocument();
    // Type is carried on the row wrapper; status renders via the reused StatusIcon.
    expect(list.querySelector('[data-nib-type="feature"]')).not.toBeNull();
    expect(within(list).getByTestId("status-icon")).toBeInTheDocument();
  });

  it("selecting a candidate inserts its id and updates the parsed filter", async () => {
    const searchNibs = vi.fn().mockResolvedValue([nib({ id: "tnib-xyz9", title: "Login flow" })]);
    const { prefs, input } = setup(searchNibs);

    await typeInto(input, "parent:log");
    await vi.advanceTimersByTimeAsync(200);
    await flush();

    await fireEvent.click(screen.getByText("Login flow"));
    await flush();

    expect(input.value).toBe("parent:tnib-xyz9");
    expect(prefs.filter.parentId).toBe("tnib-xyz9");
  });

  it("ignores a stale (out-of-order) response — an older in-flight query cannot overwrite a newer one", async () => {
    // Deferred, in call-order, so we can resolve them out of order. Fragment goes
    // a → ab → a, so the two "a" queries have the SAME fragment: only the request
    // sequence guard (not the fragment guard) can drop the stale one.
    const calls: { fragment: string; resolve: (v: NibSuggestion[]) => void }[] = [];
    const searchNibs = vi.fn(
      (fragment: string) =>
        new Promise<NibSuggestion[]>((resolve) => {
          calls.push({ fragment, resolve });
        }),
    );
    const { input } = setup(searchNibs);

    await typeInto(input, "parent:a");
    await vi.advanceTimersByTimeAsync(200); // query 0: fragment "a" (stale-to-be)
    await typeInto(input, "parent:ab");
    await vi.advanceTimersByTimeAsync(200); // query 1: fragment "ab"
    await typeInto(input, "parent:a");
    await vi.advanceTimersByTimeAsync(200); // query 2: fragment "a" (current)

    expect(calls.map((c) => c.fragment)).toEqual(["a", "ab", "a"]);

    // Newest query resolves first and is shown...
    calls[2].resolve([nib({ id: "tnib-new1", title: "Newest" })]);
    await flush();
    // ...then the ORIGINAL "a" query resolves late and must be ignored.
    calls[0].resolve([nib({ id: "tnib-old1", title: "Oldest" })]);
    await flush();

    const list = screen.getByTestId("filter-suggestions");
    expect(within(list).getByText("Newest")).toBeInTheDocument();
    expect(within(list).queryByText("Oldest")).not.toBeInTheDocument();
  });

  it("does NOT take the async path for metadata tokens — static enum completion still shows, no search fires", async () => {
    const searchNibs = vi.fn().mockResolvedValue([nib()]);
    const { input } = setup(searchNibs);

    await typeInto(input, "type:bu");
    await vi.advanceTimersByTimeAsync(500);

    // Static suggestion is shown; the async search is never consulted.
    expect(within(screen.getByTestId("filter-suggestions")).getByText("bug")).toBeInTheDocument();
    expect(searchNibs).not.toHaveBeenCalled();
  });

  it("degrades to no suggestions when the search fn REJECTS (no unhandled rejection / throw)", async () => {
    // A rejecting searchNibs models a urql client that is undefined (App.test's
    // mock) → `undefined.query(...)` rejects, or a real transport error that
    // createNibSearch doesn't swallow. runRelSearch must catch it, not leak it.
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const searchNibs = vi.fn().mockRejectedValue(new Error("boom"));
    const { input } = setup(searchNibs);

    await typeInto(input, "parent:x");
    await vi.advanceTimersByTimeAsync(200);
    await flush();

    // The query fired, but the rejection was swallowed: the popover shows no
    // rel candidate rows and the component did not throw.
    expect(searchNibs).toHaveBeenCalledOnce();
    expect(screen.queryByTestId("filter-suggestions")).not.toBeInTheDocument();
    expect(warn).toHaveBeenCalled();

    warn.mockRestore();
  });
});
