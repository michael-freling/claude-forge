import type { McpScope } from "../../gen/dashboard/v1/dashboard_pb";
import { scopeClass, scopeLabel } from "../../lib/format";

/** ScopeBadge renders an MCP server's scope (global/session/builtin). */
export function ScopeBadge({ scope }: { scope: McpScope }) {
  return (
    <span className={"badge " + scopeClass(scope)}>{scopeLabel(scope)}</span>
  );
}
