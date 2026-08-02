---
title: Dashboard
weight: 4
---


`claude-forge dashboard` serves a local web UI that summarizes your claude-forge
activity across projects in one place.

```bash
claude-forge dashboard            # serves on http://127.0.0.1:8099
claude-forge dashboard --addr 127.0.0.1:9000
```

The server binds to loopback by default (it surfaces session and pull-request
details) and stops on Ctrl-C.

## What it shows

For every project under `~/.claude-forge/`:

- **Sessions** — each session's name, Claude session ID, git branch, worktree,
  creation time, and first message (the same records `claude-forge list` shows,
  gathered across all projects rather than just the current one).
- **Pull request** — the open/merged/closed PR associated with each session's
  branch, linked directly to GitHub. PR lookup uses the same GitHub token
  resolution as the gateway (`GITHUB_TOKEN`, then `gh` credentials); if no token
  is available, PRs are omitted and a warning is shown.
- **Session-scope MCP servers** — for each running session, the status of the
  MCP servers backing it (the per-session GitHub MCP sidecar and any custom
  `scope: session` container servers).
- **Global-scope MCP servers** — the shared, host-wide servers (the Kubernetes
  MCP and any custom `scope: global` container servers) with their running
  status, shown once at the top.

The page reads live Docker state on each load; use **Refresh** to re-fetch.

The UI is a React single-page app (its source lives in `frontend/`, embedded
into the binary at build time). It talks to the server over
[Connect](https://connectrpc.com/) using the schema in
`proto/dashboard/v1/dashboard.proto` — to consume the data programmatically,
call the same RPC (Connect, gRPC, or gRPC-Web all work; JSON shown here):

```bash
curl -X POST -H 'content-type: application/json' -d '{}' \
  http://127.0.0.1:8099/dashboard.v1.DashboardService/GetDashboard
```

## Notes and limitations

- The dashboard is read-only; it observes state and never starts, stops, or
  modifies sessions or containers.
- claude-forge records a session's Claude session ID (a UUID) and its running
  containers' short ID separately, with no stored link between them, so sessions
  and running sessions are shown as aligned lists per project rather than a
  single joined row.
- A project's git branch and pull request are only resolvable when its host
  directory can be located; projects whose directory cannot be resolved are
  still listed (with their sessions) and noted in a warning.
