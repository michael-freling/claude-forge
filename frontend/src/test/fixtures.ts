import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import {
  DashboardSchema,
  GlobalMcpSchema,
  McpKind,
  McpScope,
  McpServerSchema,
  ProjectSchema,
  PullRequestSchema,
  PullRequestState,
  RunningSessionSchema,
  SessionSchema,
  type Dashboard,
  type GlobalMcp,
  type McpServer,
  type Project,
  type PullRequest,
  type RunningSession,
  type Session,
} from "../gen/dashboard/v1/dashboard_pb";

type Init<T> = Partial<Omit<T, "$typeName" | "$unknown">>;

export function makeServer(partial: Init<McpServer> = {}): McpServer {
  return create(McpServerSchema, {
    name: "github",
    scope: McpScope.GLOBAL,
    kind: McpKind.CONTAINER,
    container: "",
    image: "",
    status: "",
    running: true,
    ...partial,
  });
}

export function makePR(partial: Init<PullRequest> = {}): PullRequest {
  return create(PullRequestSchema, {
    number: 42,
    title: "Add a thing",
    state: PullRequestState.OPEN,
    url: "https://github.com/o/r/pull/42",
    draft: false,
    ...partial,
  });
}

export function makeSession(partial: Init<Session> = {}): Session {
  return create(SessionSchema, {
    id: "abcdef12-3456-7890-abcd-ef1234567890",
    name: "my session",
    worktree: "",
    branch: "",
    firstMessage: "",
    createdAt: timestampFromDate(new Date("2026-08-02T12:00:00Z")),
    lastActive: timestampFromDate(new Date("2026-08-02T12:30:00Z")),
    ...partial,
  });
}

export function makeRunningSession(
  partial: Init<RunningSession> = {},
): RunningSession {
  return create(RunningSessionSchema, {
    shortId: "abcdef12",
    // Matches makeSession's default id so default fixtures join. The shortId
    // itself never joins anything: the ids are independent in production.
    claudeSessionId: "abcdef12-3456-7890-abcd-ef1234567890",
    mcpServers: [],
    ...partial,
  });
}

export function makeProject(partial: Init<Project> = {}): Project {
  return create(ProjectSchema, {
    id: "-home-user-repo",
    dir: "/home/user/repo",
    owner: "octo",
    repo: "cat",
    resolved: true,
    sessions: [],
    runningSessions: [],
    ...partial,
  });
}

export function makeGlobal(servers: McpServer[] = []): GlobalMcp {
  return create(GlobalMcpSchema, { servers });
}

export function makeDashboard(partial: Init<Dashboard> = {}): Dashboard {
  return create(DashboardSchema, {
    generatedAt: timestampFromDate(new Date("2026-08-02T12:34:00Z")),
    warnings: [],
    global: { servers: [] },
    projects: [],
    ...partial,
  });
}
