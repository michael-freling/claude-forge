---
title: Usage
weight: 2
---


## Start a Session

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

## List and Resume Sessions

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

## Prune Old Sessions

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

## Manage Containers

```bash
# Show running containers
claude-forge status

# Stop containers for the current project
claude-forge stop
```

## Other Commands

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
