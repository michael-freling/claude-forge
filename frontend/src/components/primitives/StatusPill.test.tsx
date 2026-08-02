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
    // the label already is the state — no hidden duplicate
    expect(container.querySelector(".sr-only")).toBeNull();
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

  it("appends visually-hidden state text when the label hides the state", () => {
    const { container } = render(
      <StatusPill running={false} label="github" />,
    );
    const hidden = container.querySelector(".sr-only");
    expect(hidden).toHaveTextContent(": stopped");

    const { container: up } = render(<StatusPill running label="github" />);
    expect(up.querySelector(".sr-only")).toHaveTextContent(": running");
  });
});
