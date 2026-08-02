import { timestampDate, type Timestamp } from "@bufbuild/protobuf/wkt";
import type { Session } from "../gen/dashboard/v1/dashboard_pb";
import { fullTimestamp } from "../lib/format";
import { CopyableId } from "./primitives/CopyableId";
import { PRBadge } from "./primitives/PRBadge";
import { RelativeTime } from "./primitives/RelativeTime";
import { WorktreeBadge } from "./primitives/WorktreeBadge";

function toDate(ts?: Timestamp): Date | undefined {
  return ts ? timestampDate(ts) : undefined;
}

const valid = (d?: Date): d is Date => d != null && !Number.isNaN(d.getTime());

/**
 * SessionRow renders one recorded session as a table row. The time column
 * shows last activity (falling back to creation); its tooltip carries both
 * absolute timestamps. `running` marks the session as currently live.
 */
export function SessionRow({
  session,
  running = false,
}: {
  session: Session;
  running?: boolean;
}) {
  const named = !!(session.name && session.name.trim());
  const created = toDate(session.createdAt);
  const lastActive = toDate(session.lastActive);
  const shown = valid(lastActive) ? lastActive : created;

  let timeTitle: string | undefined;
  if (valid(created)) {
    timeTitle = "Created " + fullTimestamp(created);
    if (valid(lastActive)) {
      timeTitle += "\nLast active " + fullTimestamp(lastActive);
    }
  }

  return (
    <tr>
      <td>
        <div
          className={"s-name" + (named ? "" : " unnamed")}
          title={session.name}
        >
          {running && (
            <span className="run-dot" title="running">
              <span className="sr-only">running: </span>
            </span>
          )}
          {named ? session.name : "(unnamed)"}
        </div>
      </td>
      <td>
        {session.branch ? (
          <span className="mono clip" title={session.branch}>
            {session.branch}
          </span>
        ) : (
          <span className="faint">—</span>
        )}
      </td>
      <td>
        <WorktreeBadge worktree={session.worktree} />
      </td>
      <td>
        <RelativeTime value={shown} title={timeTitle} />
      </td>
      <td className="col-msg">
        {session.firstMessage ? (
          <span className="msg-clip" title={session.firstMessage}>
            {session.firstMessage}
          </span>
        ) : (
          <span className="faint">—</span>
        )}
      </td>
      <td>
        {session.id ? (
          <CopyableId id={session.id} />
        ) : (
          <span className="faint">—</span>
        )}
      </td>
      <td>
        <PRBadge pr={session.pr} />
      </td>
    </tr>
  );
}
