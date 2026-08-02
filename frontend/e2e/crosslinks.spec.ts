import { expect, test } from "@playwright/test";
import { connectJson, RPC_PATH, richDashboard } from "./fixtures";

// richDashboard: project "-home-user-octo-cat", running shortId "abcdef12",
// joined session "wire up dashboard" with one session MCP server.
const SERVERS_ANCHOR = "#rs--home-user-octo-cat-abcdef12";
const SESSION_ANCHOR = "#sess-abcdef12-3456-7890-abcd-ef1234567890";

test.beforeEach(async ({ page }) => {
  await page.route(RPC_PATH, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: connectJson(richDashboard()),
    }),
  );
});

test("a running session row links to its server section on /servers", async ({
  page,
}) => {
  await page.goto("/sessions");

  const link = page.getByRole("link", {
    name: "1 MCP server for session wire up dashboard",
  });
  await expect(link).toHaveText("1 server →");
  await link.click();

  await expect(page).toHaveURL(/\/servers#rs--home-user-octo-cat-abcdef12$/);
  const section = page.locator(SERVERS_ANCHOR);
  await expect(section).toHaveClass(/anchor-flash/);
  await expect(section).toBeInViewport();
  await expect(
    section.getByRole("heading", { name: /octo\/cat · wire up dashboard/ }),
  ).toBeVisible();
});

test("a server section's session name links back to its row on /sessions", async ({
  page,
}) => {
  await page.goto("/servers");

  await page.getByRole("link", { name: "wire up dashboard" }).click();

  await expect(page).toHaveURL(
    /\/sessions#sess-abcdef12-3456-7890-abcd-ef1234567890$/,
  );
  const row = page.locator(SESSION_ANCHOR);
  await expect(row).toBeInViewport();
  await expect(row).toContainText("wire up dashboard");
});

test("a running card's servers link lands on that session's server section", async ({
  page,
}) => {
  await page.goto("/");

  await page
    .getByRole("link", { name: "1 MCP server for session wire up dashboard" })
    .click();

  await expect(page).toHaveURL(/\/servers#rs--home-user-octo-cat-abcdef12$/);
  await expect(page.locator(SERVERS_ANCHOR)).toBeInViewport();
});

test("deep-linking straight to a server section anchors it after load", async ({
  page,
}) => {
  // The target only renders once the snapshot arrives; ScrollToAnchor polls.
  await page.goto(`/servers${SERVERS_ANCHOR}`);
  await expect(page.locator(SERVERS_ANCHOR)).toBeInViewport();
});
