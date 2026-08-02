---
title: Google Cloud
weight: 3
---


`gcp-mcp` is a standalone, **read-only** Google Cloud MCP server that ships in
this repo but is **not** a built-in integration — you wire it up like any other
[custom server]({{< relref "custom" >}}), as a `container` entry in
`mcp_servers`. It exposes a curated, typed set of read operations (not a
`gcloud` passthrough) and **never returns secret payloads unless you explicitly
opt in**.

It is **multi-project**: a single credential can target **any project it can
access**. `list_projects` discovers them, and every tool takes an optional
`project` argument.

## Configuration

Add it to `~/.config/claude-forge/config.yaml`:

```yaml
mcp_servers:
  - name: gcp
    type: container
    scope: global                 # one shared instance across sessions
    image: ghcr.io/michael-freling/claude-forge-gcp-mcp:latest
    port: 8084
    path: /mcp
    # Optional default project. Omit it entirely to require an explicit
    # `project` on each call, so any accessible project can be targeted.
    args: ["--project", "my-project"]
    env:
      HOME: /home/user
      CLOUDSDK_CONFIG: /home/user/.config/gcloud
    mounts:
      - "~/.config/gcloud:/home/user/.config/gcloud:ro"
```

`HOME` must be the parent of the mounted gcloud directory so the credential
lookup finds `application_default_credentials.json` at the well-known path.

### Server flags (passed via `args`)

| Flag | Purpose |
|------|---------|
| `--project <id>` | Default project for calls that omit a `project` argument. Omit to require one per call. |
| `--quota-project <id>` | Fix the project billed for quota (`x-goog-user-project`). Default: bill each call's target project. |
| `--impersonate-service-account <email>` | Impersonate a service account (or set `CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT` in `env`). |
| `--allow-secret-access` | Allow `access_secret_version` to return Secret Manager **payloads**. Off by default. |
| `--allow-writes` | Allow mutating tools (none ship today). Off by default. |

## Safety model

Access is constrained by **IAM first, the server policy second**.

1. **IAM is the primary layer.** Run it as a service account with `roles/viewer`
   (or narrower). Read-only is then enforced by Google and cannot be talked
   around. `roles/viewer` also does not grant `secretmanager.versions.access`,
   so payloads are already out of reach.
2. **The server policy is the second layer.** Every tool is classified read or
   write; writes require `--allow-writes`. Reading a secret *payload* is
   separately gated behind `--allow-secret-access`. The two gates are
   independent — enabling one never enables the other, and both default closed.
3. **Redaction backs both up.** All non-secret output is filtered for
   credential-shaped strings (OAuth tokens, API keys, private keys), because
   secrets leak through metadata surfaces too (a Cloud Run env var, an
   instance's metadata, a build substitution).

The flags are **not** a substitute for correct IAM — a server run with owner
credentials gets owner-level reads regardless.

## IAM setup

Create a read-only service account and let yourself impersonate it (no key file
to manage):

```bash
PROJECT=my-project
SA=claude-forge-gcp@$PROJECT.iam.gserviceaccount.com

gcloud iam service-accounts create claude-forge-gcp --project "$PROJECT"
gcloud projects add-iam-policy-binding "$PROJECT" \
  --member "serviceAccount:$SA" --role roles/viewer
gcloud iam service-accounts add-iam-policy-binding "$SA" \
  --member "user:$(gcloud config get-value account)" \
  --role roles/iam.serviceAccountTokenCreator
```

Then add `--impersonate-service-account claude-forge-gcp@my-project.iam.gserviceaccount.com`
to `args`. For **multiple** projects, grant the same service account `roles/viewer`
on each project you want reachable.

## Credentials and refresh

The server reads **Application Default Credentials** from the read-only
`~/.config/gcloud` mount. Authenticate once on the host:

```bash
gcloud auth application-default login
```

ADC contains a refresh token, so access tokens are minted and refreshed
**in-process** for as long as the server runs — long Claude Code sessions stay
authenticated with no 1-hour expiry. (Use `application-default login`, not plain
`gcloud auth login`, which only configures the CLI.)

The container runs as a non-host uid, so `application_default_credentials.json`
must be readable by it. gcloud writes it `0600`; if the container uid differs
from yours, make it readable (`chmod 644`) or credential loading fails.

## Tools

| Tool | Service | Notes |
|------|---------|-------|
| `list_projects`, `describe_project` | Resource Manager | `list_projects` discovers accessible projects |
| `get_iam_policy` | Resource Manager | reveals principal identities |
| `search_resources`, `list_assets` | Cloud Asset Inventory | broadest discovery |
| `list_instances`, `describe_instance` | Compute Engine | |
| `list_buckets`, `list_objects` | Cloud Storage | metadata only, never object contents |
| `list_services`, `describe_service` | Cloud Run | |
| `list_clusters`, `describe_cluster` | GKE | |
| `list_secrets`, `list_secret_versions` | Secret Manager | **metadata only** |
| `access_secret_version` | Secret Manager | **gated by `--allow-secret-access`, off by default** |
| `read_logs` | Cloud Logging | capped result size |

Every project-scoped tool accepts a `project` argument that targets any
accessible project; it is required only when the server was started without a
default `--project`. List and asset queries paginate (`page_size`/`max_results`
+ `page_token`) and are size-capped to protect the model's context.
