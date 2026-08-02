import type { McpServer } from "../gen/dashboard/v1/dashboard_pb";
import { ServerCard } from "./ServerCard";
import { EmptyNote } from "./primitives/EmptyNote";

/** GlobalPanel renders the shared, host-wide MCP servers. */
export function GlobalPanel({ servers }: { servers: McpServer[] }) {
  return (
    <section className="panel">
      <div className="p-head">
        <h2>Global MCP servers</h2>
        <span className="count">{servers.length} running</span>
      </div>
      <div className="p-body">
        {servers.length ? (
          <div className="srv-grid">
            {servers.map((s, i) => (
              <ServerCard key={s.name || i} server={s} />
            ))}
          </div>
        ) : (
          <EmptyNote>No global MCP servers running.</EmptyNote>
        )}
      </div>
    </section>
  );
}
