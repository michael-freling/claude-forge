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
  it("shows the error message and retries on click", () => {
    const onRetry = vi.fn();
    render(<ErrorState error={new Error("kaboom")} onRetry={onRetry} />);
    expect(screen.getByText("kaboom")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("falls back to 'Unknown error' when no error is given", () => {
    render(<ErrorState onRetry={() => {}} />);
    expect(screen.getByText("Unknown error")).toBeInTheDocument();
  });
});

describe("NoProjectsState", () => {
  it("explains that no projects were found", () => {
    render(<NoProjectsState />);
    expect(
      screen.getByText("No claude-forge projects found."),
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
