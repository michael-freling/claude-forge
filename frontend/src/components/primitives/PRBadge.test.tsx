import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PullRequestState } from "../../gen/dashboard/v1/dashboard_pb";
import { makePR } from "../../test/fixtures";
import { PRBadge } from "./PRBadge";

describe("PRBadge", () => {
  it("renders a safe https url as an external link, coloured open", () => {
    render(<PRBadge pr={makePR({ number: 42, title: "hello" })} />);
    const link = screen.getByRole("link");
    expect(link).toHaveAttribute("href", "https://github.com/o/r/pull/42");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
    expect(link).toHaveClass("pr", "pr-open");
    expect(link).toHaveTextContent("#42");
    expect(link).toHaveTextContent("hello");
  });

  it("colours merged and closed states and labels them in the title", () => {
    const { rerender } = render(
      <PRBadge pr={makePR({ state: PullRequestState.MERGED })} />,
    );
    expect(screen.getByRole("link")).toHaveClass("pr-merged");
    expect(screen.getByRole("link").getAttribute("title")).toContain("merged");

    rerender(<PRBadge pr={makePR({ state: PullRequestState.CLOSED })} />);
    expect(screen.getByRole("link")).toHaveClass("pr-closed");
    expect(screen.getByRole("link").getAttribute("title")).toContain("closed");
  });

  it("shows a draft tag when the PR is a draft", () => {
    render(<PRBadge pr={makePR({ draft: true })} />);
    expect(screen.getByText("draft")).toBeInTheDocument();
    expect(screen.getByRole("link").getAttribute("title")).toContain("draft");
  });

  it("renders an inert span (no link) for a javascript: url", () => {
    render(
      <PRBadge pr={makePR({ number: 66, url: "javascript:alert(1)" })} />,
    );
    expect(screen.queryByRole("link")).toBeNull();
    const chip = screen.getByText("#66").closest(".pr");
    expect(chip?.tagName).toBe("SPAN");
    expect(chip).not.toHaveAttribute("href");
  });

  it("renders an inert span for a data: url", () => {
    render(<PRBadge pr={makePR({ url: "data:text/html,x" })} />);
    expect(screen.queryByRole("link")).toBeNull();
  });

  it("omits the title suffix when the PR has no title", () => {
    render(<PRBadge pr={makePR({ number: 9, title: "" })} />);
    const link = screen.getByRole("link");
    expect(link.getAttribute("title")).toBe("#9 · open");
  });

  it("renders an em dash when there is no PR", () => {
    render(<PRBadge />);
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByRole("link")).toBeNull();
  });
});
