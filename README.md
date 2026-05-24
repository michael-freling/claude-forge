# Claude Code Tools

A collection of CLI tools for working with Claude Code.

## claude-forge

`claude-forge` launches Claude Code inside isolated Docker containers with a secure gateway proxy for GitHub access. The gateway allows read operations (clone, pull, fetch) to any repository but restricts push and write operations to only the current project's repository.

### Features

- Runs Claude Code in Docker with `--dangerously-skip-permissions` by default
- Gateway proxy mediates all GitHub traffic (git + API) with per-repo write restrictions
- Built-in GitHub MCP server for issue/PR operations from inside the sandbox
- Custom MCP servers — add your own MCP servers as sidecar containers or external URLs
- Project-level configuration via `.claude-forge.yaml`
- Multiple instances can run in parallel across different projects
- Session persistence — resume previous sessions by ID
- Automatic auth detection from `ANTHROPIC_API_KEY`, `CLAUDE_CODE_OAUTH_TOKEN`, or `~/.claude/.credentials.json`
- Dependency caching for Go, npm, pnpm, and pip
- Host git identity and Claude Code configuration carried into the container
- Optional Kubernetes MCP integration for cluster access

### Prerequisites

- Docker (daemon must be running)
- Go 1.25+ (to build from source)
- A Claude Code subscription or API key

### Installation

```bash
go install github.com/michael-freling/claude-code-tools/cmd/claude-forge@latest
```

Or build from source:

```bash
git clone https://github.com/michael-freling/claude-code-tools.git
cd claude-code-tools
go build -o claude-forge ./cmd/claude-forge/
```

### Quick Start

```bash
# Navigate to your project directory
cd ~/my-project

# Start an interactive session
claude-forge start

# Start with a prompt (non-interactive, exits when done)
claude-forge start -p "Add unit tests for the auth package"
```

On first run, `claude-forge build` is called automatically to pull the agent and gateway Docker images.

### Usage

#### Start a Session

```bash
# Interactive session (attaches to container TTY)
claude-forge start

# Non-interactive with a prompt
claude-forge start -p "Fix the flaky test in auth_test.go"

# With worktree mode
claude-forge start --worktree

# Without --dangerously-skip-permissions
claude-forge start --no-skip-permissions
```

#### Resume a Session

```bash
# Resume the most recent session
claude-forge resume

# List available sessions
claude-forge resume --list

# Resume a specific session by ID
claude-forge resume <session-id>
```

#### Manage Containers

```bash
# Show running containers
claude-forge status

# Stop containers for the current project
claude-forge stop
```

#### Other Commands

```bash
# Pull/rebuild Docker images
claude-forge build

# Verify authentication credentials
claude-forge auth

# Show version
claude-forge version
```

### Configuration

#### Global Config

`~/.config/claude-forge/config.yaml` — applies to all projects:

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

# Custom MCP servers
mcp_servers:
  # Container-based: forge runs this as a sidecar
  linear:
    image: ghcr.io/org/linear-mcp:latest
    port: 8080
    path: /mcp                            # optional, default: /mcp
    env:
      LINEAR_API_KEY: ${LINEAR_API_KEY}   # expanded from host env at runtime
    cmd: ["--workspace", "my-team"]       # optional
    mounts:                               # optional
      - source: ~/.config/linear-mcp
        target: /config
        read_only: true

  # Shared singleton: one instance across all sessions
  sentry:
    image: ghcr.io/org/sentry-mcp:latest
    port: 9090
    shared: true
    env:
      SENTRY_DSN: ${SENTRY_DSN}

  # URL-only: no container managed, just register the URL
  internal-api:
    url: http://host.docker.internal:3000/mcp

# Kubernetes MCP integration (optional)
kubernetes:
  enabled: false
  read_only: false
  image: ghcr.io/containers/kubernetes-mcp-server:latest
  contexts:
    - host_context: my-cluster
      service_account_name: claude-forge-agent
      service_account_namespace: default
  default_context: my-cluster
```

#### Project Config

`.claude-forge.yaml` in the project root — overrides global config per project. Safe to commit since secrets use `${VAR}` placeholders.

```yaml
mcp_servers:
  # Override a global MCP for this project
  linear:
    image: ghcr.io/org/linear-mcp:latest
    port: 8080
    env:
      LINEAR_API_KEY: ${LINEAR_API_KEY}
      LINEAR_TEAM_ID: ${MY_PROJECT_TEAM_ID}

  # Add a project-specific MCP
  my-db:
    image: ghcr.io/org/db-mcp:latest
    port: 5432
    env:
      DATABASE_URL: ${DATABASE_URL}

  # Disable a global MCP for this project
  sentry:
    enabled: false
```

Project entries take priority over global entries with the same name. Entries with `enabled: false` disable a globally-defined MCP for that project.

#### Custom MCP Server Types

| Type | Config | Lifecycle |
|------|--------|-----------|
| **Per-session** (default) | `image` + `port` | Created per session, cleaned up on stop |
| **Shared** | `image` + `port` + `shared: true` | Singleton, survives session cleanup |
| **URL-only** | `url` | No container — registers an external URL |

Container-based MCPs communicate with the agent over the Docker bridge network using streamable HTTP. The agent reaches them at `http://{name}:{port}{path}`.

For URL-only MCPs, the URL must be reachable from inside the Docker container (e.g., `http://host.docker.internal:<port>/mcp` for services on the host).

### Authentication

`claude-forge` resolves credentials in this order:

1. `ANTHROPIC_API_KEY` environment variable
2. `CLAUDE_CODE_OAUTH_TOKEN` environment variable
3. `~/.claude/.credentials.json` (written by `claude` CLI login)

Run `claude-forge auth` to verify your credentials are detected.

### How It Works

When you run `claude-forge start`, it:

1. Detects the current project (git remote, directory)
2. Resolves authentication credentials
3. Loads global config (`~/.config/claude-forge/config.yaml`) and project config (`.claude-forge.yaml`)
4. Creates a Docker network for the session
5. Starts a **gateway** container that proxies GitHub traffic with write restrictions
6. Starts the **GitHub MCP** sidecar for issue/PR operations
7. Starts any **custom MCP** containers defined in config
8. Starts the **agent** container running Claude Code with your project mounted at `/work`
9. Attaches your terminal (interactive) or waits for completion (with `-p`)

The gateway ensures Claude Code can freely read from any GitHub repository but can only push to or create PRs on the current project's repository.

### Plugins

Host Claude Code plugins can be synced into the forge environment:

```bash
# Sync host plugins into forge's plugin directory
claude-forge plugins sync
```

Plugins persist in `~/.claude-forge/plugins/` across sessions.

### Kubernetes Integration

To give Claude Code access to Kubernetes clusters:

```bash
# Generate RBAC manifests for a cluster
claude-forge kube render --context my-cluster | kubectl apply -f -
```

Then enable Kubernetes in your config and restart. See the `kubernetes` section in the global config example above.

### Extra Mounts

Mount additional host directories into the container:

```bash
claude-forge start --mount /path/on/host:/path/in/container
```

## License

MIT
