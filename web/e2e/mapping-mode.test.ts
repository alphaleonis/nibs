import { test, expect, type Locator, type Page } from "@playwright/test";

// Mapping mode puts a milestone panel in the detail pane's slot, and rows dragged
// onto a card there are assigned to that milestone. Only a real engine runs the
// pointer path: jsdom has no `document.elementFromPoint`.
//
// The suite shares one fixture copy, so each case drags nibs no other test moves.

const V11 = "tnib-m002";
const VIEW_TRIGGER = 'button[aria-label^="View:"]';

/** Open the app in the Flat view with no filter, and turn mapping mode on. */
async function openMapping(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem("nibs-filter-preferences", JSON.stringify({ q: "", viewLevel: "flat" }));
  });
  await page.goto("/");
  await expect(page.locator("tr[data-nib-id]").first()).toBeVisible({ timeout: 10_000 });
  await page.getByTestId("mapping-mode-toggle").click();
  await expect(page.getByTestId("mapping-panel")).toBeVisible();
  // The counts render only once the panel's query has answered, and the panel plans from that answer.
  await expect(card(page, V11).getByTestId("mapping-open-count")).toBeVisible();
}

function card(page: Page, value: string): Locator {
  return page.locator(`[data-mapping-target="${value}"]`);
}

function row(page: Page, id: string): Locator {
  return page.locator(`tr[data-nib-id="${id}"]`);
}

async function openCount(page: Page, value: string): Promise<number> {
  return Number(await card(page, value).getByTestId("mapping-open-count").textContent());
}

/** Centers a row in the table, away from the auto-scroll edges, and returns a point on it. */
async function pointOnRow(page: Page, id: string): Promise<{ x: number; y: number }> {
  await page.evaluate((nibId) => {
    document.querySelector(`tr[data-nib-id="${nibId}"]`)?.scrollIntoView({ block: "center" });
  }, id);
  const box = (await row(page, id).boundingBox())!;
  return { x: box.x + 80, y: box.y + box.height / 2 };
}

/** Presses `fromId`, moves past the drag threshold, then onto `to`, and returns a release. */
async function dragFrom(page: Page, fromId: string, to: () => Promise<{ x: number; y: number }>) {
  const from = await pointOnRow(page, fromId);
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  await page.mouse.move(from.x + 12, from.y, { steps: 5 });
  const target = await to();
  await page.mouse.move(target.x, target.y, { steps: 15 });
}

async function centerOf(locator: Locator): Promise<{ x: number; y: number }> {
  const box = (await locator.boundingBox())!;
  return { x: box.x + box.width / 2, y: box.y + box.height / 2 };
}

async function switchView(page: Page, label: string) {
  await page.locator(VIEW_TRIGGER).click();
  await page.getByRole("menuitemradio", { name: label }).click();
  await expect(page.locator(VIEW_TRIGGER)).toHaveAttribute("aria-label", `View: ${label}`);
}

/** The row ids the Milestones view draws in a milestone's section, in order. */
async function sectionIds(page: Page, milestoneId: string): Promise<string[]> {
  return page.evaluate((id) => {
    const ids = [...document.querySelectorAll("tr[data-nib-id]")].map((tr) => (tr as HTMLElement).dataset.nibId!);
    const start = ids.indexOf(id);
    if (start === -1) return [];
    const rest = ids.slice(start + 1);
    // The next section header is another milestone or the Backlog bucket.
    const end = rest.findIndex((r) => r.startsWith("/") || /^tnib-m\d+$/.test(r));
    return end === -1 ? rest : rest.slice(0, end);
  }, milestoneId);
}

async function backlogIds(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const ids = [...document.querySelectorAll("tr[data-nib-id]")].map((tr) => (tr as HTMLElement).dataset.nibId!);
    const start = ids.findIndex((r) => r.startsWith("/"));
    return start === -1 ? [] : ids.slice(start + 1);
  });
}

test("two rows dragged from the Flat view onto a milestone join the end of its queue", async ({ page }) => {
  await openMapping(page);
  const before = await openCount(page, V11);

  // tnib-t039 and tnib-t040 are open, childless and unassigned.
  await row(page, "tnib-t039").locator('[data-action="title"]').click();
  await row(page, "tnib-t040").locator('[data-action="title"]').click({ modifiers: ["ControlOrMeta"] });
  // A click only selects in mapping mode, so the panel is still there.
  await expect(page.getByTestId("mapping-panel")).toBeVisible();

  await dragFrom(page, "tnib-t039", () => centerOf(card(page, V11)));
  await expect(page.getByTestId("drag-badge-label")).toHaveText("Assign to v1.1 Team Collaboration");
  await page.mouse.up();

  await expect.poll(() => openCount(page, V11), { timeout: 10_000 }).toBe(before + 2);
  expect(await page.locator("[data-sonner-toast]").count()).toBe(0);

  await switchView(page, "Milestones");
  await expect
    .poll(async () => (await sectionIds(page, V11)).slice(-2).sort(), { timeout: 10_000 })
    .toEqual(["tnib-t039", "tnib-t040"]);
});

test("a row dropped onto No milestone lands in the Backlog", async ({ page }) => {
  await openMapping(page);

  // tnib-e004 is directly assigned to v1.0.
  await dragFrom(page, "tnib-e004", () => centerOf(card(page, "")));
  await expect(page.getByTestId("drag-badge-label")).toHaveText("Remove from milestone");
  await page.mouse.up();

  await switchView(page, "Milestones");
  await expect.poll(async () => (await backlogIds(page)).includes("tnib-e004"), { timeout: 10_000 }).toBe(true);
  expect(await sectionIds(page, "tnib-m001")).not.toContain("tnib-e004");
});

test("a nib whose ancestor is assigned elsewhere is refused by the server, with a toast", async ({ page }) => {
  await openMapping(page);

  // tnib-t013 is in v1.0 through tnib-e002, two levels up.
  await dragFrom(page, "tnib-t013", () => centerOf(card(page, V11)));
  await page.mouse.up();

  await expect(page.locator("[data-sonner-toast]")).toContainText("already assigned");

  await switchView(page, "Milestones");
  await expect.poll(async () => (await sectionIds(page, "tnib-m001")).includes("tnib-t013"), { timeout: 10_000 }).toBe(true);
  expect(await sectionIds(page, V11)).not.toContain("tnib-t013");
});

test("in the Flat view a drop onto a row is refused with the gate's message, while a panel drop succeeds", async ({ page }) => {
  await openMapping(page);
  const before = await openCount(page, V11);

  await dragFrom(page, "tnib-t034", () => pointOnRow(page, "tnib-t035"));
  await page.mouse.up();
  await expect(page.locator("[data-sonner-toast]")).toContainText("Reordering is off in the Flat view");

  await dragFrom(page, "tnib-t034", () => centerOf(card(page, V11)));
  await page.mouse.up();
  await expect.poll(() => openCount(page, V11), { timeout: 10_000 }).toBe(before + 1);
});

test("a double-clicked row opens in the panel's slot, and closing it brings the panel back", async ({ page }) => {
  await openMapping(page);

  await row(page, "tnib-t044").locator('[data-action="title"]').dblclick();
  await expect(page.getByTestId("active-nib-view")).toBeVisible({ timeout: 5_000 });
  await expect(page.getByTestId("mapping-panel")).toBeHidden();

  await page.getByTestId("anv-close").click();
  await expect(page.getByTestId("active-nib-view")).toBeHidden();
  await expect(page.getByTestId("mapping-panel")).toBeVisible();
});
