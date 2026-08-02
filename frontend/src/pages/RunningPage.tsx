import { Link } from "react-router-dom";
import type { Dashboard } from "../gen/dashboard/v1/dashboard_pb";
import { runningEntries } from "../lib/dashboard";
import { GlobalMcpStrip } from "../components/GlobalMcpStrip";
import { RunningSessionCard } from "../components/RunningSessionCard";
import { MoonIcon } from "../components/primitives/icons";

/**
 * RunningPage is the landing page: only what is currently live — every
 * running session (across all projects) joined with its recorded session
 * metadata, plus a one-line global-MCP health strip.
 */
export function RunningPage({ data }: { data: Dashboard }) {
  const entries = runningEntries(data.projects);
  return (
    <>
      <GlobalMcpStrip servers={data.global?.servers ?? []} />
      {entries.length ? (
        entries.map(({ project, running }, i) => (
          <RunningSessionCard
            key={`${project.id || i}:${running.shortId || i}`}
            project={project}
            running={running}
          />
        ))
      ) : (
        <section className="panel">
          <div className="state">
            <div className="ico" aria-hidden="true">
              <MoonIcon size={34} />
            </div>
            <h2>No sessions running</h2>
            <p className="muted">
              <Link to="/sessions">Browse past sessions</Link> or start a new
              one with <code className="mono">claude-forge start</code>.
            </p>
          </div>
        </section>
      )}
    </>
  );
}
