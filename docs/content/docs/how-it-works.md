---
title: How It Works
weight: 7
---

# How It Works

When you run `claude-forge start`, it:

1. Detects the current project (git remote, directory)
2. Resolves authentication credentials
3. Creates a Docker network for the session
4. Starts a **gateway** container that proxies GitHub traffic with write restrictions
5. Starts a **GitHub MCP** sidecar scoped to the current repository
6. Starts an **agent** container running Claude Code with your project mounted at `/work`
7. Attaches your terminal (interactive) or waits for completion (with `-p`)

The gateway ensures Claude Code can freely read from any GitHub repository but
can only push to or create PRs on the current project's repository.
