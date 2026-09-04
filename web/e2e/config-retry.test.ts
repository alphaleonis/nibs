import { test, expect } from "@playwright/test";

// nibs-zwnm: a config query that fails while the WEBSOCKET stays healthy used to
// pin the session — the only re-ask was gated on socket recovery, and queries
// travel over HTTP, so a 502 from a reverse proxy or a resolver error signalled
// nothing at all. `useLiveConfig` re-asks on a backoff instead.
//
// Only a real engine can close this: it needs a genuine HTTP request separate
// from a genuine WebSocket, so that failing one leaves the other untouched. In
// jsdom both are the same mock and "the socket stayed healthy" is an assumption
// rather than an observation.

test("a config query that fails with the socket healthy heals without a reload", async ({
  page,
}) => {
  // A FLAG rather than a failure count, so the dead end lasts exactly as long as
  // the test wants it to. Counting raced the subject: two failures plus the
  // first two backoff delays healed the page before the assertion could see it
  // unavailable at all.
  let failConfig = true;
  let configAttempts = 0;

  // The CONFIG query alone, and everything else through — the nib list must
  // still load, or the page would be blank for reasons unrelated to this.
  //
  // Matched on the URL, not the body: urql sends these as GET with the operation
  // in the query string, so `postData()` is null and a body matcher silently
  // passes every request through. Read off the parameter rather than searched
  // for as text, so `Config` cannot also match `ConfigChanged`.
  await page.route("**/graphql?*", async (route) => {
    const operation = new URL(route.request().url()).searchParams.get("operationName");
    if (operation === "Config") {
      configAttempts += 1;
      if (failConfig) {
        await route.fulfill({ status: 502, body: "bad gateway" });
        return;
      }
    }
    await route.continue();
  });

  await page.goto("/");
  await expect(page.locator("tr[data-nib-id]").first()).toBeVisible({ timeout: 15_000 });

  await page.locator('button[aria-label^="View:"]').click();
  await page.getByRole("menuitemradio", { name: "Areas" }).click();

  // The dead end, held open by the flag.
  await expect(page.getByTestId("empty-areas-unavailable")).toBeVisible();

  // The socket is untouched by an HTTP failure, so nothing reports a
  // disconnection — which is exactly why nothing used to re-ask.
  await expect(page.getByTestId("connection-status")).toBeHidden();

  const beforeHealing = configAttempts;
  failConfig = false;

  // No reload, no navigation, no click: the backoff is the only thing that can
  // change this page.
  await expect(page.getByRole("row", { name: /^web\b/ }).first()).toBeVisible({ timeout: 20_000 });
  await expect(page.getByTestId("empty-areas-unavailable")).toBeHidden();

  // The heal came from a re-ask, not from the first request finally landing.
  expect(configAttempts).toBeGreaterThan(beforeHealing);
});
