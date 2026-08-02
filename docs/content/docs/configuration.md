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
  # ServiceAccount token lifetime to request (Go duration, default 24h).
  # Tokens are re-minted at session start and refreshed while sessions run;
  # the API server may cap the granted lifetime.
  token_duration: 24h
  default_context: dev
  contexts:
    - host_context: dev
      service_account_name: claude-forge-agent
      service_account_namespace: default
```

See [Custom Servers]({{< relref "/docs/mcp-servers/custom" >}}) for the full
`mcp_servers` reference, [Kubernetes]({{< relref "/docs/mcp-servers/kubernetes" >}})
for the `kubernetes` section, and [Google Cloud]({{< relref "/docs/mcp-servers/gcp" >}})
for the read-only Google Cloud MCP server (configured under `mcp_servers`).

## Authentication

Claude Code credentials are not part of `config.yaml` — `claude-forge` detects
them automatically, in this order:

1. `ANTHROPIC_API_KEY` environment variable
2. `CLAUDE_CODE_OAUTH_TOKEN` environment variable
3. `~/.claude/.credentials.json` (written by `claude` CLI login)

Run `claude-forge auth` to verify your credentials are detected.
