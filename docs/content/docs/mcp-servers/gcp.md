---
title: Google Cloud
weight: 3
---


When `gcp.enabled` is set, claude-forge runs a shared, **read-only** Google Cloud
MCP server that Claude Code can use to inspect your projects, resources, and
logs. It is a first-party server — a curated, typed set of read operations, not
a `gcloud` command passthrough — and it **never returns secret payloads unless
you explicitly opt in**.

See the `gcp` section of the
[configuration reference]({{< relref "/docs/configuration" >}}) for all options.

## Safety model

Access is constrained by **IAM first, the server policy second** — the same
defense-in-depth stance the Kubernetes MCP takes with RBAC.

1. **IAM is the primary layer.** Run the server as a service account with
   `roles/viewer` (or narrower). Read-only is then enforced by Google and cannot
   be talked around by a cleverly constructed request. `roles/viewer` also does
   **not** grant `secretmanager.versions.access`, so secret payloads are already
   out of reach.
2. **The server policy is the second layer.** Every tool is classified read or
   write, and writes require `--allow-writes` (off by default; no write tools
   ship today). Reading a secret *payload* is separately gated behind
   `--allow-secret-access` (also off by default). The two gates are independent —
   enabling one never enables the other.
3. **Redaction backs both up.** All non-secret tool output passes through a
   redaction filter that strips credential-shaped strings (OAuth tokens, API
   keys, private keys), because secrets leak through metadata surfaces too — a
   Cloud Run env var, an instance's metadata, a build substitution.

`allow_writes` / `allow_secret_access` are **not** a substitute for correct IAM.
A server run with owner credentials gets owner-level reads regardless.

## IAM setup

Create a dedicated service account, grant it read-only access, and let yourself
impersonate it (so no long-lived key file is needed):

```bash
PROJECT=my-project
SA=claude-forge-gcp@$PROJECT.iam.gserviceaccount.com

# 1. Create the service account
gcloud iam service-accounts create claude-forge-gcp \
  --project "$PROJECT" --display-name "claude-forge read-only"

# 2. Grant it read-only access to the project
gcloud projects add-iam-policy-binding "$PROJECT" \
  --member "serviceAccount:$SA" --role roles/viewer

# 3. Let yourself mint tokens for it (impersonation; no key file to manage)
gcloud iam service-accounts add-iam-policy-binding "$SA" \
  --member "user:$(gcloud config get-value account)" \
  --role roles/iam.serviceAccountTokenCreator
```

Then point claude-forge at it in `config.yaml`:

```yaml
gcp:
  enabled: true
  project: my-project
  impersonate_service_account: claude-forge-gcp@my-project.iam.gserviceaccount.com
```

## Credentials and refresh

The server reads **Application Default Credentials** from a read-only mount of
your host's `~/.config/gcloud`. Authenticate once on the host:

```bash
gcloud auth application-default login
```

ADC contains a refresh token, so access tokens are minted and refreshed
**in-process** for as long as the server runs. This is the whole point: it
removes the 1-hour access-token expiry that makes Google's remote MCP endpoints
unusable across long Claude Code sessions.

Impersonation is configured with `impersonate_service_account` (or the
gcloud-compatible `CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT` env var). The
impersonating identity — your ADC principal — needs
`roles/iam.serviceAccountTokenCreator` on the target service account (step 3
above).

The MCP container runs as a non-host uid, so the mounted
`application_default_credentials.json` must be readable by it. gcloud writes that
file `0600`; if the container's uid differs from yours, make it group/other
readable (`chmod 644`) or the server will fail to load credentials.

## Tools

| Tool | Service | Notes |
|------|---------|-------|
| `list_projects`, `describe_project` | Resource Manager | |
| `get_iam_policy` | Resource Manager | reveals principal identities |
| `search_resources`, `list_assets` | Cloud Asset Inventory | broadest discovery |
| `list_instances`, `describe_instance` | Compute Engine | |
| `list_buckets`, `list_objects` | Cloud Storage | metadata only, never object contents |
| `list_services`, `describe_service` | Cloud Run | |
| `list_clusters`, `describe_cluster` | GKE | |
| `list_secrets`, `list_secret_versions` | Secret Manager | **metadata only** |
| `access_secret_version` | Secret Manager | **gated by `allow_secret_access`, off by default** |
| `read_logs` | Cloud Logging | capped result size |

List and asset queries support paging (`page_size`/`max_results` and
`page_token`) and are size-capped, so a broad query returns a manageable slice
rather than blowing out the model's context.

## Enabling secret access (opt-in)

Reading a secret's *value* is off by default. If a workflow genuinely needs it,
set the gate — and remember the service account still needs
`secretmanager.versions.access` in IAM (which `roles/viewer` does not grant):

```yaml
gcp:
  enabled: true
  project: my-project
  allow_secret_access: true
```
