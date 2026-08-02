import { expect, test } from "@playwright/test";
import {
  connectJson,
  RPC_PATH,
  richDashboard,
  unjoinedDashboard,
} from "./fixtures";

test("a running session shows its joined recorded-session metadata", async ({
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

  const card = page.locator(".run-card");
  // Joined name, project, shortId.
  await expect(
    card.getByRole("heading", { name: "wire up dashboard" }),
  ).toBeVisible();
  await expect(card.getByText("octo/cat")).toBeVisible();
  await expect(card.getByText("abcdef12")).toBeVisible();
  // Joined branch, worktree, and PR badge.
  await expect(card.getByText("feat/dashboard")).toBeVisible();
  await expect(card.getByText("web-dashboard")).toBeVisible();
  await expect(card.getByRole("link", { name: /#128/ })).toHaveAttribute(
    "href",
    "https://github.com/octo/cat/pull/128",
  );
  // Session-scope MCP pill.
  await expect(card.getByText("github", { exact: true })).toBeVisible();
});

test("a running session without a recorded match falls back to its shortId", async ({
  page,
}) => {
  await page.route(RPC_PATH, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: connectJson(unjoinedDashboard()),
    }),
  );

  await page.goto("/");

  const card = page.locator(".run-card");
  await expect(card.getByRole("heading", { name: "ffffffff" })).toBeVisible();
  await expect(
    card.getByText("no recorded session matches this id"),
  ).toBeVisible();
  // No stray joined metadata.
  await expect(card.getByText("feat/dashboard")).toHaveCount(0);
});
