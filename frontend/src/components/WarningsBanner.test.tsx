import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { WarningsBanner } from "./WarningsBanner";

describe("WarningsBanner", () => {
  it("renders a singular heading and each warning as an alert", () => {
    render(<WarningsBanner warnings={["no token"]} />);
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("Warning (1)");
    expect(screen.getByText("no token")).toBeInTheDocument();
  });

  it("pluralises the heading for multiple warnings", () => {
    render(<WarningsBanner warnings={["a", "b"]} />);
    expect(screen.getByRole("alert")).toHaveTextContent("Warnings (2)");
  });

  it("renders nothing when there are no warnings", () => {
    const { container } = render(<WarningsBanner warnings={[]} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("can be dismissed", () => {
    render(<WarningsBanner warnings={["x"]} />);
    fireEvent.click(screen.getByRole("button", { name: "Dismiss warnings" }));
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
