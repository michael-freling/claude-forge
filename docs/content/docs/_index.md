---
linkTitle: Documentation
title: Introduction
---

## What is claude-forge?

`claude-forge` is a CLI that runs [Claude Code](https://claude.com/claude-code)
inside an isolated Docker container, so the agent can work autonomously without
access to your host machine.

Claude Code is most productive when it can act without asking permission for
every step (`--dangerously-skip-permissions`) — but granting that on your host
means an autonomous agent with your credentials and your filesystem.
claude-forge resolves that tension with containers:

- The agent only sees the **project you mount** at `/work`, never your host
  filesystem, host Docker daemon, or unrelated credentials.
- All git and GitHub API traffic flows through a **gateway proxy** that lets
  the agent read any repository but write only to the current project's — a
  compromised or confused agent cannot push to your other repos.
- Each run is a **named session** you can list, resume, and prune, and multiple
  sessions run in parallel across projects.

Tool integrations use [MCP](https://modelcontextprotocol.io/) (Model Context
Protocol), the standard way to give Claude Code extra capabilities: a GitHub
MCP server scoped to your repository comes built in, Kubernetes access is
available behind RBAC you control, and you can add your own servers.

## Next steps

{{< cards >}}
  {{< card link="getting-started" title="Getting Started" icon="play" subtitle="Install and run your first session in two commands." >}}
  {{< card link="usage" title="Usage" icon="terminal" subtitle="The full CLI command reference." >}}
  {{< card link="configuration" title="Configuration" icon="adjustments" subtitle="The config.yaml reference: images, defaults, Docker-in-Docker." >}}
  {{< card link="mcp-servers" title="Custom MCP Servers" icon="puzzle" subtitle="Expose additional MCP servers to the agent." >}}
  {{< card link="kubernetes" title="Kubernetes Access" icon="cube" subtitle="RBAC-gated cluster access for the agent." >}}
  {{< card link="authentication" title="Authentication" icon="key" subtitle="How credentials are detected." >}}
  {{< card link="how-it-works" title="How It Works" icon="cog" subtitle="The containers behind a session and how they connect." >}}
{{< /cards >}}
