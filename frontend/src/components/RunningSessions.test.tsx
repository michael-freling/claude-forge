import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { makeRunningSession, makeServer } from "../test/fixtures";
import { RunningSessions } from "./RunningSessions";

describe("RunningSessions", () => {
  it("shows an empty note when there are no running sessions", () => {
    render(<RunningSessions sessions={[]} />);
    expect(screen.getByText("No running sessions.")).toBeInTheDocument();
  });

  it("renders a row with its short id and MCP pills", () => {
    render(
      <RunningSessions
        sessions={[
          makeRunningSession({
            shortId: "abcdef12",
            mcpServers: [
              makeServer({ name: "github" }),
              makeServer({ name: "" }), // unnamed → key falls back to the index
            ],
          }),
        ]}
      />,
    );
    expect(screen.getByText("abcdef12")).toBeInTheDocument();
    expect(screen.getByText("github")).toBeInTheDocument();
  });

  it("notes when a running session has no MCP servers, and falls back the id", () => {
    render(
      <RunningSessions
        sessions={[makeRunningSession({ shortId: "", mcpServers: [] })]}
      />,
    );
    expect(screen.getByText("no session MCP servers")).toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});
