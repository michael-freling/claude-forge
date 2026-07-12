---
title: Custom MCP Servers
weight: 4
---

# Custom MCP Servers

Beyond the built-in `github` (and optional `kubernetes`) servers, you can expose
any number of additional MCP servers to the agent via the `mcp_servers` list in
`config.yaml`. Each entry is one of these kinds:

- **Remote** (`type: http` or `type: sse`) — an HTTP/SSE endpoint reached over
  the network. Set `url`, and optionally `headers` for authentication. This is
  the right choice for hosted third-party servers such as the Vercel MCP server.
- **Stdio** (`type: stdio`) — a command launched by Claude Code inside the agent
  container. Set `command`, and optionally `args` and `env`. The command must be
  available in the agent image.
- **Container** (`type: container`) — a server claude-forge runs as a **sidecar**
  container, reached at `http://<name>:<port><path>` (default path `/mcp`). Use
  this for any MCP server packaged as a container, or to run a locally-installed
  one that the agent image doesn't have. By default it runs once and is shared
  across sessions (see **Scope** below). Two forms:
  - **Native HTTP image** — set `image` + `port` (+ optional `args`). The image
    serves MCP over HTTP itself.
  - **Wrapped stdio** — set `command` (+ optional `args`). claude-forge runs the
    command inside a sidecar and bridges its stdio to HTTP with
    [supergateway](https://github.com/supercorp-ai/supergateway). The base
    `image` defaults to `node:22-slim` (so `npx`-based servers work); override
    it when the command needs a different runtime or extra CLIs. Pass secrets
    via `env` (the wrapped process inherits the sidecar's environment) and host
    paths via `mounts` (`host:container[:ro]`).

  The sidecar's `name` is used as its network hostname, so it must be a valid
  DNS label. A server that fails to start is skipped with a warning rather than
  aborting the session.

  **Scope** (`scope:`, container only) controls how the instance is reused:
  - `global` (default) — a single shared instance on the `forge-shared` network,
    reused across all sessions (the same model as the Kubernetes MCP), so N
    sessions don't spawn N copies. A global server is **shared across projects**,
    **outlives individual sessions**, and is managed with
    `claude-forge mcp restart` rather than per-session cleanup — so it must not
    depend on any one session's or project's secrets.
  - `session` — one sidecar per session, on the session network, torn down when
    the session ends. Use it for servers that need per-session/per-project
    isolation.

> Note: a `type: stdio` server runs in the *agent* container and needs its
> command baked into the agent image; a `type: container` **wrapped stdio**
> server runs in its own sidecar, so you can bring arbitrary runtimes without
> modifying the agent image. Remote HTTP servers are reachable because the agent
> container has normal outbound internet — the git gateway only proxies git
> traffic.

## Environment variable expansion

`${VAR}` references in `url`, `headers`, `command`, `args`, and `env` are
expanded from your host environment when a session starts, so API tokens can be
supplied without being written into `config.yaml`:

```yaml
mcp_servers:
  - name: example
    type: http
    url: https://mcp.example.com
    headers:
      Authorization: "Bearer ${EXAMPLE_TOKEN}"
```

```bash
export EXAMPLE_TOKEN=...
claude-forge start "work session"
```

Server names must be unique and may not shadow the built-in `github` or
`kubernetes` servers.

## OAuth MCP servers (e.g. Vercel)

Some hosted MCP servers authenticate via an interactive OAuth flow instead of a
static token. That flow (browser + localhost callback) **cannot complete inside
the headless container**, so authenticate once on the host — Claude Code stores
the resulting token in `~/.claude/.credentials.json`, which claude-forge mounts
into the container, so every session reuses it.

Mark such servers with `oauth: true`:

```yaml
mcp_servers:
  - name: vercel
    type: http
    url: https://mcp.vercel.com
    oauth: true
```

Authenticate once on the host (use the **same name and URL** so the container
finds the token):

```bash
claude mcp add --transport http vercel https://mcp.vercel.com
claude            # then run: /mcp  →  vercel  →  Authenticate
```

At session start, claude-forge preflights every `oauth: true` server and prints
a warning (without blocking the session) if its token is missing or expired, so
you learn up front that the server won't connect rather than discovering it
mid-session.

> Note: OAuth token reuse relies on host and container sharing the file-based
> credential store, which is the case on Linux/WSL. On macOS the token is kept
> in the system Keychain (not the mounted file), so this carry-over does not
> apply there.
