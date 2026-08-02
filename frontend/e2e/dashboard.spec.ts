import { expect, test } from "@playwright/test";
import { connectJson, RPC_PATH, richDashboard } from "./fixtures";

test("renders the running session, session history, the PR link and MCP pills", async ({
  page,
}) => {
  await page.route(RPC_PATH, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: connectJson(richDashboard()),
    }),
  );

  await page.goto("/");

  // Home: the running session card, joined to its recorded session.
  await expect(
    page.getByRole("heading", { name: "wire up dashboard" }),
  ).toBeVisible();
  // Running-session MCP pill + global strip.
  await expect(page.getByText("github", { exact: true })).toBeVisible();
  await expect(page.getByText("kubernetes", { exact: true })).toBeVisible();

  // Warnings render as an amber status note, not an alert.
  await expect(
    page.getByRole("status").filter({ hasText: "Warning" }),
  ).toContainText("Warning");
  await expect(page.getByRole("alert")).toHaveCount(0);

  // History lives on /sessions.
  await page.getByRole("link", { name: "Sessions", exact: true }).click();
  await expect(page.getByRole("heading", { name: "octo/cat" })).toBeVisible();
  await expect(page.getByText("wire up dashboard")).toBeVisible();

  // PR chip is a real link, coloured for the open state.
  const pr = page.getByRole("link", { name: /#128/ });
  await expect(pr).toBeVisible();
  await expect(pr).toHaveAttribute(
    "href",
    "https://github.com/octo/cat/pull/128",
  );
  await expect(pr).toHaveAttribute("target", "_blank");
  await expect(pr).toHaveClass(/pr-open/);
});
