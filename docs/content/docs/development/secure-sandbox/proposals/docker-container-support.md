---
title: "Proposal: Docker Container Support in the Agent"
weight: 3
---

# Proposal: Docker Container Support in the Agent

**Status**: Implemented
**Depends on**: [Secure Sandbox Architecture]({{< relref "/docs/development/secure-sandbox/architecture/secure-sandbox-architecture" >}})

## 1. Motivation

Claude Code sessions routinely need to run containers: `docker build` to verify
an image change, `docker compose up` for integration tests, testcontainers-based
test suites, or a one-off `docker run` to try a tool. Today the agent image
ships the Docker CLI and daemon (`docker.io` is installed "for DinD") and the
architecture doc describes an in-agent `dockerd` (Design Decision 5), but the
runtime never wires it up: the agent container is not started privileged and no
`dockerd` is launched, so every `docker` invocation inside a session fails.

This proposal makes container support real, with two hard requirements:

1. The agent can run and stop containers of its own.
2. The agent **cannot** see, communicate with, start, stop, or inspect any
   container it did not start — not the host's containers, not claude-forge's
   own agent/gateway/MCP containers, and not those of other claude-forge
   sessions.

## 2. Goals and Non-Goals

### Goals

- `docker run` / `docker build` / `docker compose` / `docker stop` work inside
  the agent for containers the session itself creates.
- The agent has **no access to the host Docker API**: it cannot list, inspect,
  attach to, signal, or manage host-side containers, images, volumes, or
  networks — including the forge agent, gateway, and MCP sidecars of any
  session.
- Containers started by the agent are torn down with the session: they live
  inside the agent container, so removing the agent removes them.
- Opt-in and off by default, because the mechanism requires `--privileged`.

### Non-Goals

- Docker access for MCP sidecars or the gateway — only the agent gets a daemon.
- Sharing images or a build cache between sessions or with the host. Each
  session's daemon starts with an empty image store (pulls are repeated per
  session).
- Protecting the *host kernel* from a hostile agent beyond what `--privileged`
  allows (see Security Considerations).

## 3. Options Considered

### Option A — Mount the host Docker socket into the agent

Bind-mount `/var/run/docker.sock` and let the agent talk to the host daemon.

- ✅ Simplest; no privileged mode; shared image cache.
- ❌ **Violates requirement 2 completely.** The Docker API has no per-client
  namespacing: the agent could list, stop, or `docker exec` into the gateway,
  other sessions' agents, or any host container. Socket access is also
  root-equivalent on the host (`docker run -v /:/host`). Rejected outright.

### Option B — Host socket behind a filtering proxy

Front the host socket with a proxy (e.g. `docker-socket-proxy`) that whitelists
API endpoints.

- ✅ No privileged mode.
- ❌ Filtering is by *endpoint*, not by *container identity*: to let the agent
  stop its own containers, `POST /containers/*/stop` must be allowed — for every
  container on the host. Enforcing "only containers this session created" means
  writing and maintaining a custom stateful API proxy that tracks ownership
  across create/list/inspect/attach/exec/events, with any gap being a
  cross-session escape. High complexity, weak guarantee. Rejected.

### Option C — Per-session DinD sidecar (`docker:dind`)

Run the official `docker:dind` image as a privileged sidecar per session and
point the agent at it via `DOCKER_HOST=tcp://dind:2375`.

- ✅ Keeps `--privileged` off the agent container itself.
- ❌ No isolation gain: the agent fully controls that daemon anyway, and the
  privileged sidecar is exactly as exposed to the host as a privileged agent.
  Adds a container to manage per session, an unauthenticated TCP Docker API on
  the session network (reachable by other session sidecars), or TLS cert
  plumbing to avoid that. More moving parts for the same trust boundary.
  Rejected.

### Option D — In-agent dockerd (DinD), privileged agent — **chosen**

Start a dedicated `dockerd` inside the agent container. The host socket is
never mounted; the agent container runs `--privileged`.

- ✅ Satisfies both requirements structurally rather than by policy: the agent's
  daemon has its own container store, image store, and networks. There is no
  API path from the agent to the host daemon at all, so "cannot see / inspect /
  stop other containers" needs no filtering logic.
- ✅ Session-scoped lifecycle for free: the inner daemon and everything it runs
  die with the agent container.
- ✅ Already the documented design (Architecture Design Decision 5) and the
  agent image already ships `docker.io`.
- ❌ Requires `--privileged` (see Security Considerations). Mitigated by being
  opt-in, off by default.
- ❌ No image cache reuse across sessions.

### Option E — Rootless/unprivileged runtimes (rootless dockerd, podman, sysbox)

- Rootless dockerd or podman inside the container still needs most of the same
  relaxations in practice (the upstream `docker:dind-rootless` image documents
  that it requires `--privileged`), while adding uid-mapping and
  fuse-overlayfs failure modes across host kernels.
- [Sysbox](https://github.com/nestybox/sysbox) genuinely removes the need for
  `--privileged`, but requires installing a custom container runtime on every
  host — too heavy a prerequisite for a default path. Worth revisiting as an
  optional hardening if demand appears.

## 4. Design

Follows the `kubernetes.enabled` pattern: a config block gates the feature.

```yaml
# ~/.config/claude-forge/config.yaml
docker:
  enabled: true
```

Flow when enabled:

1. `Orchestrator.Start` sets `FORGE_ENABLE_DOCKER=1` in the agent environment
   and starts the agent container with `Privileged: true`
   (`internal/forge/orchestrator.go`). It also backs `/var/lib/docker` with an
   anonymous volume — the inner daemon cannot layer overlayfs on top of the
   agent's own overlayfs root (the same reason the official `docker:dind`
   image declares `VOLUME /var/lib/docker`). The volume is removed together
   with the agent container at session cleanup.
2. The agent entrypoint (`docker/agent/entrypoint.sh`), still running as root
   before dropping to the non-root user, launches `dockerd` in the background,
   waits up to 15s for `/var/run/docker.sock`, and adds `user` to the `docker`
   group. If the daemon fails to come up, it warns (with the tail of
   `/var/log/dockerd.log`) and the session continues without Docker rather than
   failing to start.
3. Claude Code runs as `user` with a working `docker` CLI talking to the
   in-container socket. `DOCKER_HOST` is unset — the default unix socket path
   is the inner daemon's.

When disabled (the default) nothing changes: no privileged mode, no daemon, and
`docker` commands fail as before.

## 5. Isolation Analysis

| Agent attempts | Result |
|---|---|
| `docker ps` / `docker inspect` on host containers | Impossible — the inner daemon has no knowledge of the host daemon's state; the host socket is not mounted |
| Stop/start/exec into the gateway, MCP sidecars, or another session's agent | Impossible — those exist only in the host daemon |
| Reach another session's containers over the network | Session networks are separate Docker bridge networks on the host; inner containers egress through the agent's network namespace, which is only attached to its own session network (and `forge-shared` when in use) |
| Fill disk / consume CPU via inner containers | Same blast radius as the agent process itself already has (bounded by the agent container's limits) |
| Escape to the host via `--privileged` | Out of scope for this boundary — see below |

The inverse also holds: `claude-forge stop`/`status` and `Cleanup` operate on
the host daemon and never see the inner containers; they are reclaimed
automatically when the agent container is removed.

## 6. Security Considerations

`--privileged` weakens the container-to-host boundary: a privileged container
has all capabilities and device access, so a *hostile* agent could in principle
escape to the host (e.g. by mounting host block devices). The isolation this
proposal provides is **API-level isolation of the Docker control plane** —
requirement 2 — not kernel-level containment of a malicious privileged process.

Accordingly:

- The feature is **off by default** and must be explicitly enabled per user.
- The host Docker socket remains unmounted in all configurations (existing
  invariant, unchanged).
- Users who need Docker *and* a hard host boundary should run claude-forge
  inside a VM, or revisit the sysbox option (Option E).

## 7. Testing

- Unit: config default/parse; orchestrator sets `Privileged` and
  `FORGE_ENABLE_DOCKER` iff `docker.enabled`.
- E2E (`test/e2e/forge`, tag `forge_e2e`): build the real agent image, start it
  privileged with `FORGE_ENABLE_DOCKER=1`, and verify `docker run hello-world`
  succeeds inside while the inner daemon sees none of the host's containers.
