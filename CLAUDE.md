# Claude Code Guidelines for claude-code-tools

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
- `TestKubernetesMCPServer_Starts` — verifies the k8s MCP image starts with
  our flags. **Update this test whenever you change the flags in
  `orchestrator.startKubernetesMCP`.**

### Coverage threshold

CI enforces 90% coverage (excluding generated mocks). When adding new exported
functions, add corresponding tests before pushing.

## Container images

The project uses these container images (configured in `internal/forge/config/config.go`):

- **Agent**: runs Claude Code
- **Gateway**: git proxy for GitHub access
- **GitHub MCP**: per-session MCP sidecar scoped to one repo
- **Kubernetes MCP**: shared singleton for cluster access (`ghcr.io/containers/kubernetes-mcp-server`)

When changing flags passed to any container image, check the image's `--help`
output to verify the flags exist. The kubernetes-mcp-server in particular has
changed its CLI interface across versions.

## Architecture

- `cmd/claude-forge/main.go` — CLI commands (start, resume, init, etc.)
- `internal/forge/orchestrator.go` — container lifecycle (Start, Cleanup, startKubernetesMCP)
- `internal/forge/container/client.go` — Docker API wrapper
- `internal/forge/session/` — session listing and JSONL parsing
- `internal/forge/kube/` — kubeconfig generation and RBAC rendering

## Key invariants

- Kubernetes MCP is always `--read-only` (security invariant, not configurable)
- MCP servers are only written to `settings.json` when actually running
- `UpdateMCPServers` replaces the map entirely (no stale entries from prior sessions)
