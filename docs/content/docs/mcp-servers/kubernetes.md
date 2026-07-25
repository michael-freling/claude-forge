---
title: Kubernetes
weight: 2
---


When `kubernetes.enabled` is set, claude-forge runs a shared Kubernetes MCP
server that Claude Code can use to inspect and operate on your cluster. Access
is constrained by RBAC rather than by disabling destructive operations — the
agent's permissions are exactly what you grant its service account.

Generate the ServiceAccount, ClusterRole, and ClusterRoleBinding (with safe
carveouts — no secrets, no exec, no RBAC tampering) and apply them:

```bash
claude-forge kube render --context dev | kubectl apply -f -
```

See the `kubernetes` section of the
[configuration reference]({{< relref "/docs/configuration" >}}) for the
available options.

## Token lifetime and refresh

The MCP server authenticates to each cluster with a ServiceAccount token
minted on the host via `kubectl create token`. Tokens expire — most API
servers cap the lifetime at 1–48 hours — while Claude Code sessions can stay
open for days, so claude-forge keeps them fresh automatically:

- **At every session start**, tokens are re-minted, even when the shared MCP
  server container is already running. If the `kubernetes.contexts`
  configuration changed since the server started, the server is recreated so
  the new contexts take effect.
- **While a session runs**, the CLI refreshes them every 15 minutes, or every
  half token lifetime if that is shorter.
- The generated kubeconfig references each token via `tokenFile` from a
  directory mounted into the container, and the server's Kubernetes client
  re-reads that file on its own — rotation requires no container restart.

`kubernetes.token_duration` sets the lifetime *requested* for each token
(default `24h`, minimum `10m`, any Go duration string). The API server may
grant less — EKS caps tokens at 24h, GKE at 48h, and a vanilla cluster caps at
its `--service-account-max-token-expiration` — in which case the request is
shortened, not rejected; if a cluster rejects the request outright,
claude-forge falls back to the server-default lifetime. A longer duration
widens the safety margin for times when no refresh can run, such as the host
being asleep.

If your clusters grant long lifetimes and you prefer effectively long-lived
credentials, set for example `token_duration: 168h`. Either way the
ServiceAccount's RBAC — not the token lifetime — remains the safety boundary.

On a multi-user host, note the on-disk trade-off: the MCP container runs as a
non-host uid, so token files must be world-readable (0644) inside
execute-only (0711) directories. Their names are salted with a 0600
`.token-salt` file, so other local users cannot list or derive them — but
anyone who obtains a token path (or root) can read a live, RBAC-scoped
cluster credential from `~/.config/claude-forge/k8s-mcp/tokens/`.
