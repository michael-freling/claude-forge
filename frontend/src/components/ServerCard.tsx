import type { McpServer } from "../gen/dashboard/v1/dashboard_pb";
import { KindBadge } from "./primitives/KindBadge";
import { ScopeBadge } from "./primitives/ScopeBadge";
import { StatusPill } from "./primitives/StatusPill";

/** ServerCard renders one MCP server's identity, tags, and runtime status. */
export function ServerCard({ server }: { server: McpServer }) {
  const status = server.status || (server.running ? "running" : "not running");
  const imageOrContainer = server.image || server.container;
  const imageTitle = [server.image, server.container]
    .filter(Boolean)
    .join(" · ");

  return (
    <div className="srv">
      <div className="srv-top">
        <span className="srv-name" title={server.name}>
          {server.name || "(unnamed)"}
        </span>
        <StatusPill
          running={server.running}
          label={server.running ? "running" : "stopped"}
          title={status}
        />
      </div>
      <div className="srv-tags">
        <ScopeBadge scope={server.scope} />
        <KindBadge kind={server.kind} />
      </div>
      <div className="srv-sub" title={status}>
        {status}
      </div>
      {imageOrContainer && (
        <div className="srv-sub srv-img mono" title={imageTitle}>
          {imageOrContainer}
        </div>
      )}
    </div>
  );
}
