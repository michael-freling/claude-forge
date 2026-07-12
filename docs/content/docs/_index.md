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
  {{< card link="configuration" title="Configuration" icon="adjustments" subtitle="The config.yaml reference, plus how credentials are detected." >}}
  {{< card link="mcp-servers" title="MCP Servers" icon="puzzle" subtitle="The built-in GitHub and Kubernetes servers, and how to add your own." >}}
  {{< card link="how-it-works" title="How It Works" icon="cog" subtitle="The containers behind a session and how they connect." >}}
{{< /cards >}}
