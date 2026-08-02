import { render, screen } from "@testing-library/react";
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
  it("combines created + last-active into the Created tooltip", () => {
    const { container } = inTable(<SessionRow session={makeSession()} />);
    const time = container.querySelector("time");
    expect(time?.getAttribute("title")).toContain("Created ");
    expect(time?.getAttribute("title")).toContain("Last active ");
  });

  it("shows only 'Created' when there is no last-active time", () => {
    const { container } = inTable(
      <SessionRow session={makeSession({ lastActive: undefined })} />,
    );
    const title = container.querySelector("time")?.getAttribute("title") ?? "";
    expect(title).toContain("Created ");
    expect(title).not.toContain("Last active");
  });

  it("renders an em dash for a missing created time", () => {
    inTable(<SessionRow session={makeSession({ createdAt: undefined })} />);
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
  });
});
