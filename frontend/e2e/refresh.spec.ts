import { expect, test } from "@playwright/test";
import { connectJson, RPC_PATH, richDashboard } from "./fixtures";

test("Refresh re-issues the RPC and updates the view", async ({ page }) => {
  let calls = 0;
  await page.route(RPC_PATH, (route) => {
    calls += 1;
    const d = richDashboard();
    d.projects[0].sessions[0].name = calls === 1 ? "first load" : "after refresh";
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: connectJson(d),
    });
  });

  await page.goto("/");
  await expect(page.getByText("first load")).toBeVisible();

  await page.getByRole("button", { name: "Refresh" }).click();

  await expect(page.getByText("after refresh")).toBeVisible();
  expect(calls).toBeGreaterThanOrEqual(2);
});
