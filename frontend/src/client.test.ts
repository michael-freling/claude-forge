import { describe, expect, it } from "vitest";
import { createDashboardClient } from "./client";

describe("createDashboardClient", () => {
  it("creates a client exposing getDashboard (default baseUrl)", () => {
    const client = createDashboardClient();
    expect(typeof client.getDashboard).toBe("function");
  });

  it("accepts a custom baseUrl", () => {
    const client = createDashboardClient("http://example.test/api/");
    expect(typeof client.getDashboard).toBe("function");
  });
});
