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

## TODOs Across Projects

`claude-forge todos` gathers your TODOs across every project on the machine so
you can recall what you were working on and pick a session to resume. Run it
from anywhere. Output is grouped by project (most recently active first), and
each project shows two kinds of list:

- A **shared backlog** — a TODO list you curate, one per project, that you edit
  from the host and that Claude Code can read and update inside every session
  for that project.
- **Session todos** — the checklists Claude Code manages within each session,
  shown read-only so you can tell which session to jump back into.

```bash
# Show everything with open items across all projects, grouped by project
claude-forge todos

# Include lists whose items are all completed
claude-forge todos --all

# Only the current project
claude-forge todos --project
```

```
-home-user-my-project
  backlog · updated 2h ago
    [ ] 1. Decide on the token refresh strategy
    [x] 2. Spike the gateway client
  add auth tests · updated 3h ago
    [x] Add unit tests for token refresh
    [~] Wire retry logic into the gateway client
    [ ] Add integration test for login redirect
```

Checklist markers are `[ ]` open, `[~]` in progress (session todos only), and
`[x]` done.

### Editing the shared backlog

The backlog commands act on the **current** project (the git repo you are in).
The numbers match those shown by `claude-forge todos`.

```bash
# Add an item
claude-forge todos add "Decide on the token refresh strategy"

# Check one off (or reopen it)
claude-forge todos done 1
claude-forge todos done 1 --reopen

# Freeform edit in $EDITOR
claude-forge todos edit
```

The backlog lives at `~/.claude-forge/<project-id>/TODO.md` and is mounted into
each of that project's sessions at `~/TODO.md`. It belongs to the project, so
it is **not** removed when sessions are pruned. Per-session todo state persists
alongside it under the same project directory and reappears when a session is
resumed.

### Session tags

When Claude Code adds an item from inside a session, it tags the item with that
session's name — `- [ ] fix the login bug (@fix-auth)` — and `claude-forge
todos` shows the `(@name)` next to it, so you can tell which session an item
came from. Items you add from the host (`todos add`) are untagged. The session
name is provided to the agent as `FORGE_SESSION_NAME`, and a comment at the top
of each `TODO.md` reminds the agent of the convention. This is a convention the
agent follows, not an enforced guarantee — a hand-written `(@name)` tag renders
the same way.

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

Pruning a session also deletes its TODO lists. Prune additionally cleans up
orphaned session-name sidecar files and TODO lists whose transcript no longer
exists. Files written within the last hour are left alone: a just-started
session records its name before its transcript exists.

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
