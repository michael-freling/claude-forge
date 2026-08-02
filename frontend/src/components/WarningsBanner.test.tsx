import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { WarningsBanner } from "./WarningsBanner";

describe("WarningsBanner", () => {
  it("renders warnings as an amber status note, not an alert", () => {
    render(<WarningsBanner warnings={["no token"]} />);
    const status = screen.getByRole("status");
    expect(status).toHaveTextContent("Warning (1)");
    expect(status).toHaveClass("banner-warn");
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByText("no token")).toBeInTheDocument();
  });

  it("pluralises the heading for multiple warnings", () => {
    render(<WarningsBanner warnings={["a", "b"]} />);
    expect(screen.getByRole("status")).toHaveTextContent("Warnings (2)");
  });

  it("renders nothing when there are no warnings", () => {
    const { container } = render(<WarningsBanner warnings={[]} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("can be dismissed, and stays dismissed for the same warnings", () => {
    const { rerender } = render(<WarningsBanner warnings={["x"]} />);
    fireEvent.click(screen.getByRole("button", { name: "Dismiss warnings" }));
    expect(screen.queryByRole("status")).toBeNull();

    rerender(<WarningsBanner warnings={["x"]} />);
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("reappears when the set of warnings changes after a dismissal", () => {
    const { rerender } = render(<WarningsBanner warnings={["x"]} />);
    fireEvent.click(screen.getByRole("button", { name: "Dismiss warnings" }));
    expect(screen.queryByRole("status")).toBeNull();

    rerender(<WarningsBanner warnings={["x", "y"]} />);
    expect(screen.getByRole("status")).toHaveTextContent("Warnings (2)");
    expect(screen.getByText("y")).toBeInTheDocument();
  });
});
