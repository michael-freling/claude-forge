import { expect, test } from "@playwright/test";
import { connectJson, RPC_PATH, richDashboard } from "./fixtures";

test("Refresh re-issues the RPC and updates the joined running card", async ({
  page,
}) => {
  // The name is flipped between the initial load and the manual refresh via
  // this variable (not a call counter: React StrictMode issues an extra,
  // aborted call on mount in dev builds).
  let name = "first load";
  let calls = 0;
  await page.route(RPC_PATH, (route) => {
    calls += 1;
    const d = richDashboard();
    d.projects[0].sessions[0].name = name;
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: connectJson(d),
    });
  });

  await page.goto("/");
  await expect(page.getByText("first load")).toBeVisible();

  name = "after refresh";
  await page.getByRole("button", { name: "Refresh" }).click();

  await expect(page.getByText("after refresh")).toBeVisible();
  expect(calls).toBeGreaterThanOrEqual(2);
});
