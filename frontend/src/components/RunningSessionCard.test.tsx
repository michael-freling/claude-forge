import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  makePR,
  makeProject,
  makeRunningSession,
  makeServer,
  makeSession,
} from "../test/fixtures";
import { RunningSessionCard } from "./RunningSessionCard";

const project = () =>
  makeProject({
    owner: "octo",
    repo: "cat",
    sessions: [
      makeSession({
        id: "abcdef12-3456-7890",
        name: "wire it up",
        branch: "feat/x",
        worktree: "wt-1",
        pr: makePR({ number: 7, title: "Add a thing" }),
      }),
    ],
  });

describe("RunningSessionCard", () => {
  it("joins the recorded session and shows name, branch, worktree, PR, pills", () => {
    render(
      <RunningSessionCard
        project={project()}
        running={makeRunningSession({
          shortId: "abcdef12",
          mcpServers: [makeServer({ name: "github" })],
        })}
      />,
    );
    expect(
      screen.getByRole("heading", { name: "wire it up" }),
    ).toBeInTheDocument();
    expect(screen.getByText("octo/cat")).toBeInTheDocument();
    expect(screen.getByText("abcdef12")).toBeInTheDocument();
    expect(screen.getByText("feat/x")).toBeInTheDocument();
    expect(screen.getByText("wt-1")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /#7/ })).toBeInTheDocument();
    expect(screen.getByText("github")).toBeInTheDocument();
  });

  it("italicises an unnamed matched session", () => {
    const p = makeProject({
      sessions: [
        makeSession({
          id: "abcdef12-3456",
          name: "",
          branch: "",
          worktree: "",
          pr: undefined,
        }),
      ],
    });
    render(
      <RunningSessionCard
        project={p}
        running={makeRunningSession({ shortId: "abcdef12" })}
      />,
    );
    expect(screen.getByRole("heading", { name: "(unnamed)" })).toHaveClass(
      "unnamed",
    );
  });

  it("falls back to the shortId with a hint when nothing matches", () => {
    render(
      <RunningSessionCard
        project={project()}
        running={makeRunningSession({ shortId: "ffffffff", mcpServers: [] })}
      />,
    );
    expect(
      screen.getByRole("heading", { name: "ffffffff" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("no recorded session matches this id"),
    ).toBeInTheDocument();
    expect(screen.getByText("no session MCP servers")).toBeInTheDocument();
  });

  it("handles a missing shortId", () => {
    render(
      <RunningSessionCard
        project={project()}
        running={makeRunningSession({ shortId: "" })}
      />,
    );
    expect(
      screen.getByRole("heading", { name: "(unknown session)" }),
    ).toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});
