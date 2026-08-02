import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import {
  makeDashboard,
  makeGlobal,
  makeProject,
  makeRunningSession,
  makeServer,
  makeSession,
} from "../test/fixtures";
import { ServersPage } from "./ServersPage";

const renderPage = (node: ReactNode) =>
  render(<MemoryRouter>{node}</MemoryRouter>);

describe("ServersPage", () => {
  it("renders the global panel and an attributed section per running session", () => {
    const { container } = renderPage(
      <ServersPage
        data={makeDashboard({
          global: makeGlobal([makeServer({ name: "kubernetes" })]),
          projects: [
            makeProject({
              id: "-home-p",
              owner: "octo",
              repo: "cat",
              sessions: [
                makeSession({ id: "abcdef12-3456", name: "wire it up" }),
              ],
              runningSessions: [
                makeRunningSession({
                  shortId: "abcdef12",
                  claudeSessionId: "abcdef12-3456",
                  mcpServers: [makeServer({ name: "github" })],
                }),
                makeRunningSession({
                  shortId: "feedbeef",
                  claudeSessionId: "",
                  mcpServers: [],
                }),
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

    // joined session: project · linked session name, project-scoped anchor
    expect(
      screen.getByRole("heading", { name: /octo\/cat · wire it up/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "wire it up" }),
    ).toHaveAttribute("href", "/sessions#sess-abcdef12-3456");
    expect(container.querySelector("#rs--home-p-abcdef12")).toHaveClass(
      "sess-srv",
    );
    expect(screen.getByText("github")).toBeInTheDocument();

    // unjoined session: bare shortId chip, no link, still anchored
    const fallback = screen.getByRole("heading", {
      name: /octo\/cat · feedbeef/,
    });
    expect(fallback.querySelector("a")).toBeNull();
    expect(fallback.querySelector(".mono")).toHaveTextContent("feedbeef");
    expect(container.querySelector("#rs--home-p-feedbeef")).toHaveClass(
      "sess-srv",
    );
    expect(screen.getByText("No session MCP servers.")).toBeInTheDocument();
    expect(screen.getByText("2 running sessions")).toBeInTheDocument();
  });

  it("italicises and links a matched-but-nameless session", () => {
    renderPage(
      <ServersPage
        data={makeDashboard({
          projects: [
            makeProject({
              owner: "octo",
              repo: "cat",
              sessions: [makeSession({ id: "abcdef12-3456", name: "  " })],
              runningSessions: [
                makeRunningSession({
                  shortId: "abcdef12",
                  claudeSessionId: "abcdef12-3456",
                }),
              ],
            }),
          ],
        })}
      />,
    );
    const link = screen.getByRole("link", { name: "(unnamed)" });
    expect(link).toHaveClass("unnamed");
    expect(link).toHaveAttribute("href", "/sessions#sess-abcdef12-3456");
  });

  it("em-dashes a running session with no shortId and no join", () => {
    renderPage(
      <ServersPage
        data={makeDashboard({
          projects: [
            makeProject({
              owner: "octo",
              repo: "cat",
              runningSessions: [
                makeRunningSession({ shortId: "", claudeSessionId: "" }),
              ],
            }),
          ],
        })}
      />,
    );
    expect(
      screen.getByRole("heading", { name: /octo\/cat · —/ }),
    ).toBeInTheDocument();
  });

  it("notes when nothing is running and tolerates an absent global section", () => {
    renderPage(<ServersPage data={makeDashboard({ global: undefined })} />);
    expect(screen.getByText("No running sessions.")).toBeInTheDocument();
    expect(screen.getByText("0 running sessions")).toBeInTheDocument();
    expect(
      screen.getByText("No global MCP servers running."),
    ).toBeInTheDocument();
  });
});
