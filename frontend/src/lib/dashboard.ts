import { timestampDate } from "@bufbuild/protobuf/wkt";
import type {
  Project,
  RunningSession,
  Session,
} from "../gen/dashboard/v1/dashboard_pb";

/**
 * projectTitle names a project "owner/repo" when both are known, falling back
 * to the raw project id, then to "(unknown project)".
 */
export function projectTitle(project: Project): string {
  if (project.owner && project.repo) {
    return `${project.owner}/${project.repo}`;
  }
  return project.id || "(unknown project)";
}

/**
 * sessionTime returns a session's last-activity instant in epoch millis: its
 * `lastActive`, falling back to `createdAt`, then 0 (sorts last).
 */
export function sessionTime(session: Session): number {
  for (const ts of [session.lastActive, session.createdAt]) {
    if (ts) {
      const ms = timestampDate(ts).getTime();
      if (!Number.isNaN(ms)) {
        return ms;
      }
    }
  }
  return 0;
}

/** sortSessions returns a copy sorted by last activity, most recent first. */
export function sortSessions(sessions: Session[]): Session[] {
  return [...sessions].sort((a, b) => sessionTime(b) - sessionTime(a));
}

/**
 * findRecordedSession joins a running session to its recorded session: the one
 * in the same project whose full Claude id starts with the runtime `shortId`.
 * An empty shortId never matches.
 */
export function findRecordedSession(
  project: Project,
  shortId: string,
): Session | undefined {
  if (!shortId) {
    return undefined;
  }
  return project.sessions.find((s) => s.id.startsWith(shortId));
}

/** isSessionRunning reports whether one of `running` prefix-matches `session`. */
export function isSessionRunning(
  running: RunningSession[],
  session: Session,
): boolean {
  return running.some(
    (rs) => rs.shortId !== "" && session.id.startsWith(rs.shortId),
  );
}

/**
 * projectActivity is a project's most recent session activity in epoch millis
 * (0 when it has no sessions).
 */
export function projectActivity(project: Project): number {
  return project.sessions.reduce((max, s) => Math.max(max, sessionTime(s)), 0);
}

/**
 * sortProjects returns a copy with running-session projects first, then by
 * most recent session activity, most recent first.
 */
export function sortProjects(projects: Project[]): Project[] {
  return [...projects].sort((a, b) => {
    const runA = a.runningSessions.length > 0 ? 1 : 0;
    const runB = b.runningSessions.length > 0 ? 1 : 0;
    if (runA !== runB) {
      return runB - runA;
    }
    return projectActivity(b) - projectActivity(a);
  });
}

/**
 * matchesFilter reports whether a session matches the free-text filter `query`
 * on its name, branch, or first message (case-insensitive). An empty or
 * whitespace-only query matches everything.
 */
export function matchesFilter(session: Session, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) {
    return true;
  }
  return [session.name, session.branch, session.firstMessage].some((f) =>
    f.toLowerCase().includes(q),
  );
}

/** A running session paired with its owning project. */
export interface RunningEntry {
  project: Project;
  running: RunningSession;
}

/**
 * runningEntries flattens every project's running sessions into (project,
 * running) pairs, in {@link sortProjects} order.
 */
export function runningEntries(projects: Project[]): RunningEntry[] {
  return sortProjects(projects).flatMap((project) =>
    project.runningSessions.map((running) => ({ project, running })),
  );
}
