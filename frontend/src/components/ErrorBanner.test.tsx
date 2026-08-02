import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ErrorBanner } from "./ErrorBanner";

describe("ErrorBanner", () => {
  it("shows the failure message as a red alert", () => {
    render(<ErrorBanner error={new Error("network down")} />);
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("Refresh failed");
    expect(alert).toHaveTextContent("network down");
    expect(alert).toHaveClass("banner-error");
  });

  it("can be dismissed", () => {
    render(<ErrorBanner error={new Error("x")} />);
    fireEvent.click(screen.getByRole("button", { name: "Dismiss error" }));
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
