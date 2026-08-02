import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { makeServer } from "../test/fixtures";
import { GlobalPanel } from "./GlobalPanel";

describe("GlobalPanel", () => {
  it("renders a card per server with a running count", () => {
    render(
      <GlobalPanel
        servers={[
          makeServer({ name: "kubernetes" }),
          makeServer({ name: "" }), // unnamed → key falls back to the index
        ]}
      />,
    );
    expect(screen.getByText("kubernetes")).toBeInTheDocument();
    expect(screen.getByText("(unnamed)")).toBeInTheDocument();
    expect(screen.getByText("2 running")).toBeInTheDocument();
  });

  it("shows an empty note when no global servers are running", () => {
    render(<GlobalPanel servers={[]} />);
    expect(
      screen.getByText("No global MCP servers running."),
    ).toBeInTheDocument();
    expect(screen.getByText("0 running")).toBeInTheDocument();
  });
});
