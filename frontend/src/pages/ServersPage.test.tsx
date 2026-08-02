import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  makeDashboard,
  makeGlobal,
  makeProject,
  makeRunningSession,
  makeServer,
} from "../test/fixtures";
import { ServersPage } from "./ServersPage";

describe("ServersPage", () => {
  it("renders the global panel and a section per running session", () => {
    render(
      <ServersPage
        data={makeDashboard({
          global: makeGlobal([makeServer({ name: "kubernetes" })]),
          projects: [
            makeProject({
              owner: "octo",
              repo: "cat",
              runningSessions: [
                makeRunningSession({
                  shortId: "abcdef12",
                  mcpServers: [makeServer({ name: "github" })],
                }),
                makeRunningSession({ shortId: "feedbeef", mcpServers: [] }),
              ],
            }),
          ],
        })}
      />,
    );
    expect(
      screen.getByRole("heading", { name: "Global MCP servers" }),
    ).toBeInTheDocument();
    expect(screen.getByText("kubernetes")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: /octo\/cat · abcdef12/ }),
    ).toBeInTheDocument();
    expect(screen.getByText("github")).toBeInTheDocument();
    // the second running session has no session servers
    expect(
      screen.getByRole("heading", { name: /octo\/cat · feedbeef/ }),
    ).toBeInTheDocument();
    expect(screen.getByText("No session MCP servers.")).toBeInTheDocument();
    expect(screen.getByText("2 running sessions")).toBeInTheDocument();
  });

  it("notes when nothing is running and tolerates an absent global section", () => {
    render(<ServersPage data={makeDashboard({ global: undefined })} />);
    expect(screen.getByText("No running sessions.")).toBeInTheDocument();
    expect(screen.getByText("0 running sessions")).toBeInTheDocument();
    expect(
      screen.getByText("No global MCP servers running."),
    ).toBeInTheDocument();
  });
});
