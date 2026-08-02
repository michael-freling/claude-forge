import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { makePR, makeRunningSession, makeSession } from "../test/fixtures";
import { SessionsTable } from "./SessionsTable";

describe("SessionsTable", () => {
  it("shows an empty note when there are no sessions", () => {
    render(<SessionsTable sessions={[]} label="octo/cat" />);
    expect(screen.getByText("No sessions.")).toBeInTheDocument();
  });

  it("renders a populated row with all columns in a named, focusable region", () => {
    render(
      <SessionsTable
        label="octo/cat"
        sessions={[
          makeSession({
            name: "wire it up",
            branch: "feat/x",
            worktree: "wt-1",
            firstMessage: "Do the thing",
            id: "abcdef12-3456",
            pr: makePR({ number: 7 }),
          }),
        ]}
      />,
    );
    expect(screen.getByText("wire it up")).toBeInTheDocument();
    expect(screen.getByText("feat/x")).toBeInTheDocument();
    expect(screen.getByText("wt-1")).toBeInTheDocument();
    expect(screen.getByText("Do the thing")).toBeInTheDocument();
    expect(screen.getByText("abcdef12")).toBeInTheDocument(); // short id
    expect(screen.getByRole("link", { name: /#7/ })).toBeInTheDocument();
    // the seven column headers, all scoped
    const headers = screen.getAllByRole("columnheader");
    expect(headers).toHaveLength(7);
    expect(headers.map((h) => h.getAttribute("scope"))).toEqual(
      Array(7).fill("col"),
    );
    expect(screen.getByText("Last active")).toBeInTheDocument();
    // the horizontal scroll container is keyboard-reachable and named
    const region = screen.getByRole("region", {
      name: "Sessions for octo/cat",
    });
    expect(region).toHaveAttribute("tabindex", "0");
    expect(
      screen.getByRole("table", { name: "Sessions for octo/cat" }),
    ).toBeInTheDocument();
  });

  it("marks a session as running when a running session carries its id", () => {
    const { container } = render(
      <SessionsTable
        label="octo/cat"
        sessions={[
          makeSession({ id: "abcdef12-3456", name: "live" }),
          makeSession({ id: "99999999-0000", name: "idle" }),
        ]}
        runningSessions={[
          makeRunningSession({
            shortId: "11223344",
            claudeSessionId: "abcdef12-3456",
          }),
        ]}
      />,
    );
    const dots = container.querySelectorAll(".run-dot");
    expect(dots).toHaveLength(1);
    expect(dots[0].closest("td")).toHaveTextContent("live");
  });

  it("renders an unnamed session in italic and em-dashes empty fields", () => {
    render(
      <SessionsTable
        label="octo/cat"
        sessions={[
          makeSession({
            name: "",
            branch: "",
            worktree: "",
            firstMessage: "",
            id: "",
            pr: undefined,
          }),
        ]}
      />,
    );
    const name = screen.getByText("(unnamed)");
    expect(name.closest(".s-name")).toHaveClass("unnamed");
    // branch, worktree, first-message, id and PR all collapse to "—"
    expect(screen.getAllByText("—").length).toBeGreaterThanOrEqual(4);
  });

  it("renders a hostile session name as literal text (no HTML injection)", () => {
    render(
      <SessionsTable
        label="octo/cat"
        sessions={[makeSession({ name: '<img src=x onerror="alert(1)">' })]}
      />,
    );
    expect(
      screen.getByText('<img src=x onerror="alert(1)">'),
    ).toBeInTheDocument();
    expect(document.querySelector("img")).toBeNull();
  });
});
