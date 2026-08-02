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

/** SessionRow renders one recorded session as a table row. */
export function SessionRow({ session }: { session: Session }) {
  const named = !!(session.name && session.name.trim());
  const created = toDate(session.createdAt);
  const lastActive = toDate(session.lastActive);

  let createdTitle: string | undefined;
  if (created && !Number.isNaN(created.getTime())) {
    createdTitle = "Created " + fullTimestamp(created);
    if (lastActive && !Number.isNaN(lastActive.getTime())) {
      createdTitle += "\nLast active " + fullTimestamp(lastActive);
    }
  }

  return (
    <tr>
      <td>
        <div
          className={"s-name" + (named ? "" : " unnamed")}
          title={session.name}
        >
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
        <RelativeTime value={created} title={createdTitle} />
      </td>
      <td className="col-msg">
        {session.firstMessage ? (
          <span className="clip" title={session.firstMessage}>
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
