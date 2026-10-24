---
title: Configuration
weight: 3
---


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
  # Claude Code main-thread agent for every session (claude --agent).
  # Empty or omitted uses Claude Code's default; start/resume --agent overrides.
  agent: my-plugin:orchestrator

# Optional Docker-in-Docker: give sessions their own isolated Docker daemon.
# The agent can build/run/stop its own containers, but cannot see or control
# the host's containers, claude-forge's containers, or other sessions'
# containers (the host Docker socket is never mounted). Requires running the
# agent container with --privileged, so it is disabled by default.
docker:
  enabled: false

# Custom MCP servers (in addition to the built-in github server)
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
  # global (one shared instance reused across all sessions). Wrapped stdio
  # commands are bridged to HTTP automatically.
  - name: my-container-mcp
    type: container
    command: npx           # wrapped stdio (bridged to HTTP)
    args: ["-y", "some-mcp-server"]
    mounts:
      - "~/.config/some-tool:/home/node/.config/some-tool:ro"
  # The read-only Google Cloud MCP server (a native HTTP image shipped in this
  # repo). See the Google Cloud page for the full setup.
  - name: gcp
    type: container
    scope: global
    image: ghcr.io/michael-freling/claude-forge-gcp-mcp:latest
    port: 8084
    path: /mcp
    args: ["--project", "my-project"]     # optional default; omit for multi-project
    env:
      HOME: /home/user
      CLOUDSDK_CONFIG: /home/user/.config/gcloud
    mounts:
      - "~/.config/gcloud:/home/user/.config/gcloud:ro"
  # The first-party Kubernetes MCP server (a native HTTP image shipped in this
  # repo). See the Kubernetes page for the full setup.
  - name: kube
    type: container
    scope: global
    image: ghcr.io/michael-freling/claude-forge-k8s-mcp:latest
    port: 8080
    path: /mcp
    args: ["--addr=:8080", "--kubeconfig=/home/user/.kube/config"]
    env:
      HOME: /home/user
    mounts:
      - "~/.kube:/home/user/.kube:ro"
  # Per-session (isolated) native HTTP image
  - name: my-sidecar
    type: container
    scope: session
    image: ghcr.io/example/some-mcp:latest
    port: 8080
    path: /mcp
```

See [Custom Servers]({{< relref "/docs/mcp-servers/custom" >}}) for the full
`mcp_servers` reference, [Kubernetes]({{< relref "/docs/mcp-servers/kubernetes" >}})
for the first-party Kubernetes MCP server, and
[Google Cloud]({{< relref "/docs/mcp-servers/gcp" >}}) for the read-only Google
Cloud MCP server (both configured under `mcp_servers`).

## Main-thread agent

`defaults.agent` names the Claude Code agent that runs the main thread of every
session — for example a plugin agent such as `my-plugin:orchestrator` that has
restricted tools and only delegates to subagents. claude-forge passes it to
Claude Code as `claude --agent <name>` on fresh, resumed, and continued
sessions.

- `--agent <name>` on `start` or `resume` overrides `defaults.agent` for that
  run. Without the flag, `resume` uses `defaults.agent` (not the agent the
  original session ran with).
- The agent must be available inside the container: a plugin agent from a
  plugin installed on the host (host plugins are synced at session start), a
  user agent under `~/.claude/agents/`, or a project agent under
  `.claude/agents/` in the repository.
- The name must be a single token: no whitespace or control characters, and
  it must not start with `-`. An invalid value is rejected before any
  container starts.

Use `defaults.agent` rather than editing `~/.config/claude-forge/settings.json`:
that file is managed by claude-forge (see below).

## Generated files

Alongside `config.yaml`, `claude-forge` writes these into
`~/.config/claude-forge/` and mounts them into the session:

| File | Mounted at | Regenerated each session |
|---|---|---|
| `settings.json` | `/home/user/.claude/settings.json` | MCP server list only |
| `.claude.json` | `/home/user/.claude.json` | MCP server list only |
| `gitconfig` | `/home/user/.gitconfig` | yes |
| `container-CLAUDE.md` | `/etc/claude-code/CLAUDE.md` | yes |

`container-CLAUDE.md` holds the instructions that tell the session it runs in a
container, how to name paths relative to `/home/user/work`, and how to reach
GitHub through the gateway; see
[How It Works]({{< relref "/docs/how-it-works" >}}#container-instructions).
Because it is regenerated, put your own instructions in the project's
`CLAUDE.md` or in `~/.claude/rules/` on the host instead.

## Authentication

Claude Code credentials are not part of `config.yaml` — `claude-forge` detects
them automatically, in this order:

1. `ANTHROPIC_API_KEY` environment variable
2. `CLAUDE_CODE_OAUTH_TOKEN` environment variable
3. `~/.claude/.credentials.json` (written by `claude` CLI login)

An environment-variable token is always passed to the agent, even when
`~/.claude/.credentials.json` exists — for example because you authenticated a
remote MCP server, which stores its tokens in that file. The file is still
mounted so those MCP tokens remain available.

Run `claude-forge auth` to verify your credentials are detected.
