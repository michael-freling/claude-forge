import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { makeServer } from "../test/fixtures";
import { McpPill } from "./McpPill";

describe("McpPill", () => {
  it("renders a running server as a green pill labelled by name", () => {
    const { container } = render(
      <McpPill server={makeServer({ name: "github", running: true })} />,
    );
    expect(container.querySelector(".pill")).toHaveClass("pill-ok");
    expect(screen.getByText("github")).toBeInTheDocument();
  });

  it("labels an unnamed stopped server and shows 'not running'", () => {
    const { container } = render(
      <McpPill
        server={makeServer({ name: "", status: "", running: false })}
      />,
    );
    expect(container.querySelector(".pill")).toHaveClass("pill-off");
    expect(screen.getByText("(unnamed)")).toBeInTheDocument();
    expect(container.querySelector(".pill")).toHaveAttribute(
      "title",
      "not running",
    );
  });
});
