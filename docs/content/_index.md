---
title: claude-forge
type: docs
---

# claude-forge

`claude-forge` launches Claude Code inside isolated Docker containers with a
secure gateway proxy for GitHub access. The gateway allows read operations
(clone, pull, fetch) to any repository but restricts push and write operations
to only the current project's repository.

```bash
go install github.com/michael-freling/claude-forge/cmd/claude-forge@latest
cd ~/my-project
claude-forge start "add auth tests"
```

[Get started →]({{< relref "/docs/getting-started" >}})

## Features

- Runs Claude Code in Docker with `--dangerously-skip-permissions` by default
- Gateway proxy mediates all GitHub traffic (git + API) with per-repo write restrictions
- Per-session **GitHub MCP** sidecar scoped to the current repository
- Optional shared **Kubernetes MCP** server for cluster access, gated by generated RBAC
- Custom **MCP servers** (remote http/sse or stdio) configurable per install
- Optional **Docker-in-Docker**: sessions get their own isolated Docker daemon,
  unable to see or touch host or other sessions' containers
- Multiple instances can run in parallel across different projects
- Named sessions with persistence — resume previous sessions by ID or name
- Automatic auth detection from `ANTHROPIC_API_KEY`, `CLAUDE_CODE_OAUTH_TOKEN`,
  or `~/.claude/.credentials.json`
- Dependency caching for Go, npm, pnpm, and pip
- Host git identity, Claude Code configuration, and plugins carried into the container
