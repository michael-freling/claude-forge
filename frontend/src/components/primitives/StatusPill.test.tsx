import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { StatusPill } from "./StatusPill";

describe("StatusPill", () => {
  it("renders a green running pill with a dot and label", () => {
    const { container } = render(<StatusPill running />);
    const pill = container.querySelector(".pill");
    expect(pill).toHaveClass("pill-ok");
    expect(pill).toHaveAttribute("title", "running");
    expect(screen.getByText("running")).toBeInTheDocument();
    expect(container.querySelector(".dot")).toBeInTheDocument();
  });

  it("renders a grey stopped pill", () => {
    const { container } = render(<StatusPill running={false} />);
    expect(container.querySelector(".pill")).toHaveClass("pill-off");
    expect(screen.getByText("stopped")).toBeInTheDocument();
  });

  it("uses a custom label and title", () => {
    const { container } = render(
      <StatusPill running label="github" title="Up 2 minutes" />,
    );
    expect(container.querySelector(".pill")).toHaveAttribute(
      "title",
      "Up 2 minutes",
    );
    expect(screen.getByText("github")).toBeInTheDocument();
  });
});
