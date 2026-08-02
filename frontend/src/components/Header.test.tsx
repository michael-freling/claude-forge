import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Header } from "./Header";

describe("Header", () => {
  it("shows the generated time and an enabled Refresh button", () => {
    const onRefresh = vi.fn();
    render(
      <Header
        generatedAt={new Date(Date.now() - 5000)}
        loading={false}
        onRefresh={onRefresh}
      />,
    );
    expect(screen.getByText(/generated/)).toBeInTheDocument();
    const btn = screen.getByRole("button", { name: "Refresh" });
    expect(btn).toBeEnabled();
    fireEvent.click(btn);
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });

  it("disables the button and relabels it while loading", () => {
    render(<Header loading onRefresh={() => {}} />);
    const btn = screen.getByRole("button", { name: "Refreshing…" });
    expect(btn).toBeDisabled();
  });

  it("omits the generated time when there is none", () => {
    render(<Header loading={false} onRefresh={() => {}} />);
    expect(screen.queryByText(/generated/)).toBeNull();
  });
});
