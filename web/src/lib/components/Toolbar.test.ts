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
  ontogglefilters: vi.fn(),
  visibleColumns: [...ALL_COLUMN_KEYS] as ColumnKey[],
  oncolumnschange: vi.fn(),
  oncreatenew: vi.fn(),
};

describe("Toolbar", () => {
  it("renders icon buttons (no search input)", () => {
    render(Toolbar, { ...defaultToolbarProps });

    // Search input has moved to FilterBar
    expect(screen.queryByPlaceholderText(/search/i)).not.toBeInTheDocument();
    expect(screen.getByTitle("New item")).toBeInTheDocument();
    expect(screen.getByTitle("Filters")).toBeInTheDocument();
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

  it("calls ontogglefilters when Filters button is clicked", async () => {
    const ontogglefilters = vi.fn();
    render(Toolbar, { ...defaultToolbarProps, ontogglefilters });

    await user.click(screen.getByTitle("Filters"));

    expect(ontogglefilters).toHaveBeenCalledOnce();
  });

  it("shows active state on Filter button when filtersOpen is true", () => {
    render(Toolbar, {
      ...defaultToolbarProps,
      filtersOpen: true,
    });

    const filtersBtn = screen.getByTitle("Filters");
    expect(filtersBtn).toHaveAttribute("aria-pressed", "true");
  });

  it("shows blue dot on Filter button when advanced filters are active", () => {
    render(Toolbar, {
      ...defaultToolbarProps,
      filter: { type: ["bug"] },
    });

    const filtersBtn = screen.getByTitle("Filters");
    expect(filtersBtn.querySelector("span")).toBeInTheDocument();
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
