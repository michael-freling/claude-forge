import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { makePR, makeSession } from "../test/fixtures";
import { SessionsTable } from "./SessionsTable";

describe("SessionsTable", () => {
  it("shows an empty note when there are no sessions", () => {
    render(<SessionsTable sessions={[]} />);
    expect(screen.getByText("No sessions.")).toBeInTheDocument();
  });

  it("renders a populated row with all columns", () => {
    render(
      <SessionsTable
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
    // the seven column headers
    expect(screen.getAllByRole("columnheader")).toHaveLength(7);
  });

  it("renders an unnamed session in italic and em-dashes empty fields", () => {
    render(
      <SessionsTable
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
        sessions={[
          makeSession({ name: '<img src=x onerror="alert(1)">' }),
        ]}
      />,
    );
    expect(
      screen.getByText('<img src=x onerror="alert(1)">'),
    ).toBeInTheDocument();
    expect(document.querySelector("img")).toBeNull();
  });
});
