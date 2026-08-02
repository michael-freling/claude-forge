import type { McpKind } from "../../gen/dashboard/v1/dashboard_pb";
import { kindLabel } from "../../lib/format";

/** KindBadge renders an MCP server's transport kind (container/stdio/remote). */
export function KindBadge({ kind }: { kind: McpKind }) {
  return <span className="badge badge-kind">{kindLabel(kind)}</span>;
}
