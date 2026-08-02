import { create, toJson } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import {
  GetDashboardResponseSchema,
  McpKind,
  McpScope,
  PullRequestState,
  type Dashboard,
} from "../src/gen/dashboard/v1/dashboard_pb";

/** The RPC path the SPA POSTs to (baseUrl "/"). */
export const RPC_PATH = "**/dashboard.v1.DashboardService/GetDashboard";

/**
 * connectJson encodes a Dashboard as the Connect unary JSON response body that
 * connect-web will decode client-side.
 */
export function connectJson(dashboard: Dashboard): string {
  const res = create(GetDashboardResponseSchema, { dashboard });
  return JSON.stringify(toJson(GetDashboardResponseSchema, res));
}

/** A rich dashboard: one project with a session (+ open PR) and a running MCP. */
export function richDashboard(): Dashboard {
  return {
    $typeName: "dashboard.v1.Dashboard",
    generatedAt: timestampFromDate(new Date("2026-08-02T12:34:00Z")),
    warnings: ["No GitHub token; PR data is unavailable for private repos."],
    global: {
      $typeName: "dashboard.v1.GlobalMcp",
      servers: [
        {
          $typeName: "dashboard.v1.McpServer",
          name: "kubernetes",
          scope: McpScope.GLOBAL,
          kind: McpKind.CONTAINER,
          container: "forge-kube-mcp",
          image: "ghcr.io/containers/kubernetes-mcp-server:latest",
          status: "Up 3 minutes",
          running: true,
        },
      ],
    },
    projects: [
      {
        $typeName: "dashboard.v1.Project",
        id: "-home-user-octo-cat",
        dir: "/home/user/octo/cat",
        owner: "octo",
        repo: "cat",
        resolved: true,
        sessions: [
          {
            $typeName: "dashboard.v1.Session",
            id: "abcdef12-3456-7890-abcd-ef1234567890",
            name: "wire up dashboard",
            worktree: "web-dashboard",
            branch: "feat/dashboard",
            createdAt: timestampFromDate(new Date("2026-08-02T11:00:00Z")),
            lastActive: timestampFromDate(new Date("2026-08-02T12:20:00Z")),
            firstMessage: "Implement the dashboard SPA per the design spec.",
            pr: {
              $typeName: "dashboard.v1.PullRequest",
              number: 128,
              title: "feat: dashboard SPA",
              state: PullRequestState.OPEN,
              url: "https://github.com/octo/cat/pull/128",
              draft: false,
            },
          },
        ],
        runningSessions: [
          {
            $typeName: "dashboard.v1.RunningSession",
            shortId: "abcdef12",
            claudeSessionId: "abcdef12-3456-7890-abcd-ef1234567890",
            mcpServers: [
              {
                $typeName: "dashboard.v1.McpServer",
                name: "github",
                scope: McpScope.SESSION,
                kind: McpKind.CONTAINER,
                container: "forge-github-mcp-abcdef12",
                image: "ghcr.io/github/github-mcp-server",
                status: "Up 1 minute",
                running: true,
              },
            ],
          },
        ],
      },
    ],
  };
}

/**
 * A dashboard whose running session has no recorded-session match: the
 * shortId prefix-matches nothing, so the join must fall back.
 */
export function unjoinedDashboard(): Dashboard {
  const d = richDashboard();
  d.warnings = [];
  d.projects[0].runningSessions[0].shortId = "ffffffff";
  d.projects[0].runningSessions[0].claudeSessionId = "";
  return d;
}

/** A dashboard whose only session carries a hostile javascript: PR url. */
export function hostilePrDashboard(): Dashboard {
  const d = richDashboard();
  d.warnings = [];
  d.projects[0].sessions[0].pr = {
    $typeName: "dashboard.v1.PullRequest",
    number: 66,
    title: "totally legit",
    url: "javascript:alert(document.domain)",
    state: PullRequestState.OPEN,
    draft: false,
  };
  d.projects[0].runningSessions = [];
  return d;
}

/** An empty dashboard: no projects. */
export function emptyDashboard(): Dashboard {
  return {
    $typeName: "dashboard.v1.Dashboard",
    generatedAt: timestampFromDate(new Date("2026-08-02T12:34:00Z")),
    warnings: [],
    global: { $typeName: "dashboard.v1.GlobalMcp", servers: [] },
    projects: [],
  };
}
