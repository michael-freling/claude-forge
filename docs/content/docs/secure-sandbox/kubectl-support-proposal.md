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

This proposal adds `kubectl` to the agent in a way that preserves Invariant 1 (the agent never holds a credential), uses standard Kubernetes RBAC for what the agent is allowed to do (no custom path-level filter), and offers a `claude-forge kube render` command that generates the necessary RBAC manifests against the user's actual cluster.

## 2. Goals and Non-Goals

### Goals

- The agent can run `kubectl` against the user's cluster: `get`, `describe`, `logs`, `apply`, `scale`, `rollout`, `top`, `auth can-i`, `explain`, `api-resources`, etc.
- The agent **cannot** read Secret objects (no `kubectl get secret`).
- The agent **cannot** mint or read tokens via the TokenRequest API (`serviceaccounts/token`).
- The agent **cannot** see its own credential (the SA token lives only in the gateway container, not the agent).
- The agent **cannot** modify RBAC, register admission webhooks, or impersonate other identities.
- The agent **cannot** delete cluster-scoped resources (namespaces, nodes, persistent volumes, CRDs).
- The agent **cannot** `kubectl exec` or `kubectl attach` into pods.
- Failure mode is **deny** — RBAC is allow-only, so anything not explicitly granted is rejected by the API server.

### Non-Goals

- Multi-cluster context switching from inside the agent. The host selects one cluster before launch; the container sees one cluster.
- `kubectl exec` and `kubectl attach`. A pod shell can read pod-mounted secrets and run arbitrary processes inside the cluster network. Excluded from v1.
- `kubectl port-forward` is allowed (it's how developers reach pod ports; without `exec` the blast radius is limited to whatever the pod's port already exposes).
- Cluster administration (`kubectl edit clusterrole`, namespace creation, node cordon).
- Cloud-provider auth plugins (`gke-gcloud-auth-plugin`, `aws-iam-authenticator`). The cluster credentials live with the gateway, not the agent; auth plugins are not needed because the gateway uses a static ServiceAccount token.

## 3. Threat Model

The agent is an LLM running with `--dangerously-skip-permissions`. Assume any tool available to the agent can and will be invoked, including via prompt injection from repository content, issue comments, or model output. Three classes of harm to prevent:

| # | Harm | Example attack | Defense |
|---|---|---|---|
| 1 | Credential exfiltration via Secrets | `kubectl get secret -A -o yaml`, base64-decode, embed in commit | RBAC denies `secrets` resource |
| 2 | Credential exfiltration via TokenRequest | `kubectl create token <sa>` to mint a token for a more privileged SA | RBAC denies `serviceaccounts/token` subresource |
| 3 | Credential exfiltration via kubeconfig | `cat $KUBECONFIG`, `kubectl config view --raw` | Agent's kubeconfig points at gateway, has no token, no CA, no cluster URL — nothing sensitive to leak |
| 4 | Identity escalation | `kubectl --as=admin get …` | RBAC denies `impersonate` verb |
| 5 | Destructive cluster ops | `kubectl delete namespace prod`, `kubectl delete node`, `kubectl delete crd` | RBAC grants only read on cluster-scoped resources |
| 6 | Pod-level credential access | `kubectl exec -- cat /var/run/secrets/...` | RBAC denies `pods/exec` and `pods/attach` |
| 7 | RBAC tampering | `kubectl create clusterrolebinding …` | RBAC denies the `rbac.authorization.k8s.io` group entirely |
| 8 | Admission-webhook tampering | `kubectl apply -f malicious-webhook.yaml` | RBAC denies the `admissionregistration.k8s.io` group entirely |

## 4. Architecture

```
┌───────────────────────────────────────────────────────────────────────────┐
│  Host                                                                     │
│                                                                           │
│  ~/.kube/config (user's full kubeconfig)                                  │
│        │                                                                  │
│        │ used by claude-forge on host to call TokenRequest                │
│        │ for the rendered ServiceAccount                                  │
│        ▼                                                                  │
│  short-lived SA token  ────────────► gateway env var (KUBE_TOKEN)         │
│                                                                           │
│  ┌────────────────────────┐    ┌────────────────────────────────┐         │
│  │ Agent container        │    │ Gateway container              │         │
│  │                        │    │                                │         │
│  │ kubectl                │    │ :8090  k8s reverse proxy       │         │
│  │   --server=            │───▶│        - injects header:       │         │
│  │   http://gateway:8090  │    │            Authorization:      │         │
│  │                        │    │            Bearer $KUBE_TOKEN  │         │
│  │ KUBECONFIG=            │    │        - rewrites Host         │         │
│  │   /etc/forge/kubeconfig│    │        - forwards as-is        │         │
│  │   server: gateway:8090 │    │          (no path filter)      │         │
│  │   token: ""            │    │                                │         │
│  │   ca:    ""            │    │ ──────────► real K8s API server│         │
│  └────────────────────────┘    └────────────────────────────────┘         │
│                                                                           │
└───────────────────────────────────────────────────────────────────────────┘
```

### 4.1 Components

**Agent container** gains:
- `kubectl` binary (latest stable from `dl.k8s.io`).
- A generated kubeconfig at `/etc/forge/kubeconfig` with one cluster (`server: http://gateway:8090`), one context, no `token`, no `certificate-authority`. `KUBECONFIG=/etc/forge/kubeconfig` is set.
- **No** `~/.kube/`, no token, no CA, no cluster identity.

**Gateway container** gains:
- A new HTTP listener on `:8090` that reverse-proxies to the cluster API server.
- `KUBE_TOKEN` env var: the ServiceAccount bearer token, supplied by `claude-forge` at startup.
- `KUBE_API_SERVER` env var: the real cluster URL.
- `KUBE_CA_DATA` env var (or mount): the cluster CA cert for TLS verification of the upstream API server.
- The proxy is **dumb** — it does not inspect or filter request paths. It only:
  1. Rewrites the request URL to the upstream API server.
  2. Sets `Authorization: Bearer $KUBE_TOKEN`.
  3. Forwards the request, including hijacked HTTP for log streaming and other long-lived connections.

**Host-side `claude-forge` start flow:**
1. Read the user's kubeconfig, select the configured context, extract `(server URL, CA data)`.
2. Call TokenRequest against that cluster for the configured ServiceAccount → short-lived bound token (default expiry, typically 1 hour).
3. Pass `KUBE_TOKEN`, `KUBE_API_SERVER`, `KUBE_CA_DATA` to the **gateway** container as env vars.
4. Generate the agent's stub kubeconfig and mount it.

The agent never receives any of these values.

### 4.2 Why No Path Filter

The original draft of this proposal had the gateway parse every K8s API URL and apply method+path allow/deny lists. That approach is rejected:

- RBAC already enforces `(verb, resource, name, namespace)` natively at the API server, with audit logging and decades of production use.
- A custom Go filter is a parallel, hand-maintained policy that can drift from RBAC and has its own bugs.
- WebSocket upgrades, watch streams, and exec/attach hijacking each require special handling in a path filter; for RBAC they're just subresources.
- New CRDs added to the cluster automatically work with discovery-driven RBAC rendering; a path filter would need a code change for every new resource.

The gateway's job is reduced to exactly one thing: **inject the credential the agent must not see**. RBAC is the policy layer.

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

For a typical cluster the rendered ClusterRole is around six to ten rules:

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
- apiGroups: [argoproj.io, cert-manager.io, monitoring.coreos.com, ...]
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

**Flags:**

| Flag | Default | Purpose |
|---|---|---|
| `--cluster-role-name` | `claude-forge-agent` | Name of the generated ClusterRole and ClusterRoleBinding |
| `--service-account-name` | `claude-forge-agent` | Name of the generated ServiceAccount |
| `--service-account-namespace` | `default` | Namespace to put the ServiceAccount in |
| `--kubeconfig` | `$KUBECONFIG` or `~/.kube/config` | Whose discovery to call against |
| `--context` | current context in kubeconfig | Which cluster's discovery to use |

**Output (stdout):** YAML containing exactly three resources, in apply order: `ServiceAccount`, `ClusterRole`, `ClusterRoleBinding`. No `Namespace` is bundled — the user is responsible for ensuring the chosen namespace exists.

**Usage:**

```bash
# One-time setup against the cluster
claude-forge kube render --service-account-namespace claude-forge \
  | kubectl apply -f -

# Then point claude-forge at the same SA in its config (~/.config/claude-forge/config.yaml):
kubernetes:
  enabled: true
  context: ""                                # default: current
  service_account_name: claude-forge-agent
  service_account_namespace: claude-forge
```

At every `claude-forge start`, the host calls TokenRequest against that SA to mint a fresh token for the gateway. The token is short-lived; if the session outlives it, the gateway re-requests via a sidecar refresh loop (out of scope for v1 — start with single-token sessions).

## 7. Implementation Sketch

| Change | Location |
|---|---|
| Install `kubectl` in agent image | `docker/agent/Dockerfile` |
| Generate stub kubeconfig at container start | `docker/agent/entrypoint.sh` |
| Generate ServiceAccount kubeconfig (gateway-side) handling | `internal/forge/kube/` (new package) |
| K8s reverse proxy (credential injection only) | `internal/gateway/k8sproxy/` (new package) |
| Wire proxy into gateway server | `internal/gateway/server.go` |
| `kube render` subcommand and discovery-based rule generator | `cmd/claude-forge/kube_render.go` (new) |
| TokenRequest call at session start | `internal/forge/orchestrator.go` |
| Config struct fields | `internal/forge/config/config.go` |
| User docs | new section in `secure-sandbox-architecture.md`; update `README.md` |

The render command's rule generator is pure-data (input: discovery output + carveout config; output: `[]rbacv1.PolicyRule`), so it's testable without a live cluster — feed it canned discovery fixtures.

## 8. Rejected Alternatives

### 8.1 Gateway with Method+Path Filter (original draft)

The first version of this proposal had the gateway implement an allow/deny list keyed on `(HTTP method, URL path)` mirroring K8s API URL grammar. Rejected because:

- Duplicates RBAC, which already exists, is audited, and is enforced at the API server.
- Hand-maintained list drifts as Kubernetes adds resources; discovery-driven RBAC adapts automatically.
- Required special-casing for WebSocket upgrades (watch, log follow, exec) and hijacked connections.
- A bug in the filter is a security hole; a bug in RBAC is a Kubernetes CVE.

### 8.2 Mount Kubeconfig in the Agent (with RBAC scoping)

Considered: mount a kubeconfig file in the agent that has the SA token embedded, RBAC-scoped. Rejected because:

- Violates Invariant 1: the agent has the credential and could exfiltrate it.
- Even a scoped token, if leaked, can be used outside the sandbox until expiry.
- The gateway architecture costs little extra (one container that already exists for git/GitHub) and earns full Invariant 1 compliance.

### 8.3 Per-Tier Roles (`view`, `edit`, `admin`)

Considered: ship pre-built ClusterRoles for different "tiers" the user can choose from. Rejected for v1 because:

- Adds a `--tier` flag and forces the user to think about a security model rather than getting one safe default.
- The built-in `edit` ClusterRole grants secrets access; can't be used as-is.
- A single sane default (the discovery-driven role above) covers the common case. Tiers can be added later if real demand appears.

## 9. Open Questions

1. **Token refresh for long-lived sessions.** TokenRequest tokens have a finite expiry. v1 uses one token per session. v2 should refresh in the gateway before expiry; needs a small refresh loop and a way for the host to pass the kubeconfig (or a refresh hook) into the gateway.
2. **Audit visibility.** All cluster activity will be audit-logged on the cluster as the SA, not as the user. Worth documenting so users can grep audit logs for `claude-forge-agent`.
3. **CRDs with sensitive `spec` fields.** A `SealedSecret`, `ExternalSecret`, or vault CRD may carry credential material in its spec. The current model grants full CRUD on these (they're just custom resources to RBAC). A future config could let users add per-CRD carveouts to render. v1 documents the limitation.
4. **`port-forward` policy.** Currently allowed. If a pod exposes an admin port (database, redis, etc.) the agent can reach it. Accept the risk for v1; revisit if it bites.
5. **Multi-context.** One gateway, one cluster, one SA per `claude-forge` instance. Users who need two clusters run two instances. Re-evaluate if this becomes painful.

## 10. Rollout

1. Land this proposal doc (current PR).
2. Implement the gateway K8s reverse proxy and the agent-side kubeconfig generation behind `kubernetes.enabled: false` default. Ship as opt-in.
3. Implement `claude-forge kube render` with discovery-driven rendering and carveouts.
4. Add e2e test: spin up a `kind` cluster in CI, render → apply → start → assert that `kubectl get pods` succeeds, `kubectl get secret` returns 403, `kubectl delete namespace default` returns 403, `kubectl exec` returns 403.
5. Document the recommended workflow in `secure-sandbox-architecture.md` and `README.md`.
6. Flip default to enabled after the e2e suite is green for two weeks.
