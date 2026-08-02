import type { Project, RunningSession } from "../gen/dashboard/v1/dashboard_pb";
import { findRecordedSession, projectTitle } from "../lib/dashboard";
import { McpPill } from "./McpPill";
import { PRBadge } from "./primitives/PRBadge";
import { WorktreeBadge } from "./primitives/WorktreeBadge";

/**
 * RunningSessionCard renders one live session: its project, runtime shortId,
 * the joined recorded-session metadata (name, branch, worktree, PR — matched
 * by `session.id.startsWith(shortId)`), and its session-scope MCP servers as
 * status pills. Without a match, the shortId stands in for the name.
 */
export function RunningSessionCard({
  project,
  running,
}: {
  project: Project;
  running: RunningSession;
}) {
  const session = findRecordedSession(project, running);
  const named = !!session?.name.trim();

  return (
    <section className="panel run-card">
      <div className="p-head">
        <h2 className={session && !named ? "unnamed" : undefined}>
          {session
            ? named
              ? session.name
              : "(unnamed)"
            : running.shortId || "(unknown session)"}
        </h2>
        <span className="run-proj muted" title={project.id}>
          {projectTitle(project)}
        </span>
        <span className="run-id mono">{running.shortId || "—"}</span>
      </div>
      <div className="p-body">
        {session ? (
          <div className="run-meta">
            {session.branch && (
              <span className="mono clip" title={session.branch}>
                {session.branch}
              </span>
            )}
            {session.worktree && <WorktreeBadge worktree={session.worktree} />}
            {session.pr && <PRBadge pr={session.pr} />}
          </div>
        ) : (
          <div className="run-meta">
            <span className="faint">no recorded session matches this id</span>
          </div>
        )}
        <div className="pills">
          {running.mcpServers.length ? (
            running.mcpServers.map((m, j) => (
              <McpPill key={m.name || j} server={m} />
            ))
          ) : (
            <span className="faint">no session MCP servers</span>
          )}
        </div>
      </div>
    </section>
  );
}
