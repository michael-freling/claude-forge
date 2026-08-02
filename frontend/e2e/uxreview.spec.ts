import { test } from "@playwright/test";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import {
  McpKind,
  McpScope,
  PullRequestState,
  type Dashboard,
  type Project,
  type Session,
} from "../src/gen/dashboard/v1/dashboard_pb";
import { connectJson, emptyDashboard, richDashboard, RPC_PATH } from "./fixtures";

const OUT = process.env.UX_OUT ?? "/tmp/ux-shots";

function session(
  id: string,
  name: string,
  branch: string,
  worktree: string,
  createdOffsetH: number,
  firstMessage: string,
  pr?: Session["pr"],
): Session {
  const created = new Date(Date.now() - createdOffsetH * 3600_000);
  return {
    $typeName: "dashboard.v1.Session",
    id,
    name,
    worktree,
    branch,
    createdAt: timestampFromDate(created),
    lastActive: timestampFromDate(new Date(created.getTime() + 30 * 60_000)),
    firstMessage,
    pr,
  };
}

function pr(
  number: number,
  title: string,
  state: PullRequestState,
  draft = false,
): NonNullable<Session["pr"]> {
  return {
    $typeName: "dashboard.v1.PullRequest",
    number,
    title,
    state,
    url: `https://github.com/octo/cat/pull/${number}`,
    draft,
  };
}

/** Several projects, mixed PR states, running sessions, warnings. */
function bigDashboard(): Dashboard {
  const d = richDashboard();
  d.warnings = [
    "No GitHub token; PR data is unavailable for private repos.",
    "project -home-user-legacy-app: worktree dir missing",
  ];
  d.global!.servers.push({
    $typeName: "dashboard.v1.McpServer",
    name: "playwright",
    scope: McpScope.GLOBAL,
    kind: McpKind.STDIO,
    container: "",
    image: "",
    status: "",
    running: false,
  });
  const p1: Project = d.projects[0];
  p1.sessions = [
    session(
      "abcdef12-3456-7890-abcd-ef1234567890",
      "wire up dashboard",
      "feat/dashboard",
      "web-dashboard",
      1,
      "Implement the dashboard SPA per the design spec.",
      pr(128, "feat: dashboard SPA", PullRequestState.OPEN),
    ),
    session(
      "bbcdef12-3456-7890-abcd-ef1234567891",
      "",
      "fix/rbac-tokens",
      "",
      30,
      "Keep Kubernetes MCP ServiceAccount tokens fresh across long sessions so attach never sees a 401",
      pr(94, "fix: keep Kubernetes MCP ServiceAccount tokens fresh across long sessions", PullRequestState.MERGED),
    ),
    session(
      "cccdef12-3456-7890-abcd-ef1234567892",
      "prune improvements",
      "fix/prune",
      "prune-work",
      70,
      "prune should delete by last activity",
      pr(93, "fix: prune deletes by last activity", PullRequestState.CLOSED),
    ),
    session(
      "ddcdef12-3456-7890-abcd-ef1234567893",
      "docs site",
      "feat/docs",
      "docs-site",
      120,
      "Add a user-facing docs site on GitHub Pages",
      pr(92, "feat: docs site (draft)", PullRequestState.OPEN, true),
    ),
    session(
      "eecdef12-3456-7890-abcd-ef1234567894",
      "",
      "",
      "",
      200,
      "",
    ),
  ];
  p1.runningSessions = [
    {
      $typeName: "dashboard.v1.RunningSession",
      shortId: "abcdef12",
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
        {
          $typeName: "dashboard.v1.McpServer",
          name: "kubernetes",
          scope: McpScope.SESSION,
          kind: McpKind.CONTAINER,
          container: "forge-kube-mcp",
          image: "ghcr.io/containers/kubernetes-mcp-server",
          status: "Up 3 minutes",
          running: true,
        },
      ],
    },
    {
      $typeName: "dashboard.v1.RunningSession",
      shortId: "ffcdef12",
      mcpServers: [
        {
          $typeName: "dashboard.v1.McpServer",
          name: "github",
          scope: McpScope.SESSION,
          kind: McpKind.CONTAINER,
          container: "forge-github-mcp-ffcdef12",
          image: "ghcr.io/github/github-mcp-server",
          status: "Exited (1) 2 minutes ago",
          running: false,
        },
      ],
    },
  ];
  d.projects.push(
    {
      $typeName: "dashboard.v1.Project",
      id: "-home-user-legacy-app",
      dir: "/home/user/legacy/app",
      owner: "octo",
      repo: "legacy-app",
      resolved: true,
      sessions: [
        session(
          "11cdef12-3456-7890-abcd-ef1234567895",
          "migrate to v2 API",
          "chore/migrate-v2",
          "migrate-v2",
          400,
          "Migrate the legacy app to the v2 API endpoints",
        ),
      ],
      runningSessions: [],
    },
    {
      $typeName: "dashboard.v1.Project",
      id: "-home-user-experiments",
      dir: "",
      owner: "",
      repo: "",
      resolved: false,
      sessions: [],
      runningSessions: [],
    },
  );
  return d;
}

const viewports = [
  { name: "desktop", width: 1280, height: 900 },
  { name: "mobile", width: 375, height: 800 },
];
const schemes = ["light", "dark"] as const;

for (const vp of viewports) {
  for (const scheme of schemes) {
    test(`big ${vp.name} ${scheme}`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
      await page.setViewportSize({ width: vp.width, height: vp.height });
      await page.route(RPC_PATH, (route) =>
        route.fulfill({
          contentType: "application/json",
          body: connectJson(bigDashboard()),
        }),
      );
      await page.goto("/");
      await page.waitForSelector("section.project");
      await page.screenshot({
        path: `${OUT}/big-${vp.name}-${scheme}.png`,
        fullPage: true,
      });
    });
  }
}

test("empty desktop light", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "light" });
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.route(RPC_PATH, (route) =>
    route.fulfill({
      contentType: "application/json",
      body: connectJson(emptyDashboard()),
    }),
  );
  await page.goto("/");
  await page.waitForSelector(".state");
  await page.screenshot({ path: `${OUT}/empty-desktop-light.png`, fullPage: true });
});

test("error first load", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.route(RPC_PATH, (route) => route.abort("connectionrefused"));
  await page.goto("/");
  await page.waitForSelector(".state[role=alert]");
  await page.screenshot({ path: `${OUT}/error-desktop-light.png`, fullPage: true });
});

test("refresh error over stale data", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  let calls = 0;
  await page.route(RPC_PATH, (route) => {
    calls++;
    if (calls === 1) {
      return route.fulfill({
        contentType: "application/json",
        body: connectJson(bigDashboard()),
      });
    }
    return route.abort("connectionrefused");
  });
  await page.goto("/");
  await page.waitForSelector("section.project");
  await page.getByRole("button", { name: "Refresh" }).click();
  await page.waitForSelector(".warn");
  await page.screenshot({ path: `${OUT}/refresh-error-desktop.png`, fullPage: true });
});
