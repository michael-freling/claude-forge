import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import {
  makePR,
  makeProject,
  makeRunningSession,
  makeServer,
  makeSession,
} from "../test/fixtures";
import { RunningSessionCard } from "./RunningSessionCard";

const renderCard = (node: ReactNode) =>
  render(<MemoryRouter>{node}</MemoryRouter>);

const project = () =>
  makeProject({
    id: "-home-p",
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
    renderCard(
      <RunningSessionCard
        project={project()}
        running={makeRunningSession({
          shortId: "abcdef12",
          claudeSessionId: "abcdef12-3456-7890",
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

  it("links onward to this session's server section on /servers", () => {
    renderCard(
      <RunningSessionCard
        project={project()}
        running={makeRunningSession({
          shortId: "abcdef12",
          claudeSessionId: "abcdef12-3456-7890",
          mcpServers: [makeServer({ name: "github" })],
        })}
      />,
    );
    const link = screen.getByRole("link", {
      name: "1 MCP server for session wire it up",
    });
    expect(link).toHaveAttribute("href", "/servers#rs--home-p-abcdef12");
    expect(link).toHaveTextContent("servers →");
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
    renderCard(
      <RunningSessionCard
        project={p}
        running={makeRunningSession({
          shortId: "abcdef12",
          claudeSessionId: "abcdef12-3456",
        })}
      />,
    );
    expect(screen.getByRole("heading", { name: "(unnamed)" })).toHaveClass(
      "unnamed",
    );
    // the a11y label falls back to the same placeholder
    expect(
      screen.getByRole("link", { name: "0 MCP servers for session (unnamed)" }),
    ).toBeInTheDocument();
  });

  it("falls back to the shortId with a hint when nothing matches", () => {
    renderCard(
      <RunningSessionCard
        project={project()}
        running={makeRunningSession({
          shortId: "ffffffff",
          claudeSessionId: "",
          mcpServers: [],
        })}
      />,
    );
    expect(
      screen.getByRole("heading", { name: "ffffffff" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("no recorded session matches this id"),
    ).toBeInTheDocument();
    expect(screen.getByText("no session MCP servers")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "0 MCP servers for session ffffffff" }),
    ).toHaveAttribute("href", "/servers#rs--home-p-ffffffff");
  });

  it("handles a missing shortId", () => {
    renderCard(
      <RunningSessionCard
        project={project()}
        running={makeRunningSession({ shortId: "", claudeSessionId: "" })}
      />,
    );
    expect(
      screen.getByRole("heading", { name: "(unknown session)" }),
    ).toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});
