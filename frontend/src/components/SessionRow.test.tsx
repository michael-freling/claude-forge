import { render, screen, within } from "@testing-library/react";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { makeRunningSession, makeServer, makeSession } from "../test/fixtures";
import { SessionRow } from "./SessionRow";

function inTable(node: ReactNode) {
  return render(
    <MemoryRouter>
      <table>
        <tbody>{node}</tbody>
      </table>
    </MemoryRouter>,
  );
}

describe("SessionRow", () => {
  it("shows the last-active time with both timestamps in the tooltip", () => {
    const { container } = inTable(<SessionRow session={makeSession()} />);
    const time = container.querySelector("time");
    // fixture: created 12:00Z, last active 12:30Z — last active is displayed
    expect(time?.getAttribute("dateTime")).toBe(
      new Date("2026-08-02T12:30:00Z").toISOString(),
    );
    expect(time?.getAttribute("title")).toContain("Created ");
    expect(time?.getAttribute("title")).toContain("Last active ");
  });

  it("falls back to the created time when there is no last-active", () => {
    const { container } = inTable(
      <SessionRow session={makeSession({ lastActive: undefined })} />,
    );
    const time = container.querySelector("time");
    expect(time?.getAttribute("dateTime")).toBe(
      new Date("2026-08-02T12:00:00Z").toISOString(),
    );
    const title = time?.getAttribute("title") ?? "";
    expect(title).toContain("Created ");
    expect(title).not.toContain("Last active");
  });

  it("tolerates a last-active without created (no tooltip override)", () => {
    const { container } = inTable(
      <SessionRow
        session={makeSession({
          createdAt: undefined,
          lastActive: timestampFromDate(new Date("2026-08-02T12:30:00Z")),
        })}
      />,
    );
    const time = container.querySelector("time");
    expect(time?.getAttribute("title")).not.toContain("Created");
  });

  it("renders an em dash when there are no timestamps at all", () => {
    inTable(
      <SessionRow
        session={makeSession({ createdAt: undefined, lastActive: undefined })}
      />,
    );
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
  });

  it("anchors the row by session id (none when the id is empty)", () => {
    const { container } = inTable(
      <SessionRow session={makeSession({ id: "abcd-1" })} />,
    );
    expect(container.querySelector("tr")?.id).toBe("sess-abcd-1");

    const { container: bare } = inTable(
      <SessionRow session={makeSession({ id: "" })} />,
    );
    expect(bare.querySelector("tr")?.hasAttribute("id")).toBe(false);
  });

  it("shows a running dot (with hidden text) only when running", () => {
    const { container } = inTable(
      <SessionRow session={makeSession()} running={makeRunningSession()} />,
    );
    expect(container.querySelector(".run-dot")).toBeInTheDocument();
    expect(screen.getByText("running:")).toHaveClass("sr-only");

    const { container: idle } = inTable(
      <SessionRow session={makeSession()} />,
    );
    expect(idle.querySelector(".run-dot")).toBeNull();
    expect(within(idle).queryByRole("link", { name: /MCP server/ })).toBeNull();
  });

  it("links a running row to its project-scoped server section", () => {
    inTable(
      <SessionRow
        session={makeSession({ name: "wire it" })}
        projectId="-home-p"
        running={makeRunningSession({
          shortId: "abcdef12",
          mcpServers: [makeServer(), makeServer({ name: "extra" })],
        })}
      />,
    );
    const link = screen.getByRole("link", {
      name: "2 MCP servers for session wire it",
    });
    expect(link).toHaveAttribute("href", "/servers#rs--home-p-abcdef12");
    expect(link).toHaveTextContent("2 servers →");
  });

  it("uses singular server wording and the (unnamed) fallback", () => {
    inTable(
      <SessionRow
        session={makeSession({ name: "" })}
        projectId="p"
        running={makeRunningSession({
          shortId: "feedbeef",
          mcpServers: [makeServer()],
        })}
      />,
    );
    const link = screen.getByRole("link", {
      name: "1 MCP server for session (unnamed)",
    });
    expect(link).toHaveAttribute("href", "/servers#rs-p-feedbeef");
    expect(link).toHaveTextContent("1 server →");
  });
});
