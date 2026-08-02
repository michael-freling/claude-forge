import { useState } from "react";
import { NoProjectsState } from "../components/NoProjectsState";
import { ProjectCard } from "../components/ProjectCard";
import { SummaryStats } from "../components/SummaryStats";
import { EmptyNote } from "../components/primitives/EmptyNote";
import type { Dashboard } from "../gen/dashboard/v1/dashboard_pb";
import { matchesFilter, sortProjects, sortSessions } from "../lib/dashboard";

/**
 * SessionsPage is the history view: per-project session tables, most recent
 * activity first (projects with running sessions lead), with a free-text
 * filter over name/branch/first message. While filtering, non-matching rows
 * and projects without any match are hidden.
 */
export function SessionsPage({ data }: { data: Dashboard }) {
  const [query, setQuery] = useState("");

  if (!data.projects.length) {
    return <NoProjectsState />;
  }

  const filtering = query.trim() !== "";
  const visible = sortProjects(data.projects)
    .map((project) => ({
      project,
      sessions: sortSessions(
        project.sessions.filter((s) => matchesFilter(s, query)),
      ),
    }))
    .filter(({ sessions }) => !filtering || sessions.length > 0);
  const sessionCount = data.projects.reduce(
    (n, p) => n + p.sessions.length,
    0,
  );

  return (
    <>
      <div className="sessions-bar">
        <SummaryStats
          projectCount={data.projects.length}
          sessionCount={sessionCount}
        />
        <input
          type="search"
          className="filter"
          placeholder="Filter by name, branch, or first message…"
          aria-label="Filter sessions"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </div>
      {visible.length ? (
        visible.map(({ project, sessions }, i) => (
          <ProjectCard
            key={project.id || i}
            project={project}
            sessions={sessions}
          />
        ))
      ) : (
        <EmptyNote>No sessions match “{query.trim()}”.</EmptyNote>
      )}
    </>
  );
}
