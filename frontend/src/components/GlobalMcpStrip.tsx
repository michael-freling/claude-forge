import { Link } from "react-router-dom";
import type { McpServer } from "../gen/dashboard/v1/dashboard_pb";

/**
 * GlobalMcpStrip is the Running page's one-line global-MCP health summary
 * ("Global MCP: ● kubernetes · ○ playwright"), linking to the servers page.
 * The filled/hollow dot is decorative; a visually-hidden suffix carries the
 * state for screen readers.
 */
export function GlobalMcpStrip({ servers }: { servers: McpServer[] }) {
  return (
    <Link to="/servers" className="mcp-strip">
      <span className="strip-label">Global MCP:</span>
      {servers.length ? (
        servers.map((s, i) => (
          <span
            key={s.name || i}
            className={"strip-item" + (s.running ? " on" : " off")}
          >
            <span className="strip-dot" aria-hidden="true">
              {s.running ? "●" : "○"}
            </span>{" "}
            <span className="strip-name">{s.name || "(unnamed)"}</span>
            <span className="sr-only">
              {s.running ? ": running" : ": stopped"}
            </span>
          </span>
        ))
      ) : (
        <span className="strip-item off">none running</span>
      )}
      <span className="strip-more" aria-hidden="true">
        all servers →
      </span>
    </Link>
  );
}
