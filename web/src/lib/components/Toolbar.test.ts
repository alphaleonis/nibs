import { render, screen, within } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import Toolbar from "./Toolbar.svelte";
import { Preferences } from "../preferences.svelte";
import { ALL_COLUMN_KEYS, DEFAULT_VISIBLE_COLUMNS } from "../types";
import { OPEN_STATUSES, OPEN_PLUS_DEFERRED_STATUSES } from "../constants";
import type { NibFilter, ViewLevel, ColumnKey } from "../types";

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
    expect(labels).toContain("State");
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
    expect(callArg).toEqual(["id", "parent", "type", "title", "state", "effort", "tags", "created", "modified"]);
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
    expect(callArg).toEqual(["id", "parent", "type", "title", "state", "effort", "tags", "blocking", "modified"]);
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
      visibleColumns: ["id", "parent", "title", "state", "effort", "tags"] as ColumnKey[],
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
    expect(callArg).toEqual(["id", "parent", "type", "title", "state", "effort", "tags"]);
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

    await user.click(screen.getByRole("button", { name: /state/i }));
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

    await user.click(screen.getByRole("button", { name: /state/i }));
    await user.click(screen.getByTestId("state-preset-open"));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0].status).toEqual([...OPEN_STATUSES]);
  });

  it("clicking the 'Open + deferred' preset overwrites status with that set", async () => {
    const onchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, onchange });

    await user.click(screen.getByRole("button", { name: /state/i }));
    await user.click(screen.getByTestId("state-preset-open-deferred"));

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

    await user.click(screen.getByRole("button", { name: /state/i }));
    await user.click(screen.getByTestId("state-preset-open"));

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

    await user.click(screen.getByRole("button", { name: /state/i }));
    await user.click(screen.getByTestId("state-preset-open-deferred"));

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

    await user.click(screen.getByRole("button", { name: /state/i }));
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
    const stateBadge = screen.getByRole("button", { name: /state/i }).querySelector("span");
    expect(stateBadge?.classList.contains("invisible")).toBe(true);

    const effortBadge = screen.getByRole("button", { name: /effort/i }).querySelector("span");
    expect(effortBadge?.classList.contains("invisible")).toBe(true);
  });
});
