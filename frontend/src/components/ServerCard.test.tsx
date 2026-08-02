import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { McpKind, McpScope } from "../gen/dashboard/v1/dashboard_pb";
import { makeServer } from "../test/fixtures";
import { ServerCard } from "./ServerCard";

describe("ServerCard", () => {
  it("renders a running server with scope, kind, status and image", () => {
    render(
      <ServerCard
        server={makeServer({
          name: "kubernetes",
          scope: McpScope.GLOBAL,
          kind: McpKind.CONTAINER,
          image: "ghcr.io/x/kube:latest",
          status: "Up 3 minutes",
          running: true,
        })}
      />,
    );
    expect(screen.getByText("kubernetes")).toBeInTheDocument();
    expect(screen.getByText("global")).toBeInTheDocument();
    expect(screen.getByText("container")).toBeInTheDocument();
    expect(screen.getByText("running")).toBeInTheDocument();
    expect(screen.getByText("Up 3 minutes")).toBeInTheDocument();
    expect(screen.getByText("ghcr.io/x/kube:latest")).toBeInTheDocument();
  });

  it("derives status/label for a stopped, unnamed, imageless server", () => {
    const { container } = render(
      <ServerCard
        server={makeServer({
          name: "",
          image: "",
          container: "",
          status: "",
          running: false,
        })}
      />,
    );
    expect(screen.getByText("(unnamed)")).toBeInTheDocument();
    expect(screen.getByText("stopped")).toBeInTheDocument();
    expect(screen.getAllByText("not running").length).toBeGreaterThan(0);
    expect(container.querySelector(".srv-img")).toBeNull();
  });

  it("falls back to the container name and 'running' status", () => {
    const { container } = render(
      <ServerCard
        server={makeServer({
          image: "",
          container: "forge-mcp-abc",
          status: "",
          running: true,
        })}
      />,
    );
    const img = container.querySelector(".srv-img");
    expect(img).toHaveTextContent("forge-mcp-abc");
    expect(img).toHaveAttribute("title", " · forge-mcp-abc");
    expect(screen.getAllByText("running").length).toBeGreaterThan(0);
  });
});
