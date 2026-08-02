import { expect, test } from "@playwright/test";
import { connectJson, emptyDashboard, RPC_PATH } from "./fixtures";

test("shows NoProjectsState when there are zero projects", async ({ page }) => {
  await page.route(RPC_PATH, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: connectJson(emptyDashboard()),
    }),
  );

  await page.goto("/");

  await expect(
    page.getByText("No claude-forge projects found."),
  ).toBeVisible();
  // The summary still renders the (plural) projects stat.
  await expect(page.getByText("projects", { exact: true })).toBeVisible();
});
