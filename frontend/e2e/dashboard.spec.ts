import { expect, test } from "@playwright/test";
import { connectJson, RPC_PATH, richDashboard } from "./fixtures";

test("renders projects, sessions, the PR link and MCP pills", async ({
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

  // Project title + session.
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

  // Global MCP server card + running-session MCP pill.
  await expect(page.getByText("kubernetes", { exact: true })).toBeVisible();
  await expect(page.getByText("github", { exact: true })).toBeVisible();

  // Warnings banner.
  await expect(page.getByRole("alert")).toContainText("Warning");
});
