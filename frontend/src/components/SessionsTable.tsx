import type { Session } from "../gen/dashboard/v1/dashboard_pb";
import { EmptyNote } from "./primitives/EmptyNote";
import { SessionRow } from "./SessionRow";

const HEADERS = [
  "Name",
  "Branch",
  "Worktree",
  "Created",
  "First message",
  "Claude ID",
  "PR",
];

/** SessionsTable renders a project's recorded sessions, or an empty note. */
export function SessionsTable({ sessions }: { sessions: Session[] }) {
  if (!sessions.length) {
    return <EmptyNote>No sessions.</EmptyNote>;
  }
  return (
    <div className="table-wrap">
      <table className="sessions">
        <thead>
          <tr>
            {HEADERS.map((h) => (
              <th key={h}>{h}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {sessions.map((s, i) => (
            <SessionRow key={s.id || i} session={s} />
          ))}
        </tbody>
      </table>
    </div>
  );
}
