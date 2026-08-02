# gcp-mcp

A first-party, **read-only** Google Cloud MCP (Model Context Protocol) server for
claude-forge. It runs as a shared sidecar container and exposes a curated,
typed set of Google Cloud read operations as MCP tools over Streamable HTTP.

Unlike a generic `gcloud` passthrough, every tool is individually classified and
policy-gated, and the server **never returns secret payloads unless explicitly
opted in**.

It is **multi-project**: a single credential can target **any project it can
access**. `--project` only sets a default for calls that omit a `project`
argument; `list_projects` discovers accessible projects.

## Design principles

1. **Read-only by default.** Only non-mutating operations are reachable. Writes,
   if ever added, are gated behind `--allow-writes` (default off) and enforced by
   `policy.go`, not by individual handlers.
2. **Secrets are never returned by default.** Secret Manager *metadata* is
   readable, but a secret *payload* (`access_secret_version`) is blocked unless
   `--allow-secret-access` is set. All other tool output is passed through a
   redaction pass that strips credential-shaped strings (OAuth tokens, API keys,
   private keys), because secrets leak through metadata surfaces too (Cloud Run
   env vars, instance metadata, build substitutions).
3. **Defense in depth.** This server's policy is the *second* safety layer. The
   primary layer is **IAM**: run it as a service account with `roles/viewer` (or
   narrower). `--allow-writes`/`--allow-secret-access` are not a substitute for
   correct IAM — owner credentials still grant owner-level reads.
4. **No gcloud CLI, no shell.** All calls go directly to Google Cloud REST APIs
   via `golang.org/x/oauth2/google`, so the image stays small and there is no
   "agent constructs a shell command" surface.

## Tools

| Tool | Service | Type |
|------|---------|------|
| `list_projects` | Resource Manager | read |
| `describe_project` | Resource Manager | read |
| `get_iam_policy` | Resource Manager | read (reveals identities) |
| `search_resources` | Cloud Asset Inventory | read |
| `list_assets` | Cloud Asset Inventory | read |
| `list_instances` | Compute Engine | read |
| `describe_instance` | Compute Engine | read |
| `list_buckets` | Cloud Storage | read (metadata only) |
| `list_objects` | Cloud Storage | read (metadata only) |
| `list_services` | Cloud Run | read |
| `describe_service` | Cloud Run | read |
| `list_clusters` | GKE | read |
| `describe_cluster` | GKE | read |
| `list_secrets` | Secret Manager | read (metadata only) |
| `list_secret_versions` | Secret Manager | read (metadata only) |
| `access_secret_version` | Secret Manager | **secret — gated by `--allow-secret-access`** |
| `read_logs` | Cloud Logging | read (capped) |

## Credentials

The server reads **Application Default Credentials** from a read-only mount of
the host's `~/.config/gcloud`. ADC contains a refresh token, so access tokens
are minted and refreshed in-process — this removes the 1-hour access-token
expiry that makes Google's remote MCP endpoints unusable for long sessions.

Impersonation is supported via `--impersonate-service-account` or the
gcloud-compatible `CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT` env var. The
caller needs `roles/iam.serviceAccountTokenCreator` on the target SA.

## Usage

```
gcp-mcp [--addr :8084] [--project <id>] \
        [--impersonate-service-account <sa-email>] \
        [--quota-project <id>] \
        [--allow-writes] [--allow-secret-access]
```

- `--project` is an **optional** default; omit it to require an explicit
  `project` on each call so any accessible project can be targeted.
- `--quota-project` fixes the `x-goog-user-project` billing project; when unset,
  each call bills the project it targets.

## Docker

```
docker build -t gcp-mcp .
docker run -v ~/.config/gcloud:/home/user/.config/gcloud:ro \
  -e HOME=/home/user gcp-mcp --project my-project
```

## Development

```
cd mcp/gcp-mcp
go test -v -race ./...
```
