import { render, screen } from "@testing-library/react";
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
import { RunningPage } from "./RunningPage";

function renderPage(data = makeDashboard()) {
  return render(
    <MemoryRouter>
      <RunningPage data={data} />
    </MemoryRouter>,
  );
}

describe("RunningPage", () => {
  it("renders a card per running session across projects, with the strip", () => {
    renderPage(
      makeDashboard({
        global: makeGlobal([makeServer({ name: "kubernetes" })]),
        projects: [
          makeProject({
            id: "p1",
            owner: "octo",
            repo: "cat",
            sessions: [makeSession({ id: "abcdef12-1", name: "one" })],
            runningSessions: [
              makeRunningSession({
                shortId: "abcdef12",
                claudeSessionId: "abcdef12-1",
              }),
            ],
          }),
          makeProject({
            id: "p2",
            owner: "octo",
            repo: "dog",
            sessions: [makeSession({ id: "beadfeed-1", name: "two" })],
            runningSessions: [
              makeRunningSession({
                shortId: "beadfeed",
                claudeSessionId: "beadfeed-1",
              }),
            ],
          }),
        ],
      }),
    );
    expect(screen.getByRole("heading", { name: "one" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "two" })).toBeInTheDocument();
    expect(screen.getByText("kubernetes")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Global MCP/ })).toHaveAttribute(
      "href",
      "/servers",
    );
  });

  it("shows the calm empty state with a link to /sessions when idle", () => {
    renderPage(
      makeDashboard({
        projects: [makeProject({ sessions: [makeSession()] })],
      }),
    );
    expect(screen.getByText("No sessions running")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Browse past sessions" }),
    ).toHaveAttribute("href", "/sessions");
  });

  it("tolerates an absent global section", () => {
    renderPage(makeDashboard({ global: undefined }));
    expect(screen.getByRole("link", { name: /Global MCP/ })).toHaveTextContent(
      "none running",
    );
  });
});
