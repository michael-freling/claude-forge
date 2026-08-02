import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ErrorState } from "./ErrorState";
import { Footer } from "./Footer";
import { LoadingState } from "./LoadingState";
import { NoProjectsState } from "./NoProjectsState";

describe("LoadingState", () => {
  it("renders the loading spinner and label", () => {
    const { container } = render(<LoadingState />);
    expect(screen.getByText("Loading dashboard…")).toBeInTheDocument();
    expect(container.querySelector(".spinner")).toBeInTheDocument();
  });
});

describe("ErrorState", () => {
  it("leads with the likely cause, keeps the raw error as detail, retries", () => {
    const onRetry = vi.fn();
    render(<ErrorState error={new Error("kaboom")} onRetry={onRetry} />);
    expect(
      screen.getByRole("heading", { name: /Can’t reach claude-forge/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/claude-forge dashboard/, { selector: "code" }),
    ).toBeInTheDocument();
    expect(screen.getByText("kaboom")).toHaveClass("err-detail");
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("falls back to 'Unknown error' when no error is given", () => {
    render(<ErrorState onRetry={() => {}} />);
    expect(screen.getByText("Unknown error")).toBeInTheDocument();
  });
});

describe("NoProjectsState", () => {
  it("explains that no projects were found and offers the start command", () => {
    render(<NoProjectsState />);
    expect(
      screen.getByText("No claude-forge projects found."),
    ).toBeInTheDocument();
    expect(
      screen.getByText("claude-forge start <name>", { selector: "code" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Copy command/ }),
    ).toBeInTheDocument();
  });
});

describe("Footer", () => {
  it("names the backing RPC", () => {
    render(<Footer />);
    expect(
      screen.getByText(/dashboard\.v1\.DashboardService\/GetDashboard/),
    ).toBeInTheDocument();
  });
});
