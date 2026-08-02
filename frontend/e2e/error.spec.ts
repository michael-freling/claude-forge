import { expect, test } from "@playwright/test";
import { connectJson, RPC_PATH, richDashboard } from "./fixtures";

test("shows an ErrorState with retry when the RPC fails, then recovers", async ({
  page,
}) => {
  let mode: "fail" | "ok" = "fail";
  await page.route(RPC_PATH, (route) => {
    if (mode === "fail") {
      return route.fulfill({
        status: 500,
        contentType: "application/json",
        body: JSON.stringify({ code: "internal", message: "boom" }),
      });
    }
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: connectJson(richDashboard()),
    });
  });

  await page.goto("/");

  await expect(page.getByText(/Couldn.t load the dashboard/)).toBeVisible();
  const retry = page.getByRole("button", { name: "Try again" });
  await expect(retry).toBeVisible();

  // Retrying after the backend recovers renders the dashboard.
  mode = "ok";
  await retry.click();
  await expect(page.getByRole("heading", { name: "octo/cat" })).toBeVisible();
});
