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

## Pages

The UI is split into three pages (client-side routes; deep links work):

- **Running** (`/`, the landing page) — only what is currently live. Each
  running session appears as a card with its project, container short ID, and
  the metadata of its recorded session — name, branch, worktree, and pull
  request — joined via the Claude session ID recovered from the agent
  container's command line. The card also shows the session-scope MCP servers (the
  per-session GitHub MCP sidecar and any custom `scope: session` container
  servers) as status pills. A one-line strip at the top summarizes global MCP
  health and links to the servers page.
- **Sessions** (`/sessions`) — the history: per-project tables of every
  recorded session with its name, branch, worktree, last-active time, first
  message, Claude session ID, and linked pull request (the same records
  `claude-forge list` shows, gathered across all projects). Sessions are
  sorted most-recently-active first, projects with running sessions lead, a
  currently running session gets a green dot on its name, and a text filter
  narrows rows by name, branch, or first message.
- **MCP servers** (`/servers`) — the shared, host-wide servers (the Kubernetes
  MCP and any custom `scope: global` container servers) with their running
  status, plus a section per running session listing its session-scope
  servers.

PR lookup uses the same GitHub token resolution as the gateway
(`GITHUB_TOKEN`, then `gh` credentials); if no token is available, PRs are
omitted and a warning is shown.

The page reads live Docker state on each fetch. It auto-refreshes every 30
seconds while the tab is visible (paused in background tabs, refreshed
immediately when you return); **Refresh** re-fetches on demand.

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
- The running↔session join uses the Claude session ID recovered from the
  agent container's `--session-id`/`--resume` arguments (the container short
  ID and the Claude session ID are unrelated values). A session started with
  `--continue`, or whose container cannot be inspected, still appears — with
  its short ID standing in for the name.
- A project's git branch and pull request are only resolvable when its host
  directory can be located; projects whose directory cannot be resolved are
  still listed with their sessions and marked "directory removed" inline — a
  removed checkout or deleted worktree is normal lifecycle, so it does not
  raise a warning.
