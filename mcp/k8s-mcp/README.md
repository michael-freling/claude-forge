# k8s-mcp

A first-party Kubernetes MCP server for claude-forge that authenticates with a
mounted kubeconfig and enforces, **at the MCP layer**, the same permission model
that `claude-forge kube render` bakes into RBAC — so you can point it at your
own (unrestricted) local credential and still get the curated, safe surface.

Unlike the upstream `kubernetes-mcp-server`'s coarse `--read-only` switch (which
forbids *all* writes), this server allows read **and write** on ordinary
resources while denying the sensitive surfaces, matching what a rendered
ServiceAccount could do.

## Permission model (mirrors `internal/forge/kube/carveouts.go`)

Enforced by `policy.go` on every operation, before any API call:

- **Denied entirely** (read and write): `secrets`, `serviceaccounts`; the
  `rbac.authorization.k8s.io` and `admissionregistration.k8s.io` API groups;
  the `pods/exec`, `pods/attach`, and `serviceaccounts/token` subresources; and
  the `impersonate` verb.
- **Cluster-scoped resources are read-only**, except `nodes` (writable, for
  cordon/drain).
- **Allowed**: read/write on ordinary namespaced resources, plus `pods/log`.

The server never offers an exec/attach tool at all.

## Tools

| Tool | Description | Type |
|------|-------------|------|
| `list_resources` | List resources of a kind (all namespaces if none given) | read |
| `get_resource` | Get a single resource | read |
| `apply_resource` | Create or update from a YAML/JSON manifest | write |
| `delete_resource` | Delete a resource | write |
| `get_logs` | Pod logs (capped) | read |
| `list_api_resources` | Discover served apiVersion/kind | read |

## Usage

```
k8s-mcp [--addr :8080] [--kubeconfig <path>] [--context <name>]
```

`--kubeconfig` defaults to `$KUBECONFIG`, then `~/.kube/config`. Auth is taken
straight from the kubeconfig (bearer token or embedded client certificate);
exec-plugin auth (GKE/EKS/OIDC) is not supported inside the container, and a
cluster served at `localhost` is not reachable from the container network.

## As a claude-forge MCP server

Run it as a global-scope container server in `~/.config/claude-forge/config.yaml`:

```yaml
mcp_servers:
  - name: kube
    type: container
    scope: global
    image: ghcr.io/michael-freling/claude-forge-k8s-mcp:latest
    port: 8080
    path: /mcp
    args: ["--addr=:8080", "--kubeconfig=/home/user/.kube/config"]
    mounts:
      - "~/.kube:/home/user/.kube:ro"
```

## Development

```
cd mcp/k8s-mcp
go test -v -race ./...
```
