import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RelativeTime } from "./RelativeTime";

afterEach(() => vi.useRealTimers());

describe("RelativeTime", () => {
  it("renders the relative label and an absolute-timestamp tooltip", () => {
    const value = new Date(Date.now() - 5000);
    render(<RelativeTime value={value} />);
    const el = screen.getByText("5s ago");
    expect(el.tagName).toBe("TIME");
    expect(el).toHaveAttribute("dateTime", value.toISOString());
    expect(el).toHaveAttribute("title", value.toLocaleString());
  });

  it("uses a title override when provided", () => {
    render(
      <RelativeTime value={new Date(Date.now() - 5000)} title="Created X" />,
    );
    expect(screen.getByText("5s ago")).toHaveAttribute("title", "Created X");
  });

  it("renders the empty fallback for undefined and invalid values", () => {
    const { rerender } = render(<RelativeTime value={undefined} />);
    expect(screen.getByText("—")).toBeInTheDocument();

    rerender(<RelativeTime value={new Date("bad")} empty="n/a" />);
    expect(screen.getByText("n/a")).toBeInTheDocument();
  });

  it("refreshes its label on the 30s timer", () => {
    vi.useFakeTimers();
    const base = new Date("2026-08-02T12:00:00Z");
    vi.setSystemTime(base);
    const value = new Date(base.getTime() - 40_000); // 40s ago

    render(<RelativeTime value={value} />);
    expect(screen.getByText("40s ago")).toBeInTheDocument();

    // Advancing the fake clock 30s fires the interval; now 70s have elapsed.
    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    expect(screen.getByText("1m ago")).toBeInTheDocument();
  });
});
