import { expect, test } from "@playwright/test";
import { connectJson, emptyDashboard, RPC_PATH } from "./fixtures";

test("shows the calm running empty state, then NoProjectsState on /sessions", async ({
  page,
}) => {
  await page.route(RPC_PATH, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: connectJson(emptyDashboard()),
    }),
  );

  await page.goto("/");

  // Nothing running: calm empty state linking to the history page.
  await expect(page.getByText("No sessions running")).toBeVisible();
  await expect(page.getByText("Global MCP:")).toBeVisible();

  await page.getByRole("link", { name: "Browse past sessions" }).click();
  await expect(page).toHaveURL(/\/sessions$/);

  // Zero projects: the empty state offers a copyable start command.
  await expect(page.getByText("No claude-forge projects found.")).toBeVisible();
  await expect(page.locator("code", { hasText: "claude-forge start" })).toBeVisible();
  await expect(
    page.getByRole("button", { name: /Copy command/ }),
  ).toBeVisible();
});
