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
 * findRecordedSession joins a running session to its recorded session by the
 * Claude session id the backend recovered from the agent container's command
 * line. The container shortId and the Claude session id are independent random
 * values, so an id-less running session (started with --continue, or whose
 * container could not be inspected) simply has no join.
 */
export function findRecordedSession(
  project: Project,
  running: RunningSession,
): Session | undefined {
  if (!running.claudeSessionId) {
    return undefined;
  }
  return project.sessions.find((s) => s.id === running.claudeSessionId);
}

/**
 * findRunningForSession is the reverse join of {@link findRecordedSession}: it
 * returns the running session whose backend-recovered Claude session id
 * matches `session`, if any. An id-less recorded session never matches (an
 * empty claudeSessionId means the backend could not recover one).
 */
export function findRunningForSession(
  running: RunningSession[],
  session: Session,
): RunningSession | undefined {
  return running.find(
    (rs) => rs.claudeSessionId !== "" && rs.claudeSessionId === session.id,
  );
}

/** isSessionRunning reports whether `session` is one of the `running` set. */
export function isSessionRunning(
  running: RunningSession[],
  session: Session,
): boolean {
  return findRunningForSession(running, session) !== undefined;
}

/**
 * runningAnchor is the /servers page anchor id for one running session's
 * server section. It is project-scoped: container shortIds are only unique
 * per Docker host and could collide across projects.
 */
export function runningAnchor(
  projectId: string,
  running: RunningSession,
): string {
  return `rs-${projectId}-${running.shortId}`;
}

/** sessionAnchor is the /sessions page anchor id for one recorded session. */
export function sessionAnchor(sessionId: string): string {
  return `sess-${sessionId}`;
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
