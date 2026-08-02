# claude-forge

`claude-forge` launches Claude Code inside isolated Docker containers with a secure gateway proxy for GitHub access. The gateway allows read operations (clone, pull, fetch) to any repository but restricts push and write operations to only the current project's repository.

**📖 Full documentation: <https://michael-freling.github.io/claude-forge/>**

## Features

- Runs Claude Code in Docker with `--dangerously-skip-permissions` by default
- Gateway proxy mediates all GitHub traffic (git + API) with per-repo write restrictions
- Per-session **GitHub MCP** sidecar scoped to the current repository
- Optional shared **Kubernetes MCP** server for cluster access, gated by generated RBAC
- Optional shared **Google Cloud MCP** server (read-only, IAM-gated; secret payloads blocked by default)
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

## Documentation

The full user guide lives on the [documentation site](https://michael-freling.github.io/claude-forge/):

- [Getting Started](https://michael-freling.github.io/claude-forge/docs/getting-started/) — install and run your first session
- [Usage](https://michael-freling.github.io/claude-forge/docs/usage/) — the full CLI command reference
- [Configuration](https://michael-freling.github.io/claude-forge/docs/configuration/) — the `config.yaml` reference and credential detection
- [MCP Servers](https://michael-freling.github.io/claude-forge/docs/mcp-servers/) — the built-in GitHub and Kubernetes servers, and adding custom ones
- [How It Works](https://michael-freling.github.io/claude-forge/docs/how-it-works/) — what happens when a session starts

## License

MIT
