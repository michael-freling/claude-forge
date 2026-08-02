import { render, screen } from "@testing-library/react";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { makeProject, makeSession } from "../test/fixtures";
import { ProjectCard } from "./ProjectCard";

const at = (iso: string) => timestampFromDate(new Date(iso));

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
    expect(
      screen.getByRole("heading", { name: "octo/cat" }),
    ).toBeInTheDocument();
    expect(screen.getByText("/home/user/cat")).toBeInTheDocument();
    expect(screen.getByText("1 session")).toBeInTheDocument();
    expect(screen.queryByText("(directory removed)")).toBeNull();
  });

  it("falls back to the id with a '(directory removed)' hint, hides the dir, pluralises", () => {
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
    expect(screen.getByText("(directory removed)")).toBeInTheDocument();
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

  it("sorts its own sessions by last activity when none are passed", () => {
    render(
      <ProjectCard
        project={makeProject({
          sessions: [
            makeSession({
              id: "1111",
              name: "older",
              lastActive: at("2026-08-01T00:00:00Z"),
            }),
            makeSession({
              id: "2222",
              name: "newer",
              lastActive: at("2026-08-02T00:00:00Z"),
            }),
          ],
        })}
      />,
    );
    const names = screen
      .getAllByRole("row")
      .slice(1) // skip the header row
      .map((r) => r.querySelector(".s-name")?.textContent);
    expect(names).toEqual(["newer", "older"]);
  });

  it("renders the sessions passed by the caller as-is", () => {
    render(
      <ProjectCard
        project={makeProject({ sessions: [makeSession(), makeSession()] })}
        sessions={[makeSession({ name: "only me" })]}
      />,
    );
    expect(screen.getByText("only me")).toBeInTheDocument();
    expect(screen.getByText("1 session")).toBeInTheDocument();
  });
});
