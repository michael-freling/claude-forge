import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { makeServer } from "../test/fixtures";
import { GlobalPanel } from "./GlobalPanel";

describe("GlobalPanel", () => {
  it("counts only servers that are actually running", () => {
    render(
      <GlobalPanel
        servers={[
          makeServer({ name: "kubernetes", running: true }),
          makeServer({ name: "playwright", running: false }),
        ]}
      />,
    );
    expect(screen.getByText("kubernetes")).toBeInTheDocument();
    expect(screen.getByText("playwright")).toBeInTheDocument();
    expect(screen.getByText("2 servers · 1 running")).toBeInTheDocument();
  });

  it("uses the singular label for one server and keys unnamed cards", () => {
    render(<GlobalPanel servers={[makeServer({ name: "" })]} />);
    expect(screen.getByText("(unnamed)")).toBeInTheDocument();
    expect(screen.getByText("1 server · 1 running")).toBeInTheDocument();
  });

  it("shows an empty note when no global servers exist", () => {
    render(<GlobalPanel servers={[]} />);
    expect(
      screen.getByText("No global MCP servers running."),
    ).toBeInTheDocument();
    expect(screen.getByText("0 servers · 0 running")).toBeInTheDocument();
  });
});
