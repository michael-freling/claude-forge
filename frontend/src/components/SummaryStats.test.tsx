import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SummaryStats } from "./SummaryStats";

describe("SummaryStats", () => {
  it("uses singular labels for a count of one", () => {
    render(
      <SummaryStats
        projectCount={1}
        sessionCount={1}
        runningCount={1}
        serverCount={1}
      />,
    );
    expect(screen.getByText("project")).toBeInTheDocument();
    expect(screen.getByText("session")).toBeInTheDocument();
    expect(screen.getByText("running")).toBeInTheDocument();
    expect(screen.getByText("global server")).toBeInTheDocument();
  });

  it("uses plural labels otherwise", () => {
    render(
      <SummaryStats
        projectCount={0}
        sessionCount={3}
        runningCount={2}
        serverCount={0}
      />,
    );
    expect(screen.getByText("projects")).toBeInTheDocument();
    expect(screen.getByText("sessions")).toBeInTheDocument();
    expect(screen.getByText("global servers")).toBeInTheDocument();
  });
});
