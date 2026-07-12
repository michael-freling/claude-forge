---
title: Getting Started
weight: 1
---

# Getting Started

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

On first run, `claude-forge build` is called automatically to pull the agent,
gateway, and GitHub MCP Docker images.

An interactive session drops you into the normal Claude Code UI — the only
difference is that it runs inside the container, sees your project at `/work`,
and reaches GitHub through the gateway.

## Next Steps

- [Usage]({{< relref "/docs/usage" >}}) — the full CLI command reference
- [Configuration]({{< relref "/docs/configuration" >}}) — customize images,
  defaults, MCP servers, and more
- [Authentication]({{< relref "/docs/authentication" >}}) — how credentials
  are detected
