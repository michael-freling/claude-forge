---
title: Kubernetes
weight: 2
---


`k8s-mcp` is a first-party Kubernetes MCP server
(`ghcr.io/michael-freling/claude-forge-k8s-mcp`) that ships in this repo but is
**not** a built-in integration — you wire it up like any other
[custom server]({{< relref "custom" >}}), as a `container` entry in
`mcp_servers`. It authenticates with **your own kubeconfig** and enforces a
fixed safety policy at the MCP layer.

Unlike a coarse read-only switch (which forbids all writes), it allows **read
and write** on ordinary resources while denying — regardless of what your
credential can do:

- `secrets` and `serviceaccounts` (read or write),
- the `rbac.authorization.k8s.io` and `admissionregistration.k8s.io` API groups,
- `pods/exec`, `pods/attach`, `serviceaccounts/token`, and `impersonate`,
- writes to cluster-scoped resources other than `nodes` (so cordon/drain work).

The carveout list lives in `mcp/k8s-mcp/policy.go` and is the canonical
definition of what the agent may touch.

## Configuration

Add it to `~/.config/claude-forge/config.yaml`:

```yaml
mcp_servers:
  - name: kube
    type: container
    scope: global                # one shared instance across sessions
    image: ghcr.io/michael-freling/claude-forge-k8s-mcp:latest
    port: 8080
    path: /mcp
    args: ["--addr=:8080", "--kubeconfig=/home/user/.kube/config"]
    env:
      HOME: /home/user
    mounts:
      - "~/.kube:/home/user/.kube:ro"
      - "~/.config/gcloud:/home/user/.config/gcloud:ro" # ADC, for GKE contexts
```

## Authentication

Credentials are taken straight from the mounted kubeconfig (bearer token or
embedded client certificate) — with one exception: **GKE kubeconfigs work**.
When the selected context uses the `gke-gcloud-auth-plugin` exec plugin (or
`--gcp-auth` forces it), the server authenticates with a Bearer token minted
in-process from Google Application Default Credentials and auto-refreshes it,
so the plugin binary is never needed. That path requires
`gcloud auth application-default login` on the host and the read-only
`~/.config/gcloud` mount shown above.

## Limitations

Other exec plugins (EKS `aws`, OIDC helpers) remain unsupported. A cluster
served at `localhost` is not reachable from the container network, and a
private GKE endpoint still needs a route (bastion/tunnel; `--network host`
when running standalone). Because the credential is your own (unrestricted)
one, the MCP policy is the *only* safety layer — if you need
defense-in-depth, point the kubeconfig at a user or ServiceAccount you have
scoped down with RBAC yourself.

## Migration from the built-in integration

Earlier versions shipped a built-in Kubernetes integration: a `kubernetes:`
section in `config.yaml` that ran the upstream `kubernetes-mcp-server` image
with claude-forge-minted ServiceAccount tokens, plus a
`claude-forge kube render` command that generated the matching RBAC
manifests. Both have been **removed**; the `mcp_servers` entry above is now
the only Kubernetes story.

To migrate:

- Replace your `kubernetes:` section with the `mcp_servers` entry shown above.
  A leftover `kubernetes:` block is harmless — config parsing is not strict,
  so unknown keys are simply ignored — but it no longer does anything.
- A leftover shared server container from an old binary can be removed with
  `docker rm -f forge-k8s-mcp` (it still shows up in `claude-forge status`).
- The `claude-forge-agent` ServiceAccount/ClusterRole/ClusterRoleBinding that
  `kube render` created in your clusters are no longer used; delete them with
  `kubectl` if you don't want them around.
