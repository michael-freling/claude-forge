---
title: "Proposal: Kubernetes Support in claude-forge"
weight: 1
---

# Proposal: Kubernetes Support in claude-forge

**Status**: Draft
**Depends on**: [Secure Sandbox Architecture]({{< relref "/docs/secure-sandbox/architecture/secure-sandbox-architecture" >}})

## 1. Motivation

Claude Code users frequently need to inspect and modify Kubernetes state while developing — listing pods, viewing logs, scaling deployments, applying manifests. Today, `claude-forge` has no Kubernetes access in the agent, so users either:

1. Drop out of the sandbox to run `kubectl` on the host (breaks the workflow), or
2. Mount `~/.kube/config` into the container (violates Invariant 1 — agent must never have access to the underlying credentials).

This proposal adds Kubernetes access to the agent via an existing open-source MCP server ([containers/kubernetes-mcp-server](https://github.com/containers/kubernetes-mcp-server)), preserving Invariant 1 by running the MCP server in a separate shared container with the credentials. The agent interacts with Kubernetes through MCP tools — no `kubectl` binary or kubeconfig needed in the agent container.

## 2. Goals and Non-Goals

### Goals

- The agent can interact with one or more user-configured cluster contexts via MCP tools: get, describe, logs, apply, scale, rollout, etc.
- The agent **cannot** read Secret objects.
- The agent **cannot** mint or read tokens via the TokenRequest API.
- The agent **cannot** see its own credential — ServiceAccount tokens live only in the K8s MCP container, not in the agent.
- The agent **cannot** modify RBAC, register admission webhooks, or impersonate other identities.
- The agent **cannot** delete cluster-scoped resources (namespaces, nodes, persistent volumes, CRDs).
- The agent **cannot** exec or attach into pods.
- Multiple cluster contexts can be exposed to a single session, each with its own RBAC scope and credential.
- Failure mode is **deny** — RBAC is allow-only, so anything not explicitly granted is rejected by the API server.
- Uses an existing OSS MCP server rather than building a custom one.

### Non-Goals

- Building a custom K8s MCP server or reverse proxy. We reuse [containers/kubernetes-mcp-server](https://github.com/containers/kubernetes-mcp-server).
- `pods/exec` and `pods/attach`. A pod shell can read pod-mounted secrets and run arbitrary processes inside the cluster network. Excluded via RBAC.
- Cluster administration (edit clusterrole, namespace creation, node cordon).
- Cloud-provider auth plugins (`gke-gcloud-auth-plugin`, `aws-iam-authenticator`). The MCP server uses static ServiceAccount bearer tokens.

## 3. Threat Model

The agent is an LLM running with `--dangerously-skip-permissions`. Assume any tool available to the agent can and will be invoked, including via prompt injection from repository content, issue comments, or model output.

| # | Harm | Example attack | Defense |
|---|---|---|---|
| 1 | Credential exfiltration via Secrets | MCP tool to get secrets, base64-decode, embed in commit | RBAC denies `secrets` resource |
| 2 | Credential exfiltration via TokenRequest | MCP tool to create token for a more privileged SA | RBAC denies `serviceaccounts/token` subresource |
| 3 | Credential exfiltration via kubeconfig | Agent reads MCP server's kubeconfig | Kubeconfig is in a separate container; agent cannot access it |
| 4 | Identity escalation | MCP tool with impersonation | RBAC denies `impersonate` verb |
| 5 | Destructive cluster ops | Delete namespace/node/CRD via MCP | RBAC grants only read on cluster-scoped resources |
| 6 | Pod-level credential access | Exec into pod via MCP | RBAC denies `pods/exec` and `pods/attach` |
| 7 | RBAC tampering | Create clusterrolebinding via MCP | RBAC denies the `rbac.authorization.k8s.io` group entirely |
| 8 | Admission-webhook tampering | Apply malicious webhook via MCP | RBAC denies the `admissionregistration.k8s.io` group entirely |
| 9 | Cross-context escalation | Use a context not configured for this host | MCP server's kubeconfig only lists explicitly enabled contexts |

## 4. Architecture

### 4.1 Using containers/kubernetes-mcp-server

[containers/kubernetes-mcp-server](https://github.com/containers/kubernetes-mcp-server) is a Go-based native implementation that speaks directly to the Kubernetes API server (not a kubectl wrapper). Key properties:

- **Multi-context support**: When multiple clusters are defined in the kubeconfig, all tools include a `context` argument. This is exactly what we need.
- **Service Account support**: Accepts any standard kubeconfig, including ones with SA bearer tokens.
- **`--read-only` flag**: An additional safety layer on top of RBAC (useful as a defense-in-depth safeguard).
- **MCP protocol**: Exposes K8s operations as MCP tools with typed schemas.
- **Actively maintained**: By the Red Hat containers team.

We run this server in a shared container with the credentials. The agent connects via MCP over HTTP. No custom proxy, no kubectl binary in the agent.

### 4.2 Shared Service Model

The K8s MCP server runs as a **shared service** — a singleton container on the `forge-shared` Docker network, accessible to all agent sessions on the host. This is the right model because:

- K8s contexts are host-level configuration (same `~/.kube/config` regardless of which repo the agent works on).
- RBAC scoping is done at the cluster level via the ServiceAccount, not per-repo.
- Running one K8s MCP server per session would waste resources and complicate token management.

This contrasts with per-session **sidecars** (like the GitHub MCP server) which must be scoped to a specific repo. See the [GitHub MCP Server proposal]({{< relref "/docs/secure-sandbox/proposals/github-mcp-server" >}}) for the full sidecar vs shared model.

### 4.3 Network Topology

```
┌─ forge-shared network ─────────────────────────────────────────────────────┐
│                                                                            │
│  k8s-mcp  (containers/kubernetes-mcp-server)                               │
│    ├─ reads generated kubeconfig (mounted read-only)                       │
│    ├─ supports multiple contexts natively                                  │
│    ├─ --read-only flag for defense-in-depth                                │
│    └─ exposes MCP tools via Streamable HTTP                                │
│                                                                            │
├────────────────────────────────────────────────────────────────────────────┤
│                                                                            │
│  ┌── Session A ── forge_net_repoa_abc ───────────────────────────────┐    │
│  │                                                                    │    │
│  │  agent-a                                                           │    │
│  │    ├─ MCP tools → http://k8s-mcp:8090/mcp  (via shared net)       │────┤
│  │    └─ git push → gateway-a:8080  (via session net)                 │    │
│  │                                                                    │    │
│  │  gateway-a:8080  (git proxy, scoped repo-a)                        │    │
│  │  github-mcp-a:8083  (GitHub MCP, scoped repo-a)                    │    │
│  │                                                                    │    │
│  └────────────────────────────────────────────────────────────────────┘    │
│                                                                            │
│  ┌── Session B ── forge_net_repob_def ───────────────────────────────┐    │
│  │                                                                    │    │
│  │  agent-b                                                           │    │
│  │    ├─ MCP tools → http://k8s-mcp:8090/mcp  (via shared net)       │────┤
│  │    └─ git push → gateway-b:8080  (via session net)                 │    │
│  │                                                                    │    │
│  │  gateway-b:8080  (git proxy, scoped repo-b)                        │    │
│  │  github-mcp-b:8083  (GitHub MCP, scoped repo-b)                    │    │
│  │                                                                    │    │
│  └────────────────────────────────────────────────────────────────────┘    │
│                                                                            │
└────────────────────────────────────────────────────────────────────────────┘
```

The agent reaches the K8s MCP server via Docker's multi-network support:
```bash
docker network connect forge-shared agent-a
```

No Kubernetes orchestrator or Docker Compose required.

### 4.4 How It Works

**K8s MCP container** (shared, on `forge-shared` network):
- Runs `containers/kubernetes-mcp-server` with `--read-only` and `--transport http`.
- A generated kubeconfig is mounted read-only, containing SA tokens for each enabled context.
- The server natively handles multi-context — tools include a `context` parameter.
- No credentials ever reach the agent container.

**Agent container**:
- No `kubectl` binary needed.
- No kubeconfig needed.
- Interacts with Kubernetes solely through MCP tools.
- MCP config in settings.json points to `http://k8s-mcp:8090/mcp`.

**Host-side `claude-forge start` flow:**
1. Read the user's kubeconfig.
2. For each context listed in `~/.config/claude-forge/config.yaml` under `kubernetes.contexts`:
   - Resolve `(server URL, CA data)`.
   - Call TokenRequest against the configured ServiceAccount → short-lived bound token.
3. Write a generated kubeconfig (0600, host user-owned) containing only the enabled contexts with their SA tokens.
4. Start the K8s MCP container on `forge-shared` if not already running (mount the generated kubeconfig).
5. Connect the agent to the `forge-shared` network.
6. Write MCP config to agent's settings.json.

The agent never receives any tokens, CAs, or real cluster URLs.

### 4.5 Shared Service Lifecycle

The K8s MCP container is managed by the orchestrator with reference counting:

- **Start**: On first `claude-forge start` with `kubernetes.enabled: true`, create the `forge-shared` network (if not exists) and start the K8s MCP container.
- **Subsequent sessions**: Connect the new agent to `forge-shared`. The K8s MCP container is already running.
- **Stop**: When the last session with K8s enabled exits, optionally stop the K8s MCP container. (Or leave it running — a lightweight idle container costs almost nothing.)

### 4.6 Agent MCP Configuration

```json
{
  "mcpServers": {
    "kubernetes": {
      "type": "url",
      "url": "http://k8s-mcp:8090/mcp"
    }
  }
}
```

## 5. RBAC Model

### 5.1 Carveouts

Three dimensions, all small:

| Dimension | Carveout | Rationale |
|---|---|---|
| **apiGroup** | `rbac.authorization.k8s.io` | Block agent from modifying RBAC itself |
| **apiGroup** | `admissionregistration.k8s.io` | Block agent from registering admission webhooks |
| **resource** (in core ``) | `secrets` | Primary credential storage |
| **subresource** (in core ``) | `serviceaccounts/token` | Block TokenRequest minting |
| **subresource** (in core ``) | `pods/exec`, `pods/attach` | Block interactive pod access |
| **verb** | `impersonate` | Block identity escalation |
| **scope** | All writes on cluster-scoped resources | Block destructive cluster ops (delete namespace/node/PV/CRD) |

Anything not in the carveouts gets `verbs: ["*"]` (intersected with what the resource supports, with `impersonate` filtered out).

### 5.2 Discovery-Driven Rendering

The render command discovers what the cluster actually has via the standard discovery API (`/api`, `/apis/...`). For each `(apiGroup, resource, namespaced, verbs)` returned:

1. Skip if the apiGroup is in the carveout list.
2. Skip if the `(group, resource)` is in the carveout list.
3. Skip if the subresource is in the carveout list.
4. Bucket into `(apiGroup, scope-class)` where scope-class ∈ {namespaced-write, cluster-read}.
5. Per `apiGroup`: if every kept resource fits one bucket and there are no per-resource carveouts inside the group, emit a single rule with `resources: ["*"]` and `verbs: ["*"]` (or read-only verbs for cluster-read).
6. Otherwise enumerate the resources within the group, splitting by bucket.
7. Filter `impersonate` out of any verb list before emission.

### 5.3 Example Output

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: claude-forge-agent
rules:
# Core "" — must enumerate (secrets and sa/token live here)
- apiGroups: [""]
  resources:
    - bindings
    - configmaps
    - endpoints
    - events
    - limitranges
    - persistentvolumeclaims
    - persistentvolumeclaims/status
    - podtemplates
    - pods
    - pods/log
    - pods/portforward
    - pods/proxy
    - replicationcontrollers
    - replicationcontrollers/scale
    - resourcequotas
    - serviceaccounts
    - services
    - services/proxy
  verbs: ["*"]
# Core "" — cluster-scoped → read-only
- apiGroups: [""]
  resources: [namespaces, nodes, persistentvolumes, componentstatuses]
  verbs: [get, list, watch]

# Other built-in groups, all namespaced, no carveouts → wildcard
- apiGroups:
    - apps
    - autoscaling
    - batch
    - coordination.k8s.io
    - discovery.k8s.io
    - events.k8s.io
    - networking.k8s.io
    - node.k8s.io
    - policy
    - scheduling.k8s.io
  resources: ["*"]
  verbs: ["*"]

# Cluster-scoped non-core groups → read-only
- apiGroups:
    - apiextensions.k8s.io
    - certificates.k8s.io
    - flowcontrol.apiserver.k8s.io
    - storage.k8s.io
  resources: ["*"]
  verbs: [get, list, watch]

# Discovered CRD groups (whatever the cluster has installed)
- apiGroups: [argoproj.io, cert-manager.io, monitoring.coreos.com]
  resources: ["*"]
  verbs: ["*"]
```

The exact `apiGroups` lists depend on what discovery returns from the target cluster.

## 6. The `claude-forge kube render` Command

```
claude-forge kube render \
  [--cluster-role-name <name>] \
  [--service-account-name <name>] \
  [--service-account-namespace <ns>] \
  [--kubeconfig <path>] \
  [--context <ctx>]
```

| Flag | Default | Purpose |
|---|---|---|
| `--cluster-role-name` | `claude-forge-agent` | Name of the generated ClusterRole and ClusterRoleBinding |
| `--service-account-name` | `claude-forge-agent` | Name of the generated ServiceAccount |
| `--service-account-namespace` | `default` | Namespace to put the ServiceAccount in |
| `--kubeconfig` | `$KUBECONFIG` or `~/.kube/config` | Whose discovery to call against |
| `--context` | current context in kubeconfig | Which cluster's discovery to use |

**Output (stdout):** YAML containing exactly three resources, in apply order: `ServiceAccount`, `ClusterRole`, `ClusterRoleBinding`. No `Namespace` is bundled — the user is responsible for ensuring the chosen namespace exists.

**Per-context usage:** the command runs once per cluster the user wants to expose. Each cluster gets its own SA, role, and binding. Names can be reused across clusters since they live in separate API servers.

```bash
# Cluster 1
claude-forge kube render --context dev \
  --service-account-namespace claude-forge \
  | KUBECONFIG=~/.kube/config kubectl --context=dev apply -f -

# Cluster 2
claude-forge kube render --context staging \
  --service-account-namespace claude-forge \
  | KUBECONFIG=~/.kube/config kubectl --context=staging apply -f -
```

## 7. Multi-Context Support

### 7.1 Host Configuration

```yaml
# ~/.config/claude-forge/config.yaml
kubernetes:
  enabled: true
  read_only: false  # pass --read-only to the MCP server (defense-in-depth)
  contexts:
    - host_context: dev          # name in user's ~/.kube/config
      service_account_name: claude-forge-agent
      service_account_namespace: claude-forge
    - host_context: staging
      service_account_name: claude-forge-agent
      service_account_namespace: claude-forge
  default_context: dev           # which one is current-context in the MCP server's kubeconfig
```

### 7.2 Generated Kubeconfig (for the MCP server container)

```yaml
apiVersion: v1
kind: Config
clusters:
  - name: dev
    cluster:
      server: https://dev-cluster.example.com
      certificate-authority-data: <base64 CA>
  - name: staging
    cluster:
      server: https://staging-cluster.example.com
      certificate-authority-data: <base64 CA>
contexts:
  - name: dev
    context: { cluster: dev, user: dev-sa }
  - name: staging
    context: { cluster: staging, user: staging-sa }
users:
  - name: dev-sa
    user: { token: <short-lived SA token> }
  - name: staging-sa
    user: { token: <short-lived SA token> }
current-context: dev
```

This file lives only in the K8s MCP container. The agent never sees it.

## 8. Implementation Sketch

| Change | Location |
|---|---|
| Shared service lifecycle (start/stop/connect) | `internal/forge/orchestrator.go` |
| `forge-shared` network management | `internal/forge/container/client.go` |
| Generate kubeconfig for MCP server with SA tokens | `internal/forge/kube/` (new package) |
| TokenRequest call(s) at session start | `internal/forge/kube/` |
| `kube render` subcommand and discovery-based rule generator | `cmd/claude-forge/kube_render.go` (new) |
| Config struct fields for kubernetes section | `internal/forge/config/config.go` |
| Write MCP config to agent settings | `internal/forge/claudecode/settings.go` |
| User docs | update architecture docs |

No custom MCP server code needed. We pull the `containers/kubernetes-mcp-server` image and run it.

The render command's rule generator is pure-data (input: discovery output + carveout config; output: `[]rbacv1.PolicyRule`), so it's testable without a live cluster — feed it canned discovery fixtures.

## 9. Rejected Alternatives

### 9.1 Custom K8s Reverse Proxy

An earlier version of this proposal built a custom HTTP reverse proxy that injected credentials per-context. Rejected because:

- `containers/kubernetes-mcp-server` already exists, is actively maintained, and handles multi-context natively.
- A custom proxy requires handling WebSocket upgrades, long-lived connections, and K8s API URL grammar.
- Using an existing MCP server means the agent doesn't need `kubectl` at all — pure MCP tools.
- Less code to maintain and fewer security-critical surfaces to audit.

### 9.2 Mount Kubeconfig in the Agent (with RBAC scoping)

Mount a kubeconfig file in the agent that has the SA token embedded, RBAC-scoped. Rejected because:

- Violates Invariant 1: the agent has the credential and could exfiltrate it.
- Even a scoped token, if leaked, can be used outside the sandbox until expiry.
- The separate-container architecture earns full Invariant 1 compliance.

### 9.3 Per-Tier Roles (`view`, `edit`, `admin`)

Pre-built ClusterRoles for different "tiers" the user can choose from. Rejected for v1 because:

- Adds a `--tier` flag and forces the user to think about a security model rather than getting one safe default.
- The built-in `edit` ClusterRole grants secrets access; can't be used as-is.
- Discovery-driven rendering with the carveouts in §5 covers the common case. Tiers can be added later if real demand appears.

### 9.4 Run K8s MCP as a Per-Session Sidecar

Run a separate K8s MCP server per agent session. Rejected because:

- K8s contexts are host-level (same clusters regardless of which repo the agent works on).
- A shared server avoids duplicate TokenRequest calls and duplicate containers.
- RBAC scoping is per-cluster, not per-repo — no need for per-session policy isolation.

### 9.5 Gateway with Method+Path Filter

Add path-based filtering at the proxy layer. Rejected because:

- Duplicates RBAC, which already exists, is audited, and is enforced at the API server.
- Hand-maintained list drifts as Kubernetes adds resources; discovery-driven RBAC adapts automatically.
- A bug in the filter is a security hole; a bug in RBAC is a Kubernetes CVE.

## 10. Open Questions

1. **Token refresh for long-lived sessions.** TokenRequest tokens have a finite expiry. v1 uses one token per context per session. v2 should refresh by regenerating the kubeconfig and restarting/signaling the MCP server.
2. **Audit visibility.** All cluster activity is audit-logged on the cluster as the SA, not as the user. Worth documenting so users can grep audit logs for `claude-forge-agent`.
3. **CRDs that embed credentials in `spec`.** Most credential-adjacent CRDs are safe to read. The narrower concern is CRDs that put plaintext credentials directly in `spec` (e.g., some Issuer/ClusterIssuer configs). The current model grants full CRUD on all CRDs. A future config could let users add per-CRD carveouts to render; v1 documents the limitation.
4. **MCP server version pinning.** Which version of `containers/kubernetes-mcp-server` to pin? Track latest stable or pin to a tested version in `config.yaml`?
5. **Tool filtering.** If the MCP server exposes tools that bypass RBAC (unlikely given native API access), we may need a lightweight MCP proxy that filters `tools/list` and rejects disallowed `tools/call`. Verify this isn't needed before shipping.

## 11. Rollout

1. Land this proposal doc.
2. Implement shared service infrastructure in the orchestrator (`forge-shared` network, reference-counted container lifecycle, `docker network connect` for agents).
3. Add K8s config to `config.yaml`, kubeconfig generation with SA tokens, and MCP config writing. Wire into orchestrator to pull and run `containers/kubernetes-mcp-server` image. Ship behind `kubernetes.enabled: false` default.
4. Implement `claude-forge kube render` with discovery-driven rendering and carveouts.
5. Add e2e test: spin up a `kind` cluster in CI, render → apply → start with one context → assert that listing pods succeeds via MCP, getting secrets fails, deleting namespace fails. Then add a second `kind` cluster and assert multi-context works. Also test that two concurrent sessions share the same K8s MCP container.
6. Document the workflow in architecture docs.
7. Flip default to enabled after the e2e suite is green for two weeks.
