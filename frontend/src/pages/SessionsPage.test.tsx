import { fireEvent, render as rtlRender, screen } from "@testing-library/react";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import {
  makeDashboard,
  makeProject,
  makeRunningSession,
  makeServer,
  makeSession,
} from "../test/fixtures";
import { SessionsPage } from "./SessionsPage";

const at = (iso: string) => timestampFromDate(new Date(iso));

const render = (node: ReactNode) =>
  rtlRender(<MemoryRouter>{node}</MemoryRouter>);

const data = () =>
  makeDashboard({
    projects: [
      makeProject({
        id: "p-idle",
        owner: "octo",
        repo: "idle",
        sessions: [
          makeSession({
            id: "aaaa1111-1",
            name: "old work",
            branch: "feat/old",
            lastActive: at("2026-08-01T00:00:00Z"),
          }),
          makeSession({
            id: "aaaa2222-2",
            name: "new work",
            branch: "feat/new",
            lastActive: at("2026-08-02T00:00:00Z"),
          }),
        ],
      }),
      makeProject({
        id: "p-live",
        owner: "octo",
        repo: "live",
        sessions: [
          makeSession({
            id: "bbbb1111-1",
            name: "running thing",
            firstMessage: "special needle",
            lastActive: at("2026-07-01T00:00:00Z"),
          }),
        ],
        runningSessions: [
          makeRunningSession({
            shortId: "bbbb1111",
            claudeSessionId: "bbbb1111-1",
            mcpServers: [makeServer({ name: "github" })],
          }),
        ],
      }),
    ],
  });

describe("SessionsPage", () => {
  it("puts projects with running sessions first, sessions most-recent first", () => {
    render(<SessionsPage data={data()} />);
    const headings = screen
      .getAllByRole("heading", { level: 2 })
      .map((h) => h.textContent);
    expect(headings).toEqual(["octo/live", "octo/idle"]);

    const names = [...document.querySelectorAll(".s-name")].map((n) =>
      n.textContent?.replace("running:", "").trim(),
    );
    expect(names).toEqual(["running thing", "new work", "old work"]);
  });

  it("marks the running session row with a dot and a servers link", () => {
    const { container } = render(<SessionsPage data={data()} />);
    const dots = container.querySelectorAll(".run-dot");
    expect(dots).toHaveLength(1);
    expect(dots[0].closest("td")).toHaveTextContent("running thing");
    // the row is anchored and links to its server section on /servers
    expect(container.querySelector("#sess-bbbb1111-1")).toBeInTheDocument();
    expect(
      screen.getByRole("link", {
        name: "1 MCP server for session running thing",
      }),
    ).toHaveAttribute("href", "/servers#rs-p-live-bbbb1111");
  });

  it("shows the projects/sessions tally", () => {
    render(<SessionsPage data={data()} />);
    expect(screen.getByText("projects")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    expect(screen.getByText("sessions")).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
  });

  it("filters rows by name/branch/first message and hides empty projects", () => {
    render(<SessionsPage data={data()} />);
    const input = screen.getByRole("searchbox", { name: "Filter sessions" });

    // name match
    fireEvent.change(input, { target: { value: "new work" } });
    expect(screen.getByText("new work")).toBeInTheDocument();
    expect(screen.queryByText("old work")).toBeNull();
    expect(screen.queryByRole("heading", { name: "octo/live" })).toBeNull();

    // first-message match hides the other project entirely
    fireEvent.change(input, { target: { value: "special needle" } });
    expect(screen.getByText("running thing")).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "octo/idle" })).toBeNull();

    // branch match
    fireEvent.change(input, { target: { value: "feat/old" } });
    expect(screen.getByText("old work")).toBeInTheDocument();
    expect(screen.queryByText("new work")).toBeNull();
  });

  it("shows a no-match note when the filter excludes everything", () => {
    render(<SessionsPage data={data()} />);
    fireEvent.change(
      screen.getByRole("searchbox", { name: "Filter sessions" }),
      { target: { value: "zzz-nothing" } },
    );
    expect(screen.getByText(/No sessions match/)).toHaveTextContent(
      "zzz-nothing",
    );
  });

  it("keeps session-less projects visible when not filtering", () => {
    render(
      <SessionsPage
        data={makeDashboard({
          projects: [makeProject({ owner: "octo", repo: "bare", sessions: [] })],
        })}
      />,
    );
    expect(
      screen.getByRole("heading", { name: "octo/bare" }),
    ).toBeInTheDocument();
    expect(screen.getByText("No sessions.")).toBeInTheDocument();
  });

  it("renders the no-projects state for an empty dashboard", () => {
    render(<SessionsPage data={makeDashboard()} />);
    expect(
      screen.getByText("No claude-forge projects found."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("searchbox")).toBeNull();
  });
});
