import type { Project, Session } from "../gen/dashboard/v1/dashboard_pb";
import { projectTitle, sortSessions } from "../lib/dashboard";
import { SessionsTable } from "./SessionsTable";

/**
 * ProjectCard renders one claude-forge project's session history. `sessions`
 * (already filtered/sorted by the caller) defaults to the project's own
 * sessions sorted by last activity.
 */
export function ProjectCard({
  project,
  sessions,
}: {
  project: Project;
  sessions?: Session[];
}) {
  const list = sessions ?? sortSessions(project.sessions);
  const title = projectTitle(project);
  const count = list.length;

  return (
    <section className="project">
      <div className="p-head">
        <h2 title={project.id}>{title}</h2>
        {!project.resolved && <span className="unresolved">(unresolved)</span>}
        {project.resolved && project.dir && (
          <span className="mono faint clip" title={project.dir}>
            {project.dir}
          </span>
        )}
        <span className="count">
          {count} {count === 1 ? "session" : "sessions"}
        </span>
      </div>
      <div className="p-body">
        <SessionsTable
          sessions={list}
          runningSessions={project.runningSessions}
          label={title}
        />
      </div>
    </section>
  );
}
