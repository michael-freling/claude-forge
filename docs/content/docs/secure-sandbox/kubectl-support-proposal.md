---
title: "Proposal: kubectl Support in claude-forge"
weight: 3
---

# Proposal: kubectl Support in claude-forge

**Status**: Draft
**Depends on**: [Secure Sandbox Architecture]({{< relref "/docs/secure-sandbox/secure-sandbox-architecture" >}})

## 1. Motivation

Claude Code users frequently need to inspect Kubernetes state while developing — listing pods, viewing logs, describing deployments, debugging failed jobs. Today, `claude-forge` has no `kubectl` in the agent image, so users either:

1. Drop out of the sandbox to run `kubectl` on the host (breaks the workflow), or
2. Mount `~/.kube/config` into the container (violates Invariant 1 — agent must never have secrets).

This proposal adds `kubectl` to the agent in a way that preserves Invariant 1 and blocks destructive cluster operations, following the same gateway-mediated pattern already used for git and the GitHub API.

## 2. Goals and Non-Goals

### Goals

- The agent can run common `kubectl` read commands (`get`, `describe`, `logs`, `top`, `auth can-i`, `explain`, `api-resources`).
- The agent can run benign mutations scoped to the current namespace (`apply`, `rollout restart`, `scale`, `port-forward` to a local pod).
- The agent **cannot** read Secret objects (`kubectl get secret`, `-o yaml` exfiltration).
- The agent **cannot** read its own kubeconfig or any embedded credentials (`kubectl config view --raw`, exec-plugin tokens, client certificates).
- The agent **cannot** delete cluster-scoped resources (namespaces, nodes, PVs, CRDs, ClusterRoles, ClusterRoleBindings) or the cluster itself.
- The agent **cannot** modify RBAC.
- Failure mode is **deny** — if the gateway can't classify a request, it is rejected.

### Non-Goals

- Multi-cluster context switching from inside the agent. The host selects the active context before launch; the container sees one cluster.
- `kubectl exec` into pods. Exec opens a shell that can read pod-mounted secrets and bypass the gateway's HTTP-level filters. Excluded from v1.
- Cluster administration tasks (`kubectl edit clusterrole`, namespace creation, node cordon).
- Support for cloud-provider auth plugins inside the agent (`gke-gcloud-auth-plugin`, `aws-iam-authenticator`, etc.). These are credentials and live with the gateway, not the agent.

## 3. Threat Model

The agent is an LLM running with `--dangerously-skip-permissions`. Assume any tool available to the agent can and will be invoked, including via prompt injection from repository content, issue comments, or model output. Three classes of harm to prevent:

| # | Harm | Example attack |
|---|---|---|
| 1 | Credential exfiltration | `kubectl get secret -A -o yaml` → base64-decode → embed in commit / PR body / log line |
| 2 | Credential exfiltration via kubeconfig | `cat ~/.kube/config`, `kubectl config view --raw`, reading exec-plugin tokens cached in `~/.kube/cache/` |
| 3 | Destructive cluster ops | `kubectl delete namespace prod`, `kubectl delete node`, `kubectl delete crd`, `kubectl delete clusterrolebinding` |

Lateral risks worth noting but out of scope for v1: `kubectl exec` (covered above), `kubectl cp` from a pod that mounts a secret, `kubectl proxy` started by the agent itself (would re-expose the API surface unfiltered — the gateway must be the only path).

## 4. Architecture

The same gateway pattern used for GitHub is extended to Kubernetes. The agent never sees a kubeconfig or a cluster CA; it talks to the gateway over plain HTTP on the internal Docker network, and the gateway authenticates outbound to the real API server using credentials mounted only in the gateway container.

```
┌──────────────────────────────────────────────────────────────────────┐
│  Host                                                                │
│                                                                      │
│  ~/.kube/config (ro)  ──────────────────────┐                        │
│                                             ▼                        │
│  ┌────────────────────────┐    ┌────────────────────────────────┐    │
│  │ Agent container        │    │ Gateway container              │    │
│  │                        │    │                                │    │
│  │ kubectl                │    │ :8090  k8s reverse proxy       │    │
│  │   --server=            │───▶│        - rewrites Host header  │    │
│  │   http://gateway:8090  │    │        - injects bearer token  │    │
│  │                        │    │        - filters by method+path│    │
│  │ KUBECONFIG=            │    │                                │    │
│  │   /etc/forge/kubeconfig│    │ ──────────► real K8s API server│    │
│  │   (no creds, no CA)    │    │                                │    │
│  └────────────────────────┘    └────────────────────────────────┘    │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

### 4.1 Components

**Agent container** gains:
- `kubectl` binary (latest stable from `dl.k8s.io`).
- `/etc/forge/kubeconfig` — a generated kubeconfig with one cluster (`server: http://gateway:8090`, no `certificate-authority`, no `token`, no `exec`), one context, and one namespace (the user's selected namespace from the host kubeconfig).
- `KUBECONFIG=/etc/forge/kubeconfig` set in the container env.
- **No** `~/.kube/` mount. **No** cloud-provider auth plugins.

**Gateway container** gains:
- `~/.kube/config` mounted read-only (gateway already runs with the principle that secrets live here, not in the agent — same pattern as `~/.ssh` and `~/.config/gh`).
- A new HTTP listener on `:8090` that reverse-proxies to the cluster API server, with a request filter (Section 4.2).

### 4.2 Request Filtering

The gateway classifies every incoming request by `(method, path)` against the K8s API URL grammar (`/api/v1/...`, `/apis/<group>/<version>/...`). Allow/deny lists below; anything not matched is **denied**.

**Always denied** — credential and cluster-control surfaces:

| Method | Path pattern | Reason |
|---|---|---|
| `*` | `**/secrets`, `**/secrets/**` | Block secret reads/writes (Invariant 1) |
| `*` | `**/serviceaccounts/*/token` | Block TokenRequest API |
| `DELETE` | `/api/v1/namespaces/*` | No namespace deletion |
| `DELETE` | `/api/v1/nodes/*` | No node deletion |
| `DELETE` | `/api/v1/persistentvolumes/*` | Cluster-scoped storage |
| `DELETE` | `/apis/apiextensions.k8s.io/**/customresourcedefinitions/*` | No CRD deletion |
| `*` (write) | `/apis/rbac.authorization.k8s.io/**` | No RBAC modification |
| `*` (write) | `/apis/admissionregistration.k8s.io/**` | No admission controller changes |
| `POST` | `/api/v1/nodes/*/proxy/**` | Block node proxy |
| `*` | `**/pods/*/exec`, `**/pods/*/attach` | No interactive pod access (v1) |
| `*` | `**/pods/*/portforward` | Excluded from v1; revisit |

**Always allowed** — read-only discovery and observation:

| Method | Path pattern |
|---|---|
| `GET` | `/api`, `/apis`, `/openapi/**`, `/version`, `/healthz`, `/readyz` |
| `GET` | `/api/v1/namespaces/<scoped-ns>/{pods,services,configmaps,events,...}` (excluding `secrets`) |
| `GET` | `/apis/apps/v1/namespaces/<scoped-ns>/{deployments,replicasets,statefulsets,daemonsets}` |
| `GET` | `/api/v1/namespaces/<scoped-ns>/pods/*/log` |
| `POST` | `/apis/authorization.k8s.io/v1/selfsubjectaccessreviews` (so `kubectl auth can-i` works) |

**Conditionally allowed** — namespace-scoped mutations to non-RBAC, non-Secret resources:

- `POST`/`PUT`/`PATCH`/`DELETE` on namespaced resources within `<scoped-ns>`, excluding the always-denied list.
- `<scoped-ns>` is the single namespace the gateway was configured with at launch. Cross-namespace mutations are denied even if RBAC would permit them.

### 4.3 Defense in Depth

The proxy is the architectural boundary, but two extra layers reduce the chance of bypass:

1. **Cluster RBAC.** `claude-forge` documentation will recommend the user point their kubeconfig at a least-privilege ServiceAccount (read-only on most resources, no `secrets`, no cluster-scoped writes). If the proxy filter has a bug, RBAC is the second wall.
2. **PreToolUse hook.** The existing hooks framework (`internal/hooks/`) gains a `kubectl_rule.go` that pattern-matches the agent's bash invocation. It blocks string-level patterns the proxy would also block — `kubectl get secret`, `kubectl delete namespace`, `kubectl config view --raw`, `kubectl --kubeconfig=` (attempt to override the generated config). This catches the obvious cases earlier and produces a clear error message in the agent's transcript instead of an opaque HTTP 403.

Both layers are advisory relative to the proxy. The proxy is the contract; the hook is a usability and clarity aid.

## 5. Configuration

Host-side, in `~/.config/claude-forge/config.yaml`:

```yaml
kubernetes:
  enabled: false           # opt-in; default off
  kubeconfig: ~/.kube/config
  context: ""              # optional; defaults to current-context
  namespace: ""            # optional; defaults to context's namespace, then "default"
```

`claude-forge start` reads this, validates that the chosen context exists, resolves the namespace, and passes both into the gateway as env vars. The agent's generated kubeconfig hard-codes the resolved namespace.

CLI override for ad-hoc use:

```
claude-forge start --kube-namespace=staging
claude-forge start --no-kube     # disable for this run
```

## 6. Implementation Sketch

| Change | Location |
|---|---|
| Install `kubectl` in agent image | `docker/agent/Dockerfile` |
| Generate stub kubeconfig at container start | `docker/agent/entrypoint.sh` (driven by env vars set by `claude-forge`) |
| Mount `~/.kube/config` read-only into gateway | `internal/forge/container/client.go` |
| K8s reverse proxy and filter | new package `internal/gateway/k8sproxy/` |
| Wire proxy into gateway server | `internal/gateway/server.go` |
| `kubectl` PreToolUse rule | new file `internal/hooks/kubectl_rule.go` (+ test) |
| Config struct fields | `internal/forge/config/config.go` |
| User docs | new section in `secure-sandbox-architecture.md`, plus a usage section in `README.md` |

Out-of-band: the gateway's container image gains `ca-certificates` if not already present, so it can verify the cluster API server's TLS.

## 7. Open Questions

1. **`kubectl exec` and `kubectl port-forward`.** Both are genuinely useful for debugging but tunnel arbitrary protocols past the HTTP filter. Options for v2: (a) keep them denied; (b) allow `port-forward` only to a fixed allowlist of pod-label selectors; (c) allow `exec` with a shell wrapper that runs the same DCG-style command guard as the agent's bash. Need a separate proposal.
2. **Cluster API discovery caching.** `kubectl` caches `/openapi/v3` and discovery docs under `~/.kube/cache/discovery/`. With no `~/.kube/`, this lands in `$HOME/.kube/cache/` inside the agent's home. Acceptable, but should be documented so users understand the working files.
3. **CRDs with credential-like fields.** A custom resource (e.g., `SealedSecret`, `ExternalSecret`) may carry sensitive material in `spec`. The current filter only blocks core `Secret`. Plausible v2: a configurable deny list of `(group, kind)` pairs. For v1, document the limitation.
4. **WebSocket upgrades on `/api/v1/.../log` follow mode.** `kubectl logs -f` upgrades to a streaming connection. The reverse proxy needs to support hijacked HTTP connections; standard Go `httputil.ReverseProxy` handles this but the filter must run before the upgrade.
5. **Multi-context users.** Should the gateway expose more than one context (e.g., "dev" and "staging") with separate filters? For v1 we say no — one container, one cluster, one namespace. Users who need both run two `claude-forge` instances.
6. **PreToolUse rule scope.** Should the hook also block `kubectl proxy`, `kubectl --server=...`, and similar attempts to bypass the configured server? Lean yes; cheap to add.

## 8. Rollout

1. Land this proposal doc (current PR).
2. Implement the gateway K8s proxy and the agent-side kubeconfig generation behind the `kubernetes.enabled: false` default. Ship as opt-in.
3. Add the PreToolUse hook.
4. Dogfood against a throwaway `kind` cluster in CI; add an e2e test that asserts `kubectl get pods` succeeds and `kubectl get secret` fails with 403.
5. Document the recommended least-privilege ServiceAccount in `secure-sandbox-architecture.md`.
6. Flip default to enabled once the proxy has settled and the e2e suite is green for two weeks.
