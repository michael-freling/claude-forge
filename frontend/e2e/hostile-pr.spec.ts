import { expect, test } from "@playwright/test";
import { connectJson, hostilePrDashboard, RPC_PATH } from "./fixtures";

test("never renders a navigable link for a javascript: PR url", async ({
  page,
}) => {
  await page.route(RPC_PATH, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: connectJson(hostilePrDashboard()),
    }),
  );

  await page.goto("/");

  // The PR chip still renders...
  await expect(page.getByText("#66")).toBeVisible();
  // ...but as an inert span, not a link.
  await expect(page.getByRole("link", { name: /#66/ })).toHaveCount(0);
  // And there is no anchor anywhere carrying a javascript: href.
  await expect(page.locator('a[href^="javascript:"]')).toHaveCount(0);
});
