import type { RunningSession } from "../gen/dashboard/v1/dashboard_pb";
import { McpPill } from "./McpPill";
import { EmptyNote } from "./primitives/EmptyNote";

/**
 * RunningSessions renders the live sessions of a project, each with its
 * session-scope MCP server pills.
 */
export function RunningSessions({ sessions }: { sessions: RunningSession[] }) {
  return (
    <div className="running">
      <h3>Running sessions</h3>
      {sessions.length ? (
        sessions.map((rs, i) => (
          <div className="run-row" key={rs.shortId || i}>
            <span className="run-id">{rs.shortId || "—"}</span>
            <div className="pills">
              {rs.mcpServers.length ? (
                rs.mcpServers.map((m, j) => (
                  <McpPill key={m.name || j} server={m} />
                ))
              ) : (
                <span className="faint">no session MCP servers</span>
              )}
            </div>
          </div>
        ))
      ) : (
        <EmptyNote>No running sessions.</EmptyNote>
      )}
    </div>
  );
}
