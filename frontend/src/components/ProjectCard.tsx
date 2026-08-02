import type { Project } from "../gen/dashboard/v1/dashboard_pb";
import { RunningSessions } from "./RunningSessions";
import { SessionsTable } from "./SessionsTable";

/** ProjectCard renders one claude-forge project with its sessions. */
export function ProjectCard({ project }: { project: Project }) {
  const title =
    project.owner && project.repo
      ? `${project.owner}/${project.repo}`
      : project.id || "(unknown project)";
  const count = project.sessions.length;

  return (
    <section className="project">
      <div className="p-head">
        <h2 title={project.id}>{title}</h2>
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
        <SessionsTable sessions={project.sessions} />
        <RunningSessions sessions={project.runningSessions} />
      </div>
    </section>
  );
}
