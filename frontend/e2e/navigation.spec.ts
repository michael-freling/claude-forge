import { expect, test } from "@playwright/test";
import { connectJson, RPC_PATH, richDashboard } from "./fixtures";

test.beforeEach(async ({ page }) => {
  await page.route(RPC_PATH, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: connectJson(richDashboard()),
    }),
  );
});

test("lands on Running, navigates to Sessions and Servers via the tabs", async ({
  page,
}) => {
  await page.goto("/");

  // Running (home): only live content — no history table.
  await expect(
    page.getByRole("heading", { name: "wire up dashboard" }),
  ).toBeVisible();
  await expect(page.locator("table.sessions")).toHaveCount(0);
  await expect(page.getByText("Global MCP:")).toBeVisible();
  const runningTab = page.getByRole("link", { name: "Running", exact: true });
  await expect(runningTab).toHaveClass(/active/);

  // Sessions: the per-project history table.
  await page.getByRole("link", { name: "Sessions", exact: true }).click();
  await expect(page).toHaveURL(/\/sessions$/);
  await expect(page.getByRole("heading", { name: "octo/cat" })).toBeVisible();
  await expect(page.locator("table.sessions")).toHaveCount(1);
  await expect(
    page.getByRole("link", { name: "Sessions", exact: true }),
  ).toHaveClass(/active/);

  // Servers: global panel + per-session server sections.
  await page.getByRole("link", { name: "Servers", exact: true }).click();
  await expect(page).toHaveURL(/\/servers$/);
  await expect(
    page.getByRole("heading", { name: "Global MCP servers" }),
  ).toBeVisible();
  await expect(page.getByText("kubernetes", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("heading", { name: /octo\/cat · abcdef12/ }),
  ).toBeVisible();
});

test("deep-linking directly to /sessions works", async ({ page }) => {
  await page.goto("/sessions");
  await expect(page.getByRole("heading", { name: "octo/cat" })).toBeVisible();
  await expect(page.locator("table.sessions")).toHaveCount(1);
  await expect(
    page.getByRole("link", { name: "Sessions", exact: true }),
  ).toHaveClass(/active/);
});
