# Claude Code Guidelines for claude-forge

## Testing

### E2E tests for container startup

When modifying container startup commands (CLI flags, image references, mount
paths), always verify the change works against the real container image. Unit
tests mock the container manager and cannot catch flag incompatibilities with
upstream images.

E2E tests live in `test/e2e/forge/e2e_test.go` (build tag `forge_e2e`).
Run locally with:

```bash
go test -tags=forge_e2e -v -race -timeout 15m ./test/e2e/forge/...
```

Key tests:
- `TestForgeStart` — full start-to-finish session with Claude Code
- `TestAgentDockerInDocker` — verifies the agent's opt-in DinD (`docker.enabled`):
  the in-container dockerd works and cannot see host containers

### Coverage threshold

CI enforces 90% coverage (excluding generated mocks). When adding new exported
functions, add corresponding tests before pushing.

## Container images

The project uses these container images (configured in `internal/forge/config/config.go`):

- **Agent**: runs Claude Code
- **Gateway**: git proxy for GitHub access
- **GitHub MCP**: per-session MCP sidecar scoped to one repo

First-party MCP server images (`mcp/k8s-mcp/`, `mcp/gcp-mcp/`) are not built
in: users run them as generic `mcp_servers` container entries.

When changing flags passed to any container image, check the image's `--help`
output to verify the flags exist.

## Documentation

- `README.md` — condensed overview (features, install, quick start) linking to
  the docs site. Detailed reference material does not belong here.
- `docs/` — Hugo site (theme: hextra submodule) deployed to GitHub Pages by
  `.github/workflows/docs.yml`. `make docs-serve` previews it locally.
  - `docs/content/docs/` — the **user guide**, published. This is the canonical
    detailed documentation: when a CLI command, flag, or config field changes,
    update these pages (the `update-readme` skill covers both files).
  - `docs/content/docs/development/` — internal architecture research and
    proposals. **Not published**: `docs/config/production/hugo.yaml` excludes
    it from the production build via a `build: {render: never, list: never}`
    cascade. Preview locally with `hugo server` in `docs/` (development
    environment renders everything).

## Architecture

- `cmd/claude-forge/main.go` — CLI commands (start, resume, init, etc.)
- `internal/forge/orchestrator.go` — container lifecycle (Start, Cleanup, sidecars)
- `internal/forge/container/client.go` — Docker API wrapper
- `internal/forge/session/` — session listing and JSONL parsing

## Key invariants

- `mcp/k8s-mcp/policy.go` is the canonical carveout list for Kubernetes access
  (the policy is enforced at the MCP layer, not via RBAC)
- MCP servers are only written to `settings.json` when actually running
- `UpdateMCPServers` replaces the map entirely (no stale entries from prior sessions)
- The host Docker socket is never mounted into the agent; the agent runs
  unprivileged unless `docker.enabled` is set (DinD needs `--privileged`)
