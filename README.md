# claude-forge

`claude-forge` launches Claude Code inside isolated Docker containers with a secure gateway proxy for GitHub access. The gateway allows read operations (clone, pull, fetch) to any repository but restricts push and write operations to only the current project's repository.

## Features

- Runs Claude Code in Docker with `--dangerously-skip-permissions` by default
- Gateway proxy mediates all GitHub traffic (git + API) with per-repo write restrictions
- Per-session **GitHub MCP** sidecar scoped to the current repository
- Optional shared **Kubernetes MCP** server for cluster access, gated by generated RBAC
- Custom **MCP servers** (remote http/sse or stdio) configurable per install
- Optional **Docker-in-Docker**: sessions get their own isolated Docker daemon, unable to see or touch host or other sessions' containers
- Multiple instances can run in parallel across different projects
- Named sessions with persistence — resume previous sessions by ID or name
- Automatic auth detection from `ANTHROPIC_API_KEY`, `CLAUDE_CODE_OAUTH_TOKEN`, or `~/.claude/.credentials.json`
- Dependency caching for Go, npm, pnpm, and pip
- Host git identity, Claude Code configuration, and plugins carried into the container

## Prerequisites

- Docker (daemon must be running)
- Go 1.25+ (to build from source)
- A Claude Code subscription or API key

## Installation

```bash
go install github.com/michael-freling/claude-forge/cmd/claude-forge@latest
```

Or build from source:

```bash
git clone https://github.com/michael-freling/claude-forge.git
cd claude-forge
go build -o claude-forge ./cmd/claude-forge/
```

## Quick Start

```bash
# Navigate to your project directory
cd ~/my-project

# Start an interactive session (the name is required)
claude-forge start "add auth tests"

# Start with a prompt (non-interactive, exits when done)
claude-forge start "auth tests" -p "Add unit tests for the auth package"
```

On first run, `claude-forge build` is called automatically to pull the agent, gateway, and GitHub MCP Docker images.

## Usage

### Start a Session

`start` takes a required human-readable `<name>` argument. The name is passed to
Claude Code and shown in `claude-forge list`.

```bash
# Interactive session (attaches to container TTY)
claude-forge start "refactor parser"

# Non-interactive with a prompt
claude-forge start "fix flaky test" -p "Fix the flaky test in auth_test.go"

# With worktree mode
claude-forge start "experiment" --worktree

# Without --dangerously-skip-permissions
claude-forge start "careful run" --no-skip-permissions

# Mount additional host directories (repeatable)
claude-forge start "with data" --mount /host/data:/work/data
```

### List and Resume Sessions

```bash
# List past sessions for the current project
claude-forge list

# Resume the most recent session
claude-forge resume

# Resume a specific session by ID or name
claude-forge resume <session-id>
claude-forge resume "refactor parser"

# Override the session name on resume
claude-forge resume <session-id> --name "new name"
```

### Prune Old Sessions

```bash
# Delete sessions older than 30 days (default)
claude-forge prune

# Delete sessions older than a custom age
claude-forge prune --older-than 7d

# Keep the 10 most recent sessions, delete the rest
claude-forge prune --keep 10

# Preview without deleting
claude-forge prune --dry-run
```

### Manage Containers

```bash
# Show running containers
claude-forge status

# Stop containers for the current project
claude-forge stop
```

### Other Commands

```bash
# Write a default config to ~/.config/claude-forge/config.yaml
claude-forge init

# Pull/rebuild Docker images
claude-forge build

# Verify authentication credentials
claude-forge auth

# Sync host Claude Code plugins into forge's plugin directory
# (this also runs automatically at the start of every session)
claude-forge plugins sync

# Restart the shared MCP server containers (e.g. Kubernetes MCP)
claude-forge mcp restart

# Show version
claude-forge version
```

## Configuration

Configuration is stored in `~/.config/claude-forge/config.yaml`. Run
`claude-forge init` to write a default file with detected settings.

```yaml
# Override Docker images (defaults to GHCR.io images)
images:
  agent: ghcr.io/michael-freling/claude-forge-agent:latest
  gateway: ghcr.io/michael-freling/claude-forge-gateway:latest
  github_mcp: ghcr.io/michael-freling/claude-forge-github-mcp:latest

# Default flags
defaults:
  skip_permissions: true
  worktree: false

# Optional Docker-in-Docker: give sessions their own isolated Docker daemon.
# The agent can build/run/stop its own containers, but cannot see or control
# the host's containers, claude-forge's containers, or other sessions'
# containers (the host Docker socket is never mounted). Requires running the
# agent container with --privileged, so it is disabled by default.
docker:
  enabled: false

# Custom MCP servers (in addition to the built-in github/kubernetes servers)
mcp_servers:
  # Remote server (http/sse) with a static token
  - name: example
    type: http                       # http (default) | sse | stdio
    url: https://mcp.example.com
    headers:
      Authorization: "Bearer ${EXAMPLE_TOKEN}"
  # Remote server that authenticates via OAuth (e.g. hosted Vercel MCP)
  - name: vercel
    type: http
    url: https://mcp.vercel.com
    oauth: true
  # Stdio server — a command run inside the agent container
  - name: my-tool
    type: stdio
    command: my-mcp-server
    args: ["--flag"]
    env:
      API_KEY: "${MY_TOOL_API_KEY}"
  # Container server — claude-forge runs it as a sidecar. scope defaults to
  # global (one shared instance reused across all sessions).
  - name: gcloud
    type: container
    command: npx           # wrapped stdio (bridged to HTTP)
    args: ["-y", "@google-cloud/gcloud-mcp"]
    mounts:
      - "~/.config/gcloud:/root/.config/gcloud:ro"
  # Per-session (isolated) native HTTP image
  - name: my-sidecar
    type: container
    scope: session
    image: ghcr.io/example/some-mcp:latest
    port: 8080
    path: /mcp

# Optional Kubernetes MCP integration
kubernetes:
  enabled: false
  image: ghcr.io/containers/kubernetes-mcp-server:latest
  default_context: dev
  contexts:
    - host_context: dev
      service_account_name: claude-forge-agent
      service_account_namespace: default
```

## Custom MCP Servers

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

### OAuth MCP servers (e.g. Vercel)

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

## Authentication

`claude-forge` resolves credentials in this order:

1. `ANTHROPIC_API_KEY` environment variable
2. `CLAUDE_CODE_OAUTH_TOKEN` environment variable
3. `~/.claude/.credentials.json` (written by `claude` CLI login)

Run `claude-forge auth` to verify your credentials are detected.

## Kubernetes Access

When `kubernetes.enabled` is set, claude-forge runs a shared Kubernetes MCP
server that Claude Code can use to inspect and operate on your cluster. Access
is constrained by RBAC rather than by disabling destructive operations — the
agent's permissions are exactly what you grant its service account.

Generate the ServiceAccount, ClusterRole, and ClusterRoleBinding (with safe
carveouts — no secrets, no exec, no RBAC tampering) and apply them:

```bash
claude-forge kube render --context dev | kubectl apply -f -
```

## How It Works

When you run `claude-forge start`, it:

1. Detects the current project (git remote, directory)
2. Resolves authentication credentials
3. Creates a Docker network for the session
4. Starts a **gateway** container that proxies GitHub traffic with write restrictions
5. Starts a **GitHub MCP** sidecar scoped to the current repository
6. Starts an **agent** container running Claude Code with your project mounted at `/work`
7. Attaches your terminal (interactive) or waits for completion (with `-p`)

The gateway ensures Claude Code can freely read from any GitHub repository but can only push to or create PRs on the current project's repository.

## License

MIT
