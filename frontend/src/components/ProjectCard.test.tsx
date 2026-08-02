import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { makeProject, makeSession } from "../test/fixtures";
import { ProjectCard } from "./ProjectCard";

describe("ProjectCard", () => {
  it("titles the card owner/repo and shows the resolved dir + session count", () => {
    render(
      <ProjectCard
        project={makeProject({
          owner: "octo",
          repo: "cat",
          resolved: true,
          dir: "/home/user/cat",
          sessions: [makeSession()],
        })}
      />,
    );
    expect(screen.getByRole("heading", { name: "octo/cat" })).toBeInTheDocument();
    expect(screen.getByText("/home/user/cat")).toBeInTheDocument();
    expect(screen.getByText("1 session")).toBeInTheDocument();
  });

  it("falls back to the id, hides the dir when unresolved, pluralises sessions", () => {
    render(
      <ProjectCard
        project={makeProject({
          owner: "",
          repo: "",
          id: "-home-user-cat",
          resolved: false,
          dir: "/home/user/cat",
          sessions: [makeSession(), makeSession()],
        })}
      />,
    );
    expect(
      screen.getByRole("heading", { name: "-home-user-cat" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("/home/user/cat")).toBeNull();
    expect(screen.getByText("2 sessions")).toBeInTheDocument();
  });

  it("shows '(unknown project)' when there is no owner/repo or id", () => {
    render(
      <ProjectCard
        project={makeProject({ owner: "", repo: "", id: "", sessions: [] })}
      />,
    );
    expect(
      screen.getByRole("heading", { name: "(unknown project)" }),
    ).toBeInTheDocument();
    expect(screen.getByText("0 sessions")).toBeInTheDocument();
  });
});
