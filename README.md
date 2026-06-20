# claude-forge

`claude-forge` launches Claude Code inside isolated Docker containers with a secure gateway proxy for GitHub access. The gateway allows read operations (clone, pull, fetch) to any repository but restricts push and write operations to only the current project's repository.

> **Note:** This repository was previously named `claude-code-tools`. The
> standalone `claude-hooks` and `update-ci-secrets` tools now live in
> [`michael-freling/claude-code-tools`](https://github.com/michael-freling/claude-code-tools).

## Features

- Runs Claude Code in Docker with `--dangerously-skip-permissions` by default
- Gateway proxy mediates all GitHub traffic (git + API) with per-repo write restrictions
- Multiple instances can run in parallel across different projects
- Session persistence — resume previous sessions by ID
- Automatic auth detection from `ANTHROPIC_API_KEY`, `CLAUDE_CODE_OAUTH_TOKEN`, or `~/.claude/.credentials.json`
- Dependency caching for Go, npm, pnpm, and pip
- Host git identity and Claude Code configuration carried into the container

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

# Start an interactive session
claude-forge start

# Start with a prompt (non-interactive, exits when done)
claude-forge start -p "Add unit tests for the auth package"
```

On first run, `claude-forge build` is called automatically to pull the agent and gateway Docker images.

## Usage

### Start a Session

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

### Resume a Session

```bash
# Resume the most recent session
claude-forge resume

# List available sessions
claude-forge resume --list

# Resume a specific session by ID
claude-forge resume <session-id>
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
# Pull/rebuild Docker images
claude-forge build

# Verify authentication credentials
claude-forge auth

# Show version
claude-forge version
```

## Configuration

Configuration is stored in `~/.config/claude-forge/config.yaml`:

```yaml
# Override Docker images (defaults to GHCR.io images)
images:
  agent: ghcr.io/michael-freling/claude-forge-agent:latest
  gateway: ghcr.io/michael-freling/claude-forge-gateway:latest

# Default flags
defaults:
  skip_permissions: true
  worktree: false
```

## Authentication

`claude-forge` resolves credentials in this order:

1. `ANTHROPIC_API_KEY` environment variable
2. `CLAUDE_CODE_OAUTH_TOKEN` environment variable
3. `~/.claude/.credentials.json` (written by `claude` CLI login)

Run `claude-forge auth` to verify your credentials are detected.

## How It Works

When you run `claude-forge start`, it:

1. Detects the current project (git remote, directory)
2. Resolves authentication credentials
3. Creates a Docker network for the session
4. Starts a **gateway** container that proxies GitHub traffic with write restrictions
5. Starts an **agent** container running Claude Code with your project mounted at `/work`
6. Attaches your terminal (interactive) or waits for completion (with `-p`)

The gateway ensures Claude Code can freely read from any GitHub repository but can only push to or create PRs on the current project's repository.

## License

MIT
