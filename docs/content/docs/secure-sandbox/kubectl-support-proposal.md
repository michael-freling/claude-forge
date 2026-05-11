---
title: "Proposal: kubectl Support in claude-forge"
weight: 3
---

# Proposal: kubectl Support in claude-forge

**Status**: Draft
**Depends on**: [Secure Sandbox Architecture]({{< relref "/docs/secure-sandbox/secure-sandbox-architecture" >}})

## 1. Motivation

Claude Code users frequently need to inspect and modify Kubernetes state while developing — listing pods, viewing logs, scaling deployments, applying manifests. Today, `claude-forge` has no `kubectl` in the agent image, so users either:

1. Drop out of the sandbox to run `kubectl` on the host (breaks the workflow), or
2. Mount `~/.kube/config` into the container (violates Invariant 1 — agent must never have access to the underlying credentials).

This proposal adds `kubectl` to the agent in a way that preserves Invariant 1, uses standard Kubernetes RBAC for what the agent is allowed to do, supports multiple cluster contexts in a single session, and offers a `claude-forge kube render` command that generates the necessary RBAC manifests against the user's actual cluster.

## 2. Goals and Non-Goals

### Goals

- The agent can run `kubectl` against one or more user-configured cluster contexts: `get`, `describe`, `logs`, `apply`, `scale`, `rollout`, `top`, `auth can-i`, `explain`, `api-resources`, etc.
- The agent **cannot** read Secret objects (no `kubectl get secret`).
- The agent **cannot** mint or read tokens via the TokenRequest API (`serviceaccounts/token`).
- The agent **cannot** see its own credential — ServiceAccount tokens live only in the gateway container, not in the agent.
- The agent **cannot** modify RBAC, register admission webhooks, or impersonate other identities.
- The agent **cannot** delete cluster-scoped resources (namespaces, nodes, persistent volumes, CRDs).
- The agent **cannot** `kubectl exec` or `kubectl attach` into pods.
- Multiple cluster contexts can be exposed to a single session, each with its own RBAC scope and credential.
- Failure mode is **deny** — RBAC is allow-only, so anything not explicitly granted is rejected by the API server.

### Non-Goals

- `kubectl exec` and `kubectl attach`. A pod shell can read pod-mounted secrets and run arbitrary processes inside the cluster network. Excluded.
- `kubectl port-forward` is allowed (it's how developers reach pod ports; without `exec` the blast radius is limited to whatever the pod's port already exposes).
- Cluster administration (`kubectl edit clusterrole`, namespace creation, node cordon).
- Cloud-provider auth plugins (`gke-gcloud-auth-plugin`, `aws-iam-authenticator`). Cluster credentials live with the gateway, which uses static ServiceAccount bearer tokens.
- Changes to the existing GitHub gateway. This proposal is K8s-only.

## 3. Threat Model

The agent is an LLM running with `--dangerously-skip-permissions`. Assume any tool available to the agent can and will be invoked, including via prompt injection from repository content, issue comments, or model output.

| # | Harm | Example attack | Defense |
|---|---|---|---|
| 1 | Credential exfiltration via Secrets | `kubectl get secret -A -o yaml`, base64-decode, embed in commit | RBAC denies `secrets` resource |
| 2 | Credential exfiltration via TokenRequest | `kubectl create token <sa>` to mint a token for a more privileged SA | RBAC denies `serviceaccounts/token` subresource |
| 3 | Credential exfiltration via kubeconfig | `cat $KUBECONFIG`, `kubectl config view --raw` | Agent's kubeconfig only contains gateway URLs — no token, no CA, no real cluster URL |
| 4 | Identity escalation | `kubectl --as=admin get …` | RBAC denies `impersonate` verb |
| 5 | Destructive cluster ops | `kubectl delete namespace prod`, `kubectl delete node`, `kubectl delete crd` | RBAC grants only read on cluster-scoped resources |
| 6 | Pod-level credential access | `kubectl exec -- cat /var/run/secrets/...` | RBAC denies `pods/exec` and `pods/attach` |
| 7 | RBAC tampering | `kubectl create clusterrolebinding …` | RBAC denies the `rbac.authorization.k8s.io` group entirely |
| 8 | Admission-webhook tampering | `kubectl apply -f malicious-webhook.yaml` | RBAC denies the `admissionregistration.k8s.io` group entirely |
| 9 | Cross-context escalation | `kubectl --context=prod` from a session that should only see dev | Agent's kubeconfig only lists explicitly enabled contexts; gateway routes by URL prefix and an unknown prefix returns 404 |

## 4. Architecture

```
┌────────────────────────────────────────────────────────────────────────────┐
│  Host                                                                      │
│                                                                            │
│  ~/.kube/config (user's full kubeconfig)                                   │
│        │                                                                   │
│        │ on `claude-forge start`: for each enabled context,                │
│        │ call TokenRequest against the rendered ServiceAccount             │
│        ▼                                                                   │
│  k8s-contexts.json (host file, 0600)                                       │
│  [ {name: dev,     server, ca, token},                                     │
│    {name: staging, server, ca, token} ]                                    │
│        │                                                                   │
│        │ mounted read-only into gateway container                          │
│        ▼                                                                   │
│  ┌────────────────────────┐    ┌────────────────────────────────────────┐  │
│  │ Agent container        │    │ Gateway container                      │  │
│  │                        │    │ (single binary: `claude-forge gateway`)│  │
│  │ kubectl                │    │                                        │  │
│  │                        │    │ :8080  git HTTP proxy                  │  │
│  │ KUBECONFIG=            │    │ :8083  forge-gh REST API               │  │
│  │   /etc/forge/kubeconfig│    │ :8090  k8s reverse proxy ◄────────┐    │  │
│  │                        │    │         routes by /<ctx> prefix:  │    │  │
│  │ contexts:              │    │           /dev/...     → dev      │    │  │
│  │  dev → :8090/dev       │───►│           /staging/... → staging  │    │  │
│  │  staging → :8090/staging│   │         injects bearer token      │    │  │
│  │                        │    │         no path filter            │    │  │
│  │ no token, no CA,       │    │                                   │    │  │
│  │ no real cluster URL    │    │ ──────────► real K8s API server   │    │  │
│  └────────────────────────┘    └────────────────────────────────────────┘  │
│                                                                            │
└────────────────────────────────────────────────────────────────────────────┘
```

### 4.1 Single Gateway Binary

The gateway container today already runs one `claude-forge gateway` binary that hosts two listeners (`:8080` git proxy, `:8083` forge-gh API) inside one Go process via `internal/gateway.Server`. The K8s proxy is a **third listener on the same `Server` struct** — not a separate binary, not a separate container, not a sidecar. Adding it touches `internal/gateway/server.go` to register the new handler and a new `internal/gateway/k8sproxy/` package for the handler itself.

### 4.2 Components

**Agent container** gains:
- `kubectl` binary (latest stable from `dl.k8s.io`).
- A generated kubeconfig at `/etc/forge/kubeconfig` with one cluster + context per enabled context, each cluster's `server` set to `http://gateway:8090/<ctx>`. No `token`, no `certificate-authority`, no real cluster URLs anywhere.
- `KUBECONFIG=/etc/forge/kubeconfig` set in the container env.
- **No** `~/.kube/`, no tokens, no CAs.

**Gateway container** (single binary, multiple listeners) gains:
- A new HTTP listener on `:8090`.
- A read-only mount of `k8s-contexts.json` (the per-context routing table the host writes at session start).
- The K8s handler in `internal/gateway/k8sproxy/` reads the routing table on startup, then for each request:
  1. Extract the first path segment as context name.
  2. Look up `(upstream URL, CA, bearer token)` for that context. 404 if not found.
  3. Strip the context prefix from the path.
  4. Set `Authorization: Bearer <token>`, set `Host` to upstream, forward.
  5. Pass through hijacked HTTP for log streaming and other long-lived connections.
- The proxy is **dumb** — no method+path filtering. RBAC at the upstream API server is the policy point.

**Host-side `claude-forge start` flow:**
1. Read the user's kubeconfig.
2. For each context listed in `~/.config/claude-forge/config.yaml` under `kubernetes.contexts`:
   - Resolve `(server URL, CA data)`.
   - Call TokenRequest against the configured ServiceAccount → short-lived bound token.
3. Write `k8s-contexts.json` (0600, host user-owned, regenerated each session) containing the per-context routing table.
4. Mount `k8s-contexts.json` read-only into the gateway container.
5. Generate the agent's stub kubeconfig with one entry per context and mount it.

The agent never receives any tokens, CAs, or real cluster URLs.

### 4.3 Why No Path Filter

RBAC already enforces `(verb, resource, name, namespace)` natively at the API server, with audit logging and decades of production use. A custom Go filter would be a parallel, hand-maintained policy that can drift from RBAC and has its own bugs. The gateway's job is reduced to exactly one thing: **inject the credential the agent must not see.** New CRDs added to the cluster work automatically with discovery-driven RBAC rendering; a path filter would need a code change for every new resource.

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
  contexts:
    - host_context: dev          # name in user's ~/.kube/config
      service_account_name: claude-forge-agent
      service_account_namespace: claude-forge
    - host_context: staging
      service_account_name: claude-forge-agent
      service_account_namespace: claude-forge
  default_context: dev           # which one is current-context in the agent's kubeconfig
```

### 7.2 Generated Agent Kubeconfig

```yaml
apiVersion: v1
kind: Config
clusters:
  - name: dev
    cluster: { server: http://gateway:8090/dev }
  - name: staging
    cluster: { server: http://gateway:8090/staging }
contexts:
  - name: dev
    context: { cluster: dev,     namespace: default }
  - name: staging
    context: { cluster: staging, namespace: default }
current-context: dev
```

The agent runs `kubectl --context=staging get pods`; kubectl sends the request to `http://gateway:8090/staging/api/v1/namespaces/default/pods`; the gateway peels `/staging` off the path, looks up `(upstream URL, CA, token)` for that context from `k8s-contexts.json`, sets the Authorization header, and forwards.

### 7.3 Per-Context State at the Gateway

The gateway holds, for each enabled context: upstream API server URL, upstream CA cert, bearer token. These are written by the host into a single file:

```json
[
  {
    "name": "dev",
    "server": "https://dev-cluster.example.com",
    "ca_data": "<PEM>",
    "token": "<short-lived bound token>"
  },
  {
    "name": "staging",
    "server": "https://staging-cluster.example.com",
    "ca_data": "<PEM>",
    "token": "<short-lived bound token>"
  }
]
```

File is `0600` on the host, owned by the launching user, regenerated every session, mounted read-only into the gateway. The agent never sees this file.

## 8. Implementation Sketch

| Change | Location |
|---|---|
| Install `kubectl` in agent image | `docker/agent/Dockerfile` |
| Generate stub multi-context kubeconfig at container start | `docker/agent/entrypoint.sh` (driven by env from `claude-forge`) |
| K8s reverse proxy with prefix-based context routing | `internal/gateway/k8sproxy/` (new package) |
| Wire third listener into existing gateway server | `internal/gateway/server.go` |
| Routing-table loader (reads `k8s-contexts.json`) | `internal/gateway/k8sproxy/contexts.go` |
| `kube render` subcommand and discovery-based rule generator | `cmd/claude-forge/kube_render.go` (new) |
| TokenRequest call(s) at session start, write `k8s-contexts.json` | `internal/forge/kube/` (new package), called from `internal/forge/orchestrator.go` |
| Config struct fields | `internal/forge/config/config.go` |
| User docs | new section in `secure-sandbox-architecture.md`; update `README.md` |

The render command's rule generator is pure-data (input: discovery output + carveout config; output: `[]rbacv1.PolicyRule`), so it's testable without a live cluster — feed it canned discovery fixtures.

## 9. Rejected Alternatives

### 9.1 Gateway with Method+Path Filter

The first version of this proposal had the gateway implement an allow/deny list keyed on `(HTTP method, URL path)` mirroring K8s API URL grammar. Rejected because:

- Duplicates RBAC, which already exists, is audited, and is enforced at the API server.
- Hand-maintained list drifts as Kubernetes adds resources; discovery-driven RBAC adapts automatically.
- Required special-casing for WebSocket upgrades (watch, log follow) and hijacked connections.
- A bug in the filter is a security hole; a bug in RBAC is a Kubernetes CVE.

### 9.2 Mount Kubeconfig in the Agent (with RBAC scoping)

Mount a kubeconfig file in the agent that has the SA token embedded, RBAC-scoped. Rejected because:

- Violates Invariant 1: the agent has the credential and could exfiltrate it.
- Even a scoped token, if leaked, can be used outside the sandbox until expiry.
- The gateway architecture costs little extra (one container, one extra listener) and earns full Invariant 1 compliance.

### 9.3 Per-Tier Roles (`view`, `edit`, `admin`)

Pre-built ClusterRoles for different "tiers" the user can choose from. Rejected for v1 because:

- Adds a `--tier` flag and forces the user to think about a security model rather than getting one safe default.
- The built-in `edit` ClusterRole grants secrets access; can't be used as-is.
- Discovery-driven rendering with the carveouts in §5 covers the common case. Tiers can be added later if real demand appears.

## 10. Open Questions

1. **Token refresh for long-lived sessions.** TokenRequest tokens have a finite expiry. v1 uses one token per context per session. v2 should refresh in the gateway before expiry; needs a small refresh loop and a way for the host to participate (or for the gateway to re-read a regenerated `k8s-contexts.json`).
2. **Audit visibility.** All cluster activity is audit-logged on the cluster as the SA, not as the user. Worth documenting so users can grep audit logs for `claude-forge-agent`.
3. **CRDs that embed credentials in `spec`.** Most credential-adjacent CRDs are safe to read — `SealedSecret` holds only ciphertext, `ExternalSecret` holds only a reference to the upstream store, cert-manager `Certificate` writes the key material out to a Secret (which RBAC blocks). The narrower concern is CRDs that put plaintext credentials directly in `spec` — for example, some `Issuer`/`ClusterIssuer` configurations with inline ACME or webhook tokens, or backup-operator CRDs with embedded cloud credentials. The current model grants full CRUD on all CRDs. A future config could let users add per-CRD carveouts to render; v1 documents the limitation.
4. **`port-forward` policy.** Currently allowed. If a pod exposes an admin port (database, redis) the agent can reach it. Accept the risk for v1; revisit if it bites.

## 11. Rollout

1. Land this proposal doc (current PR).
2. Implement the gateway K8s proxy as a third listener on `internal/gateway.Server`, plus the agent-side multi-context kubeconfig generation. Ship behind `kubernetes.enabled: false` default.
3. Implement `claude-forge kube render` with discovery-driven rendering and carveouts.
4. Add e2e test: spin up a `kind` cluster in CI, render → apply → start with one context → assert that `kubectl get pods` succeeds, `kubectl get secret` returns 403, `kubectl delete namespace default` returns 403, `kubectl exec` returns 403. Then add a second `kind` cluster and assert multi-context routing works end-to-end.
5. Document the workflow in `secure-sandbox-architecture.md` and `README.md`.
6. Flip default to enabled after the e2e suite is green for two weeks.
