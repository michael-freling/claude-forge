import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactElement } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { Header } from "./Header";

function renderAt(route: string, ui: ReactElement) {
  return render(<MemoryRouter initialEntries={[route]}>{ui}</MemoryRouter>);
}

describe("Header", () => {
  it("shows the generated time and an active Refresh button", () => {
    const onRefresh = vi.fn();
    renderAt(
      "/",
      <Header
        generatedAt={new Date(Date.now() - 5000)}
        loading={false}
        onRefresh={onRefresh}
      />,
    );
    expect(screen.getByText(/generated/)).toBeInTheDocument();
    const btn = screen.getByRole("button", { name: "Refresh" });
    expect(btn).toHaveAttribute("aria-disabled", "false");
    fireEvent.click(btn);
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });

  it("marks the button busy (not disabled) and ignores clicks while loading", () => {
    const onRefresh = vi.fn();
    renderAt("/", <Header loading onRefresh={onRefresh} />);
    const btn = screen.getByRole("button", { name: "Refreshing…" });
    // aria-busy/aria-disabled instead of `disabled`: keyboard focus survives
    expect(btn).not.toBeDisabled();
    expect(btn).toHaveAttribute("aria-busy", "true");
    expect(btn).toHaveAttribute("aria-disabled", "true");
    fireEvent.click(btn);
    expect(onRefresh).not.toHaveBeenCalled();
  });

  it("omits the generated time when there is none", () => {
    renderAt("/", <Header loading={false} onRefresh={() => {}} />);
    expect(screen.queryByText(/generated/)).toBeNull();
  });

  it("renders the nav tabs and marks Running active on /", () => {
    renderAt("/", <Header loading={false} onRefresh={() => {}} />);
    const running = screen.getByRole("link", { name: "Running" });
    expect(running).toHaveClass("active");
    expect(running).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: "Sessions" })).not.toHaveClass(
      "active",
    );
    expect(screen.getByRole("link", { name: "Servers" })).not.toHaveClass(
      "active",
    );
  });

  it("marks Sessions active on /sessions and not Running", () => {
    renderAt("/sessions", <Header loading={false} onRefresh={() => {}} />);
    expect(screen.getByRole("link", { name: "Sessions" })).toHaveClass(
      "active",
    );
    expect(screen.getByRole("link", { name: "Running" })).not.toHaveClass(
      "active",
    );
  });

  it("marks Servers active on /servers", () => {
    renderAt("/servers", <Header loading={false} onRefresh={() => {}} />);
    expect(screen.getByRole("link", { name: "Servers" })).toHaveClass(
      "active",
    );
  });
});
