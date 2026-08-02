import { Link } from "react-router-dom";
import { GlobalPanel } from "../components/GlobalPanel";
import { ServerCard } from "../components/ServerCard";
import { EmptyNote } from "../components/primitives/EmptyNote";
import type { Dashboard } from "../gen/dashboard/v1/dashboard_pb";
import {
  findRecordedSession,
  projectTitle,
  runningAnchor,
  runningEntries,
  sessionAnchor,
} from "../lib/dashboard";

/**
 * ServersPage shows every MCP server: the global (host-wide) panel, then one
 * section per running session with its session-scope servers. Each section is
 * attributed to its session — project plus the joined recorded-session name,
 * which links back to the session's row on /sessions (falling back to the
 * bare container shortId when nothing joins) — and carries a project-scoped
 * anchor (`#rs-<projectId>-<shortId>`) so session rows and running cards can
 * deep-link here.
 */
export function ServersPage({ data }: { data: Dashboard }) {
  const entries = runningEntries(data.projects);
  return (
    <>
      <GlobalPanel servers={data.global?.servers ?? []} />
      <section className="panel">
        <div className="p-head">
          <h2>Session MCP servers</h2>
          <span className="count">
            {entries.length} running{" "}
            {entries.length === 1 ? "session" : "sessions"}
          </span>
        </div>
        <div className="p-body">
          {entries.length ? (
            entries.map(({ project, running }, i) => {
              const session = findRecordedSession(project, running);
              const named = !!session?.name.trim();
              return (
                <div
                  className="sess-srv"
                  id={runningAnchor(project.id, running)}
                  key={`${project.id || i}:${running.shortId || i}`}
                >
                  <h3>
                    {projectTitle(project)} ·{" "}
                    {session ? (
                      <Link
                        className={"sess-link" + (named ? "" : " unnamed")}
                        to={`/sessions#${sessionAnchor(session.id)}`}
                      >
                        {named ? session.name : "(unnamed)"}
                      </Link>
                    ) : (
                      <span className="mono">{running.shortId || "—"}</span>
                    )}
                  </h3>
                  {running.mcpServers.length ? (
                    <div className="srv-grid">
                      {running.mcpServers.map((s, j) => (
                        <ServerCard key={s.name || j} server={s} />
                      ))}
                    </div>
                  ) : (
                    <EmptyNote>No session MCP servers.</EmptyNote>
                  )}
                </div>
              );
            })
          ) : (
            <EmptyNote>No running sessions.</EmptyNote>
          )}
        </div>
      </section>
    </>
  );
}
