import { create } from "@bufbuild/protobuf";
import { TimestampSchema, timestampFromDate } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import {
  makeProject,
  makeRunningSession,
  makeSession,
} from "../test/fixtures";
import {
  findRecordedSession,
  isSessionRunning,
  matchesFilter,
  projectActivity,
  projectTitle,
  runningEntries,
  sessionTime,
  sortProjects,
  sortSessions,
} from "./dashboard";

const at = (iso: string) => timestampFromDate(new Date(iso));

describe("projectTitle", () => {
  it("prefers owner/repo, then id, then a placeholder", () => {
    expect(projectTitle(makeProject({ owner: "octo", repo: "cat" }))).toBe(
      "octo/cat",
    );
    expect(
      projectTitle(makeProject({ owner: "", repo: "", id: "-home-x" })),
    ).toBe("-home-x");
    expect(projectTitle(makeProject({ owner: "octo", repo: "", id: "" }))).toBe(
      "(unknown project)",
    );
  });
});

describe("sessionTime", () => {
  it("prefers lastActive, falls back to createdAt, then 0", () => {
    const s = makeSession({
      createdAt: at("2026-08-01T00:00:00Z"),
      lastActive: at("2026-08-02T00:00:00Z"),
    });
    expect(sessionTime(s)).toBe(Date.parse("2026-08-02T00:00:00Z"));
    expect(sessionTime(makeSession({ lastActive: undefined }))).toBe(
      Date.parse("2026-08-02T12:00:00Z"),
    );
    expect(
      sessionTime(makeSession({ createdAt: undefined, lastActive: undefined })),
    ).toBe(0);
  });

  it("skips a timestamp that does not convert to a valid date", () => {
    const bogus = create(TimestampSchema, { seconds: 10n ** 18n });
    const s = makeSession({
      lastActive: bogus,
      createdAt: at("2026-08-01T00:00:00Z"),
    });
    expect(sessionTime(s)).toBe(Date.parse("2026-08-01T00:00:00Z"));
  });
});

describe("sortSessions", () => {
  it("sorts by last activity, most recent first, without mutating", () => {
    const old = makeSession({ id: "old", lastActive: at("2026-08-01T00:00:00Z") });
    const fresh = makeSession({
      id: "fresh",
      lastActive: at("2026-08-02T00:00:00Z"),
    });
    const input = [old, fresh];
    expect(sortSessions(input).map((s) => s.id)).toEqual(["fresh", "old"]);
    expect(input.map((s) => s.id)).toEqual(["old", "fresh"]);
  });
});

describe("findRecordedSession", () => {
  const project = makeProject({
    sessions: [makeSession({ id: "abcdef12-3456", name: "hit" })],
  });

  it("joins by the backend-recovered Claude session id", () => {
    const rs = makeRunningSession({
      shortId: "11223344",
      claudeSessionId: "abcdef12-3456",
    });
    expect(findRecordedSession(project, rs)?.name).toBe("hit");
  });

  it("returns undefined without a claudeSessionId or on a non-matching id", () => {
    expect(
      findRecordedSession(project, makeRunningSession({ shortId: "abcdef12" })),
    ).toBeUndefined();
    expect(
      findRecordedSession(
        project,
        makeRunningSession({ claudeSessionId: "ffffffff-0000" }),
      ),
    ).toBeUndefined();
  });
});

describe("isSessionRunning", () => {
  const session = makeSession({ id: "abcdef12-3456" });

  it("matches on the Claude session id", () => {
    expect(
      isSessionRunning(
        [makeRunningSession({ claudeSessionId: "abcdef12-3456" })],
        session,
      ),
    ).toBe(true);
  });

  it("never matches an empty or different claudeSessionId", () => {
    expect(
      isSessionRunning([makeRunningSession({ shortId: "abcdef12" })], session),
    ).toBe(false);
    expect(
      isSessionRunning(
        [makeRunningSession({ claudeSessionId: "other-id" })],
        session,
      ),
    ).toBe(false);
    expect(isSessionRunning([], session)).toBe(false);
  });
});

describe("projectActivity / sortProjects", () => {
  const idle = makeProject({
    id: "idle",
    sessions: [makeSession({ lastActive: at("2026-08-01T00:00:00Z") })],
    runningSessions: [],
  });
  const fresh = makeProject({
    id: "fresh",
    sessions: [makeSession({ lastActive: at("2026-08-02T00:00:00Z") })],
    runningSessions: [],
  });
  const live = makeProject({
    id: "live",
    sessions: [makeSession({ lastActive: at("2026-07-01T00:00:00Z") })],
    runningSessions: [makeRunningSession()],
  });

  it("computes a project's most recent activity", () => {
    expect(projectActivity(fresh)).toBe(Date.parse("2026-08-02T00:00:00Z"));
    expect(projectActivity(makeProject({ sessions: [] }))).toBe(0);
  });

  it("puts running projects first, then most recent activity", () => {
    expect(sortProjects([idle, fresh, live]).map((p) => p.id)).toEqual([
      "live",
      "fresh",
      "idle",
    ]);
  });
});

describe("matchesFilter", () => {
  const s = makeSession({
    name: "Wire Dashboard",
    branch: "feat/spa",
    firstMessage: "Build the UI",
  });

  it("matches name, branch, and first message case-insensitively", () => {
    expect(matchesFilter(s, "dash")).toBe(true);
    expect(matchesFilter(s, "FEAT/")).toBe(true);
    expect(matchesFilter(s, "build the")).toBe(true);
    expect(matchesFilter(s, "nomatch")).toBe(false);
  });

  it("matches everything on an empty or whitespace query", () => {
    expect(matchesFilter(s, "")).toBe(true);
    expect(matchesFilter(s, "   ")).toBe(true);
  });
});

describe("runningEntries", () => {
  it("flattens running sessions across projects in sorted order", () => {
    const a = makeProject({
      id: "a",
      sessions: [],
      runningSessions: [makeRunningSession({ shortId: "aaaa1111" })],
    });
    const b = makeProject({
      id: "b",
      sessions: [],
      runningSessions: [
        makeRunningSession({ shortId: "bbbb1111" }),
        makeRunningSession({ shortId: "bbbb2222" }),
      ],
    });
    const none = makeProject({ id: "none", sessions: [], runningSessions: [] });
    const entries = runningEntries([none, a, b]);
    expect(entries.map((e) => e.running.shortId)).toEqual([
      "aaaa1111",
      "bbbb1111",
      "bbbb2222",
    ]);
    expect(entries.every((e) => e.project.runningSessions.length > 0)).toBe(
      true,
    );
  });
});
