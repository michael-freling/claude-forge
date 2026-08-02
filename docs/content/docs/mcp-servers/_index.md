---
title: MCP Servers
weight: 4
---

Claude Code's tool integrations use the
[Model Context Protocol](https://modelcontextprotocol.io/) (MCP). In
claude-forge, every MCP server the agent can reach is registered at session
start — and only servers that are actually running are advertised, so the agent
never sees a dead endpoint.

Two kinds of servers are available:

- **`github` (built in, always on)** — a sidecar started for every session,
  scoped to the current repository via its launch flags. GitHub operations
  work out of the box, and the server cannot touch other repos.
- **Custom servers** — any remote endpoint, in-container command, or container
  sidecar you declare in `config.yaml`.
  See [Custom Servers]({{< relref "custom" >}}).

Two ready-made servers ship in this repo and are added as custom `container`
servers: **[Kubernetes]({{< relref "kubernetes" >}})** (read/write cluster
access with policy-enforced carveouts — no secrets, no exec, no RBAC) and
**[Google Cloud]({{< relref "gcp" >}})** (read-only, IAM-gated,
multi-project — secret payloads are blocked by default).

{{< cards >}}
  {{< card link="custom" title="Custom Servers" icon="puzzle" subtitle="Remote, stdio, and container sidecar servers, env expansion, and OAuth." >}}
  {{< card link="kubernetes" title="Kubernetes" icon="cube" subtitle="First-party server: kubeconfig-authenticated cluster access with policy-enforced carveouts." >}}
  {{< card link="gcp" title="Google Cloud" icon="cloud" subtitle="Read-only, IAM-gated, multi-project Google Cloud access; added as a custom container server." >}}
{{< /cards >}}
