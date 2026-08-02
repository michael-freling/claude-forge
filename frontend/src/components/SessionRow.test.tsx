import { render, screen } from "@testing-library/react";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { makeSession } from "../test/fixtures";
import { SessionRow } from "./SessionRow";

function inTable(node: ReactNode) {
  return render(
    <table>
      <tbody>{node}</tbody>
    </table>,
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

  it("shows a running dot (with hidden text) only when running", () => {
    const { container } = inTable(
      <SessionRow session={makeSession()} running />,
    );
    expect(container.querySelector(".run-dot")).toBeInTheDocument();
    expect(screen.getByText("running:")).toHaveClass("sr-only");

    const { container: idle } = inTable(
      <SessionRow session={makeSession()} />,
    );
    expect(idle.querySelector(".run-dot")).toBeNull();
  });
});
