import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import Toolbar from "./Toolbar.svelte";
import { ALL_COLUMN_KEYS } from "../types";
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
  it("renders New button, keyword input, filter dropdowns, and view controls", () => {
    render(Toolbar, { ...defaultToolbarProps });

    expect(screen.getByTitle("New item")).toBeInTheDocument();
    expect(screen.getByTitle("New item")).toHaveTextContent("New");
    expect(screen.getByTestId("filter-keyword")).toBeInTheDocument();
    expect(screen.getByTitle("Options")).toBeInTheDocument();
    expect(screen.getByTitle("Columns")).toBeInTheDocument();
  });

  it("renders view selector button showing current view label", () => {
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "milestones" });
    expect(screen.getByTitle("Select view")).toHaveTextContent("Milestones");
  });

  it("shows 'Epics' label when viewLevel is epics", () => {
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "epics" });
    expect(screen.getByTitle("Select view")).toHaveTextContent("Epics");
  });

  it("shows 'Backlog Items' label when viewLevel is backlog", () => {
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "backlog" });
    expect(screen.getByTitle("Select view")).toHaveTextContent("Backlog Items");
  });

  it("view selector button is enabled (not disabled)", () => {
    render(Toolbar, { ...defaultToolbarProps });
    expect(screen.getByTitle("Select view")).not.toBeDisabled();
  });

  it("opens dropdown with three view options when view selector is clicked", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    await user.click(screen.getByTitle("Select view"));

    // All three view levels should appear as radio items
    const radioItems = screen.getAllByRole("menuitemradio");
    expect(radioItems).toHaveLength(3);
    expect(screen.getByRole("menuitemradio", { name: /Milestones/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitemradio", { name: /Epics/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitemradio", { name: /Backlog Items/i })).toBeInTheDocument();
  });

  it("calls onviewlevelchange and closes dropdown when an option is clicked", async () => {
    const onviewlevelchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "milestones", onviewlevelchange });

    await user.click(screen.getByTitle("Select view"));
    await user.click(screen.getByRole("menuitemradio", { name: /Epics/i }));

    expect(onviewlevelchange).toHaveBeenCalledWith("epics");
  });

  it("closes dropdown on second click of view selector button", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    const viewBtn = screen.getByTitle("Select view");
    await user.click(viewBtn);
    expect(screen.getAllByRole("menuitemradio").length).toBeGreaterThan(0);

    await user.click(viewBtn);
    expect(screen.queryAllByRole("menuitemradio")).toHaveLength(0);
  });

  it("opens type dropdown with all 5 nib types when New item is clicked", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    await user.click(screen.getByTestId("toolbar-add"));

    // All 5 nib types should appear as menu items
    const items = screen.getAllByRole("menuitem");
    expect(items).toHaveLength(5);
    expect(screen.getByTestId("toolbar-add-milestone")).toBeInTheDocument();
    expect(screen.getByTestId("toolbar-add-epic")).toBeInTheDocument();
    expect(screen.getByTestId("toolbar-add-bug")).toBeInTheDocument();
    expect(screen.getByTestId("toolbar-add-feature")).toBeInTheDocument();
    expect(screen.getByTestId("toolbar-add-task")).toBeInTheDocument();
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

    expect(screen.getByTitle("Columns")).not.toBeDisabled();
  });

  it("opens Options dropdown when Options button is clicked", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    const optionsBtn = screen.getByTitle("Options");
    await user.click(optionsBtn);

    expect(screen.getByText("Include completed")).toBeInTheDocument();
    expect(screen.getByRole("menuitemcheckbox")).toBeInTheDocument();
  });

  it("closes Options dropdown on second click", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    const optionsBtn = screen.getByTitle("Options");
    await user.click(optionsBtn);
    expect(screen.getByRole("menuitemcheckbox")).toBeInTheDocument();

    await user.click(optionsBtn);
    expect(screen.queryByRole("menuitemcheckbox")).not.toBeInTheDocument();
  });

  it("Include Completed checkbox is checked by default (no excludeStatus)", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    const optionsBtn = screen.getByTitle("Options");
    await user.click(optionsBtn);

    const toggle = screen.getByRole("menuitemcheckbox");
    expect(toggle).toHaveAttribute("data-state", "checked");
  });

  it("Include Completed checkbox is unchecked when excludeStatus is set", async () => {
    render(Toolbar, {
      ...defaultToolbarProps,
      filter: { excludeStatus: ["completed", "scrapped"] },
    });

    const optionsBtn = screen.getByTitle("Options");
    await user.click(optionsBtn);

    const toggle = screen.getByRole("menuitemcheckbox");
    expect(toggle).toHaveAttribute("data-state", "unchecked");
  });

  it("emits excludeStatus when Include Completed is unchecked", async () => {
    const onchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, onchange });

    // Open options
    await user.click(screen.getByTitle("Options"));
    // Toggle off "Include completed" (currently checked)
    await user.click(screen.getByRole("menuitemcheckbox"));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0]).toMatchObject({
      excludeStatus: ["completed", "scrapped"],
    });
  });

  it("removes excludeStatus when Include Completed is checked", async () => {
    const onchange = vi.fn();
    render(Toolbar, {
      ...defaultToolbarProps,
      filter: { excludeStatus: ["completed", "scrapped"] },
      onchange,
    });

    await user.click(screen.getByTitle("Options"));
    await user.click(screen.getByRole("menuitemcheckbox"));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0].excludeStatus).toBeUndefined();
  });

  it("preserves existing filter fields when Include Completed is toggled", async () => {
    const onchange = vi.fn();
    render(Toolbar, {
      ...defaultToolbarProps,
      filter: { search: "auth" },
      onchange,
    });

    await user.click(screen.getByTitle("Options"));
    await user.click(screen.getByRole("menuitemcheckbox"));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0]).toMatchObject({
      search: "auth",
      excludeStatus: ["completed", "scrapped"],
    });
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

  // Columns dropdown tests
  it("opens Columns dropdown when Columns button is clicked", async () => {
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "epics" as ViewLevel });

    await user.click(screen.getByTitle("Columns"));

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

    await user.click(screen.getByTitle("Columns"));

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

    await user.click(screen.getByTitle("Columns"));

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

  it("closes Columns dropdown on second click", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    const columnsBtn = screen.getByTitle("Columns");
    await user.click(columnsBtn);
    expect(screen.getAllByRole("menuitemcheckbox").length).toBeGreaterThan(0);

    await user.click(columnsBtn);
    expect(screen.queryAllByRole("menuitemcheckbox")).toHaveLength(0);
  });

  it("Parent column is not shown in Columns checklist when viewLevel is milestones", async () => {
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "milestones" as ViewLevel });

    await user.click(screen.getByTitle("Columns"));

    const items = screen.getAllByRole("menuitemcheckbox");
    const labels = items.map(item => item.textContent?.trim());
    expect(labels).not.toContain("Parent");
    // But other columns should be there
    expect(labels).toContain("ID");
    expect(labels).toContain("Title");
  });

  it("Parent column is shown in Columns checklist when viewLevel is epics", async () => {
    render(Toolbar, { ...defaultToolbarProps, viewLevel: "epics" as ViewLevel });

    await user.click(screen.getByTitle("Columns"));

    const items = screen.getAllByRole("menuitemcheckbox");
    const labels = items.map(item => item.textContent?.trim());
    expect(labels).toContain("Parent");
  });

  it("shows row density radio items in Options dropdown", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    await user.click(screen.getByTitle("Options"));

    const radioItems = screen.getAllByRole("menuitemradio");
    expect(radioItems).toHaveLength(2);
    expect(screen.getByRole("menuitemradio", { name: /Compact/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitemradio", { name: /Comfortable/i })).toBeInTheDocument();
  });

  it("Compact radio item is checked when rowDensity is compact", async () => {
    render(Toolbar, { ...defaultToolbarProps, rowDensity: "compact" });

    await user.click(screen.getByTitle("Options"));

    expect(screen.getByRole("menuitemradio", { name: /Compact/i })).toHaveAttribute("data-state", "checked");
    expect(screen.getByRole("menuitemradio", { name: /Comfortable/i })).toHaveAttribute("data-state", "unchecked");
  });

  it("Comfortable radio item is checked when rowDensity is comfortable", async () => {
    render(Toolbar, { ...defaultToolbarProps, rowDensity: "comfortable" });

    await user.click(screen.getByTitle("Options"));

    expect(screen.getByRole("menuitemradio", { name: /Comfortable/i })).toHaveAttribute("data-state", "checked");
    expect(screen.getByRole("menuitemradio", { name: /Compact/i })).toHaveAttribute("data-state", "unchecked");
  });

  it("clicking Comfortable calls ondensitychange when compact is active", async () => {
    const ondensitychange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, rowDensity: "compact", ondensitychange });

    await user.click(screen.getByTitle("Options"));
    await user.click(screen.getByRole("menuitemradio", { name: /Comfortable/i }));

    expect(ondensitychange).toHaveBeenCalledWith("comfortable");
  });

  it("clicking Compact calls ondensitychange when comfortable is active", async () => {
    const ondensitychange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, rowDensity: "comfortable", ondensitychange });

    await user.click(screen.getByTitle("Options"));
    await user.click(screen.getByRole("menuitemradio", { name: /Compact/i }));

    expect(ondensitychange).toHaveBeenCalledWith("compact");
  });

  it("does not render standalone density toggle button", () => {
    render(Toolbar, { ...defaultToolbarProps });

    expect(screen.queryByTestId("toolbar-density")).not.toBeInTheDocument();
  });

  it("Options dropdown shows both Include completed checkbox and density radio items", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    await user.click(screen.getByTitle("Options"));

    // Checkbox for Include completed
    const checkbox = screen.getByRole("menuitemcheckbox");
    expect(checkbox).toBeInTheDocument();
    expect(checkbox).toHaveTextContent("Include completed");

    // Radio items for density
    const radioItems = screen.getAllByRole("menuitemradio");
    expect(radioItems).toHaveLength(2);
  });

  it("toggling Include completed still emits filter after density items added", async () => {
    const onchange = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, onchange });

    await user.click(screen.getByTitle("Options"));
    await user.click(screen.getByRole("menuitemcheckbox"));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0]).toMatchObject({
      excludeStatus: ["completed", "scrapped"],
    });
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

    await user.click(screen.getByTitle("Columns"));

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

// Filter-dropdown coverage ported from the deleted FilterBar.test.ts (nibs-oqr8).
// The filter-toggle logic (toggleArrayValue/handleToggle, mutual exclusion,
// per-category Clear, status conflict resolution, count badges) was moved verbatim
// from FilterBar into Toolbar during the nibs-5a8k design-system refactor, but its
// only test coverage lived in FilterBar.test.ts, which was deleted with the component.
describe("Toolbar — filter dropdowns", () => {
  it("opens the Type dropdown with all 5 type checkboxes when its trigger is clicked", async () => {
    render(Toolbar, { ...defaultToolbarProps });

    // No type checkboxes visible initially
    expect(screen.queryByRole("menuitemcheckbox", { name: "bug" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /type/i }));

    expect(screen.getByRole("menuitemcheckbox", { name: "bug" })).toBeInTheDocument();
    expect(screen.getByRole("menuitemcheckbox", { name: "feature" })).toBeInTheDocument();
    expect(screen.getByRole("menuitemcheckbox", { name: "task" })).toBeInTheDocument();
    expect(screen.getByRole("menuitemcheckbox", { name: "milestone" })).toBeInTheDocument();
    expect(screen.getByRole("menuitemcheckbox", { name: "epic" })).toBeInTheDocument();
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

  it("resolves a conflicting status away when checked (resolveStatusConflicts)", async () => {
    const onchange = vi.fn();
    render(Toolbar, {
      ...defaultToolbarProps,
      filter: { excludeStatus: ["completed", "scrapped"] },
      onchange,
    });

    await user.click(screen.getByRole("button", { name: /state/i }));
    // "completed" is in excludeStatus, so checking it must be resolved away
    await user.click(screen.getByRole("menuitemcheckbox", { name: "completed" }));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0].status).toBeUndefined();
    expect(lastCall[0].excludeStatus).toEqual(["completed", "scrapped"]);
  });

  it("emits a non-conflicting status and preserves excludeStatus", async () => {
    const onchange = vi.fn();
    render(Toolbar, {
      ...defaultToolbarProps,
      filter: { excludeStatus: ["completed", "scrapped"] },
      onchange,
    });

    await user.click(screen.getByRole("button", { name: /state/i }));
    await user.click(screen.getByRole("menuitemcheckbox", { name: "todo" }));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0].status).toEqual(["todo"]);
    expect(lastCall[0].excludeStatus).toEqual(["completed", "scrapped"]);
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
