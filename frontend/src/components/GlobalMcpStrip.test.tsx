import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { makeServer } from "../test/fixtures";
import { GlobalMcpStrip } from "./GlobalMcpStrip";

function renderStrip(servers = [makeServer({ name: "kubernetes" })]) {
  return render(
    <MemoryRouter>
      <GlobalMcpStrip servers={servers} />
    </MemoryRouter>,
  );
}

describe("GlobalMcpStrip", () => {
  it("links to /servers and lists each server with its state", () => {
    const { container } = renderStrip([
      makeServer({ name: "kubernetes", running: true }),
      makeServer({ name: "playwright", running: false }),
    ]);
    const link = screen.getByRole("link");
    expect(link).toHaveAttribute("href", "/servers");
    expect(link).toHaveTextContent("Global MCP:");
    expect(link).toHaveTextContent("kubernetes");
    expect(link).toHaveTextContent("playwright");
    // filled dot for running, hollow for stopped; hidden state text for both
    expect(container.querySelector(".strip-item.on")).toHaveTextContent(
      "kubernetes",
    );
    expect(container.querySelector(".strip-item.off")).toHaveTextContent(
      "playwright",
    );
    const hidden = [...container.querySelectorAll(".sr-only")].map(
      (n) => n.textContent,
    );
    expect(hidden).toEqual([": running", ": stopped"]);
  });

  it("labels an unnamed server", () => {
    renderStrip([makeServer({ name: "" })]);
    expect(screen.getByRole("link")).toHaveTextContent("(unnamed)");
  });

  it("says none running when there are no global servers", () => {
    renderStrip([]);
    expect(screen.getByRole("link")).toHaveTextContent("none running");
  });
});
