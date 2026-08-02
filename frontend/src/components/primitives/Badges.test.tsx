import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { McpKind, McpScope } from "../../gen/dashboard/v1/dashboard_pb";
import { KindBadge } from "./KindBadge";
import { ScopeBadge } from "./ScopeBadge";
import { WorktreeBadge } from "./WorktreeBadge";

describe("ScopeBadge", () => {
  it("renders the label and class for each scope", () => {
    const { container, rerender } = render(
      <ScopeBadge scope={McpScope.GLOBAL} />,
    );
    expect(container.querySelector(".badge")).toHaveClass("scope-global");
    expect(screen.getByText("global")).toBeInTheDocument();

    rerender(<ScopeBadge scope={McpScope.UNSPECIFIED} />);
    expect(container.querySelector(".badge")).toHaveClass("scope-unknown");
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});

describe("KindBadge", () => {
  it("renders the kind label", () => {
    render(<KindBadge kind={McpKind.STDIO} />);
    expect(screen.getByText("stdio")).toBeInTheDocument();
  });
});

describe("WorktreeBadge", () => {
  it("renders a badge when a worktree is present", () => {
    const { container } = render(<WorktreeBadge worktree="web-dashboard" />);
    expect(container.querySelector(".badge.wt")).toHaveTextContent(
      "web-dashboard",
    );
  });
  it("renders an em dash when absent", () => {
    render(<WorktreeBadge worktree="" />);
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});
