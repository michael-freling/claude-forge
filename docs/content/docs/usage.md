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

Session age is measured by **last activity** (the transcript file's
modification time), so a session started long ago but resumed recently is not
pruned.

```bash
# Delete sessions inactive for 30+ days (default)
claude-forge prune

# Delete sessions inactive for a custom duration
claude-forge prune --older-than 7d

# Keep the 10 most recently active sessions, delete all the rest
# (used alone, --keep replaces the 30-day default instead of combining with it)
claude-forge prune --keep 10

# Combine both: only delete sessions that are inactive for 7+ days
# AND not among the 10 most recently active
claude-forge prune --older-than 7d --keep 10

# Preview without deleting
claude-forge prune --dry-run
```

Prune also cleans up orphaned session-name sidecar files whose transcript no
longer exists. Sidecars written within the last hour are left alone: a
just-started session records its name before its transcript exists.

Git worktrees created by `start --worktree` are never removed automatically —
they may contain uncommitted work. When pruning deletes the last session that
referenced a worktree, prune prints a hint to remove it yourself:

```bash
git worktree remove .claude-worktrees/<name>
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
