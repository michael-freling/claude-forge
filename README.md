# claude-forge

`claude-forge` launches Claude Code inside isolated Docker containers with a secure gateway proxy for GitHub access. The gateway allows read operations (clone, pull, fetch) to any repository but restricts push and write operations to only the current project's repository.

## Features

- Runs Claude Code in Docker with `--dangerously-skip-permissions` by default
- Gateway proxy mediates all GitHub traffic (git + API) with per-repo write restrictions
- Per-session **GitHub MCP** sidecar scoped to the current repository
- Optional shared **Kubernetes MCP** server for cluster access, gated by generated RBAC
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
