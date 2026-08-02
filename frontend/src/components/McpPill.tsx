import type { McpServer } from "../gen/dashboard/v1/dashboard_pb";
import { StatusPill } from "./primitives/StatusPill";

/** McpPill renders a single session-scope MCP server as a status pill. */
export function McpPill({ server }: { server: McpServer }) {
  const status = server.status || (server.running ? "running" : "not running");
  return (
    <StatusPill
      running={server.running}
      label={server.name || "(unnamed)"}
      title={status}
    />
  );
}
