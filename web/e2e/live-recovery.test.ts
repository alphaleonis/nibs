import { test, expect } from "@playwright/test";

// The panel must catch up on its own after the live connection drops and
// returns. This is the defect that prompted the work (nibs-1seo): the socket was
// configured to give up permanently, nothing re-established it when the page
// came back, and nothing refetched — so the panel served pre-drop cached data
// indefinitely with no indication.
//
// Only a real engine can cover this: it needs a genuine WebSocket, a real
// offline transition, and urql's real cache. The browser's own bfcache is not
// drivable headlessly, but `context.setOffline` reaches the same end state — a
// dead socket plus a stale cache — which is what the recovery has to handle.
//
// What this DOES guard, measured rather than assumed: the catch-up path —
// refetching the detail query and re-arming the one-shot seed so the fresh
// result can reach the buffer. Removing `invalidateDetailSeed` fails it, because
// the refetched snapshot is discarded as already-seeded and the panel keeps
// rendering its pre-drop body.
//
// What it does NOT guard, so nobody reads more into a green run than is there:
// a short offline blip reconnects within graphql-ws's DEFAULT five retries, so
// this still passes with the retry configuration reverted. Those settings —
// `retryAttempts: Infinity` and `shouldRetry` — are what survive a LONG or fatal
// outage, and they are pinned in graphql.test.ts. The `pageshow`/`online`/
// visibility listeners are likewise covered in
// useConnectionRecovery.svelte.test.ts; the bfcache restore they exist for
// cannot be driven headlessly at all.

const MARKER = "EDITED-WHILE-DISCONNECTED";

test("the detail panel catches up after the connection drops and returns", async ({
  page,
  context,
  request,
}) => {
  await page.goto("/");
  const row = page.locator("tr[data-nib-id]").first();
  await expect(row).toBeVisible({ timeout: 10_000 });
  const nibId = await row.getAttribute("data-nib-id");

  await row.locator('[data-action="title"]').click();
  const panel = page.locator('[data-testid="active-nib-view"]');
  await expect(panel).toBeVisible({ timeout: 5_000 });
  await expect(panel).not.toContainText(MARKER);

  // Prove later that recovery happened in the live page rather than via a
  // reload, which would trivially show fresh data.
  await page.evaluate(() => {
    (window as unknown as { __notReloaded: boolean }).__notReloaded = true;
  });

  await context.setOffline(true);

  // Edit through a request context of its own, so the mutation still reaches the
  // server while the browser is offline — standing in for the agent that edits
  // a nib while the page cannot hear about it.
  const res = await request.post("/graphql", {
    data: {
      query: `mutation ($id: ID!, $body: String!) {
        updateNib(id: $id, input: { body: $body }) { id }
      }`,
      variables: { id: nibId, body: `## Description\n\n${MARKER}\n` },
    },
  });
  expect(res.ok()).toBe(true);
  expect((await res.json()).errors).toBeUndefined();

  // Still offline: the page cannot possibly know yet.
  await expect(panel).not.toContainText(MARKER);

  await context.setOffline(false);

  // No reload, no click, no manual refresh — the page recovers by itself.
  await expect(panel).toContainText(MARKER, { timeout: 15_000 });
  expect(
    await page.evaluate(() => (window as unknown as { __notReloaded?: boolean }).__notReloaded),
  ).toBe(true);
});
