import type { RunningSession, Session } from "../gen/dashboard/v1/dashboard_pb";
import { findRunningForSession } from "../lib/dashboard";
import { EmptyNote } from "./primitives/EmptyNote";
import { SessionRow } from "./SessionRow";

const HEADERS = [
  "Name",
  "Branch",
  "Worktree",
  "Last active",
  "First message",
  "Claude ID",
  "PR",
];

/**
 * SessionsTable renders a project's recorded sessions, or an empty note. The
 * scroll container is a keyboard-focusable named region so the horizontal
 * overflow stays reachable without a pointer. Rows whose session is currently
 * running (matched by Claude session id) get a green running dot plus a link
 * to that running session's server section on /servers; `projectId` scopes
 * the anchor those links target.
 */
export function SessionsTable({
  sessions,
  runningSessions = [],
  projectId = "",
  label,
}: {
  sessions: Session[];
  runningSessions?: RunningSession[];
  projectId?: string;
  label: string;
}) {
  if (!sessions.length) {
    return <EmptyNote>No sessions.</EmptyNote>;
  }
  return (
    <div
      className="table-wrap"
      tabIndex={0}
      role="region"
      aria-label={`Sessions for ${label}`}
    >
      <table className="sessions" aria-label={`Sessions for ${label}`}>
        <thead>
          <tr>
            {HEADERS.map((h) => (
              <th key={h} scope="col">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {sessions.map((s, i) => (
            <SessionRow
              key={s.id || i}
              session={s}
              projectId={projectId}
              running={findRunningForSession(runningSessions, s)}
            />
          ))}
        </tbody>
      </table>
    </div>
  );
}
