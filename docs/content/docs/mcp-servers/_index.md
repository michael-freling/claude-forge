---
title: MCP Servers
weight: 4
---

Claude Code's tool integrations use the
[Model Context Protocol](https://modelcontextprotocol.io/) (MCP). In
claude-forge, every MCP server the agent can reach is registered at session
start — and only servers that are actually running are advertised, so the agent
never sees a dead endpoint.

Three kinds of servers are available:

- **`github` (built in, always on)** — a sidecar started for every session,
  scoped to the current repository via its launch flags. GitHub operations
  work out of the box, and the server cannot touch other repos.
- **`kubernetes` (built in, optional)** — a single shared server for cluster
  access, enabled in `config.yaml` and constrained by RBAC you generate.
  See [Kubernetes]({{< relref "kubernetes" >}}).
- **Custom servers** — any remote endpoint, in-container command, or container
  sidecar you declare in `config.yaml`.
  See [Custom Servers]({{< relref "custom" >}}).

A ready-made **[Google Cloud]({{< relref "gcp" >}})** server (read-only,
IAM-gated, multi-project) ships in this repo and is added as a custom
`container` server — secret payloads are blocked by default.

{{< cards >}}
  {{< card link="custom" title="Custom Servers" icon="puzzle" subtitle="Remote, stdio, and container sidecar servers, env expansion, and OAuth." >}}
  {{< card link="kubernetes" title="Kubernetes" icon="cube" subtitle="RBAC-gated cluster access via the shared Kubernetes MCP server." >}}
  {{< card link="gcp" title="Google Cloud" icon="cloud" subtitle="Read-only, IAM-gated, multi-project Google Cloud access; added as a custom container server." >}}
{{< /cards >}}
