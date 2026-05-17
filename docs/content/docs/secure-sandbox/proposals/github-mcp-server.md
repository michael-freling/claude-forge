---
title: "Proposal: GitHub MCP Server for claude-forge"
weight: 2
---

# Proposal: GitHub MCP Server for claude-forge

**Status**: Draft
**Depends on**: [Secure Sandbox Architecture]({{< relref "/docs/secure-sandbox/architecture/secure-sandbox-architecture" >}})

## 1. Motivation

Today, the agent container interacts with GitHub through two mechanisms:

1. **Git transport** (push/pull/fetch): The agent's gitconfig rewrites `https://github.com/` to `http://gateway:8080/github.com/`, and the gateway's HTTP proxy (`internal/gateway/proxy.go`) injects the credential and forwards to GitHub. This is transparent to git and works well.

2. **GitHub API** (PRs, issues, checks): A custom `forge-gh` binary in the agent container emulates a subset of `gh` CLI syntax. It parses CLI args, fetches a schema from the gateway's REST API server (`:8083`), maps commands to operations, and the gateway proxies to `api.github.com` with authentication.

The `forge-gh` approach has several problems:

- **Limited coverage**: Only 14 operations are wired up. Every new GitHub API operation requires changes to three places: the operation list in `api.go`, the command-to-operation mapping in `forgegh/client.go`, and the request body builder.
- **Fragile CLI emulation**: `forge-gh` parses `gh`-style arguments through manual string matching. It doesn't support `gh api` (raw API calls), JSON output formatting (`--json`), or many flags that the real `gh` supports.
- **Discovery friction**: Claude Code must use `gh`-style commands that may not match what it was trained on. The schema endpoint provides some discoverability, but Claude can't introspect available operations the way it can with MCP tools.
- **No structured responses**: Results come back as printed JSON to stdout. Claude parses this as text rather than receiving structured tool results.

Claude Code has native MCP (Model Context Protocol) support. Exposing GitHub operations as MCP tools would let Claude discover, invoke, and receive structured results from GitHub operations without any wrapper binary.

## 2. Goals and Non-Goals

### Goals

- Replace the `forge-gh` REST API with a dedicated GitHub MCP sidecar container.
- Claude Code discovers available GitHub tools via MCP protocol at startup.
- All current `forge-gh` operations continue to work via MCP tools.
- Additional operations (labels, reviews, review comments, workflow dispatch, file contents, branch management) become trivial to add — just define the tool schema.
- Policy enforcement (owner/repo allowlist, read vs. write) is preserved.
- The git HTTP proxy remains unchanged — MCP replaces only the API layer.
- The agent container no longer needs the `forge-gh` binary.
- The GitHub MCP server runs as a per-session sidecar, scoped to one repo.

### Non-Goals

- Replacing the git transport proxy. Git push/pull/fetch works well over the HTTP proxy and doesn't benefit from MCP.
- Building a general-purpose GitHub MCP server. This is scoped to the session's allowlisted repo.
- Supporting GitHub Enterprise Server in v1 (though the architecture doesn't preclude it).
- Shared/singleton deployment of the GitHub MCP server (it must be per-session for repo scoping).

## 3. Architecture

### 3.1 Sidecar vs Shared Service Model

This proposal introduces a key architectural distinction for services that support the agent:

| Type | Scope | Lifecycle | Example |
|---|---|---|---|
| **Sidecar** (per-session) | Scoped to one repo/project | Created and destroyed with the agent session | GitHub MCP, git proxy |
| **Shared** (singleton) | Same state regardless of project | Started on first session, stopped when last session exits | K8s MCP (future) |

The GitHub MCP server is a **sidecar** because:
- It's configured with a specific `owner/repo` allowlist per session
- Write operations must be restricted to the session's project
- Multiple agents working on different repos need independent policy enforcement
- Credentials may differ per repo (e.g., fine-grained PATs)

### 3.2 Network Topology

Multiple agents can run in parallel on the same host, each with their own sidecars, plus shared services accessible to all:

```
┌─ forge-shared network ─────────────────────────────────────────────────────┐
│                                                                            │
│  k8s-mcp (shared, future)                                                  │
│  :8090                                                                     │
│                                                                            │
├────────────────────────────────────────────────────────────────────────────┤
│                                                                            │
│  ┌── Session A (owner-x/repo-a) ── forge_net_repoa_abc ──────────────┐    │
│  │                                                                    │    │
│  │  agent-a ─────► gateway-a:8080     (git proxy, scoped repo-a)      │    │
│  │           ────► github-mcp-a:8083  (GitHub MCP, scoped repo-a)     │────┤
│  │                                                                    │    │
│  └────────────────────────────────────────────────────────────────────┘    │
│                                                                            │
│  ┌── Session B (owner-y/repo-b) ── forge_net_repob_def ──────────────┐    │
│  │                                                                    │    │
│  │  agent-b ─────► gateway-b:8080     (git proxy, scoped repo-b)      │    │
│  │           ────► github-mcp-b:8083  (GitHub MCP, scoped repo-b)     │────┤
│  │                                                                    │    │
│  └────────────────────────────────────────────────────────────────────┘    │
│                                                                            │
└────────────────────────────────────────────────────────────────────────────┘
```

Key properties:
- Each session has its own Docker network. Sidecars live on this network — containers in Session A cannot reach Session B's sidecars.
- Shared services live on a persistent `forge-shared` network. Each session network connects to it via Docker's multi-network support (`docker network connect`).
- DNS names are scoped: `github-mcp` resolves within the session network; `k8s-mcp` resolves via the shared network.

### 3.3 Docker Multi-Network (no orchestrator required)

Docker natively supports connecting a container to multiple networks:

```bash
# Shared network (created once, persists across sessions)
docker network create forge-shared

# Per-session network
docker network create forge_net_repoa_abc

# Start sidecar on session network only
docker run --network forge_net_repoa_abc --name github-mcp-a ...

# Start agent on session network, then connect to shared
docker run --network forge_net_repoa_abc --name agent-a ...
docker network connect forge-shared agent-a
```

No Kubernetes or Docker Compose required. The orchestrator manages this via the Docker API it already uses.

### 3.4 Container Separation

The GitHub MCP server runs as its own container, separate from the git proxy gateway:

| Container | Port | Responsibility |
|---|---|---|
| `gateway` | `:8080` | Git HTTP proxy (push/pull/fetch) + SSH forwarding |
| `github-mcp` | `:8083` | GitHub API via MCP protocol |

This separation means:
- A crash in the MCP server doesn't kill git operations.
- The MCP server image can be updated independently.
- The gateway stays focused on git transport (its original purpose).
- Future MCP servers (K8s, secrets, etc.) follow the same pattern.

### 3.5 MCP Transport

The MCP server uses **Streamable HTTP transport** (the standard for remote MCP servers). It exposes a single endpoint at `http://github-mcp:8083/mcp`. Claude Code connects to it as a remote MCP server configured in the agent's settings.

### 3.6 Agent Configuration

At container startup, `claude-forge` writes the MCP server configuration into the agent's Claude settings:

```json
{
  "mcpServers": {
    "github": {
      "type": "url",
      "url": "http://github-mcp:8083/mcp"
    }
  }
}
```

Claude Code discovers the available tools via the MCP `tools/list` method when it starts. Additional shared MCP servers are added to the same config:

```json
{
  "mcpServers": {
    "github": {
      "type": "url",
      "url": "http://github-mcp:8083/mcp"
    },
    "kubernetes": {
      "type": "url",
      "url": "http://k8s-mcp:8090/mcp"
    }
  }
}
```

## 4. Tool Definitions

Each GitHub operation becomes an MCP tool with a typed JSON Schema for inputs and structured JSON outputs:

| Tool Name | Description | Inputs |
|---|---|---|
| `github_pr_list` | List pull requests | `state?`, `per_page?`, `sort?`, `direction?` |
| `github_pr_get` | Get a pull request | `number` |
| `github_pr_create` | Create a pull request | `title`, `body?`, `head`, `base` |
| `github_pr_update` | Update a pull request | `number`, `title?`, `body?`, `state?`, `base?` |
| `github_pr_merge` | Merge a pull request | `number`, `merge_method?`, `commit_title?` |
| `github_pr_comment` | Comment on a PR | `number`, `body` |
| `github_pr_reviews` | List PR reviews | `number` |
| `github_issue_list` | List issues | `state?`, `per_page?`, `labels?` |
| `github_issue_get` | Get an issue | `number` |
| `github_issue_create` | Create an issue | `title`, `body?`, `labels?` |
| `github_issue_comment` | Comment on an issue | `number`, `body` |
| `github_repo_get` | Get repository info | (none) |
| `github_release_list` | List releases | `per_page?` |
| `github_checks_list` | List check runs | `ref` |
| `github_api` | Raw GitHub API call | `method`, `path`, `body?` |

The `github_api` tool is a catch-all that allows Claude to call any GitHub API endpoint within the allowed repo, preserving the policy enforcement. This eliminates the need to pre-wire every possible operation.

## 5. Policy Enforcement

The MCP server enforces the same policy the REST API enforces today:

- **Owner/repo scope**: Write operations are restricted to the configured `AllowedOwner/AllowedRepo`. Read operations to other public repos are allowed.
- **Auth injection**: The MCP server adds `Authorization: Bearer <token>` to all upstream requests. The agent never sees the token.
- **No credential exposure**: MCP tool responses never include auth headers or tokens.

## 6. Advantages over forge-gh

| Dimension | forge-gh (current) | MCP server (proposed) |
|---|---|---|
| **Discoverability** | Agent must know `gh` CLI syntax | Claude sees typed tool schemas via MCP |
| **Coverage** | 14 hardcoded operations | Unlimited via `github_api` catch-all + typed shortcuts |
| **Adding operations** | 3 code changes (api.go, client.go, body builder) | 1 tool definition in the MCP handler |
| **Response format** | Unstructured JSON printed to stdout | Structured MCP tool results |
| **Agent binary** | Requires `forge-gh` binary in container | No extra binary needed |
| **Error handling** | Exit codes + stderr text | Structured MCP error responses with `isError` flag |
| **Streaming** | Not supported | MCP supports streaming for large responses |
| **Multi-repo reads** | Limited by hardcoded patterns | `github_api` allows reads to any public repo |
| **Isolation** | Shares process with git proxy | Independent container — crash/restart independent |

## 7. Implementation Plan

### 7.1 New Package: `internal/github-mcp/`

A new package implementing the GitHub MCP server (separate binary from the gateway):

| File | Purpose |
|---|---|
| `main.go` | Entrypoint, config loading, HTTP listener |
| `server.go` | MCP protocol handler (initialize, tools/list, tools/call) |
| `tools.go` | Tool definitions and JSON Schema generation |
| `github.go` | GitHub API execution with auth injection |
| `policy.go` | Owner/repo policy enforcement |

### 7.2 New Dockerfile: `docker/github-mcp/Dockerfile`

Minimal container running just the MCP server binary.

### 7.3 Changes to Existing Code

| File | Change |
|---|---|
| `internal/forge/orchestrator.go` | Start `github-mcp` sidecar container; write MCP config to agent settings |
| `internal/forge/container/client.go` | Add `StartGitHubMCP()` method (similar to `StartGateway()`) |
| `internal/forge/claudecode/settings.go` | Add MCP server config to settings generation |
| `internal/gateway/server.go` | Remove API server (`:8083` listener); gateway only runs git proxy |
| `internal/gateway/api.go` | Delete (replaced by MCP server) |
| `docker/agent/Dockerfile` | Remove `forge-gh` binary |
| `internal/forgegh/` | Delete package (after migration) |

### 7.4 Orchestrator Lifecycle Changes

The orchestrator gains awareness of two container classes:

```go
type ContainerClass string

const (
    ClassSidecar ContainerClass = "sidecar" // per-session, on session network
    ClassShared  ContainerClass = "shared"  // singleton, on forge-shared network
)
```

For sidecars (GitHub MCP):
- Created in `Orchestrator.Start()` alongside the gateway
- Cleaned up in `Orchestrator.Cleanup()` alongside the gateway

For shared services (future K8s MCP):
- Started on first session if not already running
- Connected to session network via `docker network connect`
- Stopped when last session exits (reference counting)

### 7.5 Migration Path

1. **Phase 1**: Add `github-mcp` container alongside existing REST API in gateway. Both `forge-gh` and MCP work simultaneously. Agent settings include MCP config.
2. **Phase 2**: Configure agent to use MCP exclusively. Verify all operations work. Keep `forge-gh` as fallback.
3. **Phase 3**: Remove `forge-gh` binary, REST API from gateway, and `internal/forgegh/` package. Gateway only does git proxy.

## 8. MCP Protocol Details

### 8.1 Tool Call Flow

```
Claude Code                    GitHub MCP Server               GitHub API
    │                               │                              │
    ├─ POST /mcp ──────────────────►│                              │
    │  {"method": "tools/call",     │                              │
    │   "params": {                 │                              │
    │     "name": "github_pr_list", │                              │
    │     "arguments": {            │                              │
    │       "state": "open"         │                              │
    │     }                         │                              │
    │   }}                          │                              │
    │                               ├─ GET /repos/o/r/pulls ──────►│
    │                               │   Authorization: Bearer xxx  │
    │                               │   state=open                 │
    │                               │                              │
    │                               │◄── 200 [{...}, {...}] ───────┤
    │                               │                              │
    │◄─ {"result": {"content": [    │                              │
    │     {"type": "text",          │                              │
    │      "text": "[{...}]"}       │                              │
    │   ]}} ────────────────────────┤                              │
```

### 8.2 Tool Schema Example

```json
{
  "name": "github_pr_create",
  "description": "Create a pull request in the project repository",
  "inputSchema": {
    "type": "object",
    "properties": {
      "title": { "type": "string", "description": "PR title" },
      "body": { "type": "string", "description": "PR description (markdown)" },
      "head": { "type": "string", "description": "Branch containing changes" },
      "base": { "type": "string", "description": "Branch to merge into (default: main)" }
    },
    "required": ["title", "head"]
  }
}
```

### 8.3 Error Handling

MCP tool errors are returned as structured responses:

```json
{
  "result": {
    "content": [{ "type": "text", "text": "GitHub API error: 422 - head branch does not exist" }],
    "isError": true
  }
}
```

## 9. Threat Model

The security model is identical to the current gateway architecture:

| # | Concern | Defense |
|---|---|---|
| 1 | Agent calls operations on repos outside allowlist | MCP policy layer enforces owner/repo on write operations |
| 2 | Agent tries to extract GitHub token via MCP | Token is never included in MCP responses; auth is injected server-side |
| 3 | Agent tries to call unauthorized endpoints | `github_api` catch-all still enforces owner/repo policy for writes |
| 4 | Prompt injection via MCP tool results | Same risk as current JSON responses — Claude Code's safety layers apply |
| 5 | Agent in Session A reaches Session B's MCP server | Docker network isolation — sidecars are only on their session's network |

## 10. Open Questions

1. **MCP library choice**: Use an existing Go MCP library (e.g., `mark3labs/mcp-go`) or implement the subset needed (initialize, tools/list, tools/call) directly? The protocol subset needed is small enough that a direct implementation avoids dependency risk.

2. **Pagination**: GitHub API responses can be paginated. Should the MCP server auto-paginate and return all results, or expose pagination parameters and return one page at a time? Recommendation: expose `per_page` and `page` params, let Claude decide when to paginate.

3. **Webhook events / notifications**: MCP supports server-initiated notifications. Could the MCP server push PR review events to Claude? Out of scope for v1 but the transport supports it.

4. **Shared network lifecycle**: When should the `forge-shared` network be created/destroyed? Options: (a) create on first `claude-forge start`, never destroy; (b) destroy when last session exits. Recommendation: option (a) — a lingering empty network costs nothing.

## 11. Rejected Alternatives

### 11.1 Install Real `gh` CLI in Container

Mount the host's gh config and install the real `gh` CLI. Rejected because:
- Violates Invariant 1 (agent has the credential directly).
- `gh` supports arbitrary API calls (`gh api`) with no policy enforcement.
- Token in container can be exfiltrated via prompt injection.

### 11.2 Keep forge-gh, Add More Operations

Extend the current forge-gh approach. Rejected because:
- Each new operation requires coordinated changes across 3 files.
- Claude doesn't get typed tool schemas — it must know gh CLI syntax.
- The REST API + CLI emulation pattern doesn't scale and adds maintenance burden.
- No path toward streaming, notifications, or resource exposure that MCP provides.

### 11.3 Stdio MCP Server in Agent Container

Run the MCP server as a local stdio process in the agent container. Rejected because:
- The MCP server needs the GitHub token to make API calls.
- Putting the token in the agent container violates Invariant 1.
- The gateway architecture keeps credentials in a separate container by design.

### 11.4 MCP Server Inside the Gateway Container

Run the MCP server as another listener in the existing gateway binary. Rejected because:
- Conflates git transport with API operations — different failure domains.
- Can't update/restart the MCP server without interrupting git operations.
- As more MCP servers are added (K8s, secrets), the gateway becomes a monolith.
- Separate containers allow independent images, resource limits, and restart policies.

### 11.5 GitHub MCP as a Shared Service

Run one GitHub MCP server shared across all sessions. Rejected because:
- Each session's agent must be restricted to writes against its own repo.
- A shared server would need per-request routing based on caller identity — complex and error-prone.
- Per-session sidecars get policy for free via startup configuration.

## 12. Rollout

1. Land this proposal doc.
2. Implement MCP server in `internal/github-mcp/` as a standalone binary with its own Dockerfile.
3. Add `StartGitHubMCP()` to container client and wire into orchestrator lifecycle.
4. Update orchestrator to write MCP config to agent settings. Test that Claude uses MCP tools.
5. Remove `forge-gh` binary and REST API after confirming MCP covers all use cases (Phase 3).
6. Add e2e test: start session, verify Claude can list PRs, create PR, and comment via MCP tools.
7. Document the sidecar/shared service model in architecture docs.
