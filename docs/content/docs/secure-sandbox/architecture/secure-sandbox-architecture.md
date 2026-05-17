---
title: "Secure Sandbox Architecture"
weight: 2
---

# Secure Sandbox Architecture

## 1. Overview

`claude-forge` is a Go CLI that launches Claude Code inside an isolated Docker container, pre-authenticated and ready to work on the user's project. A companion gateway container mediates git and GitHub operations — allowing read access (clone, pull, fetch) to any repository but restricting push operations to only the current project's repository. Multiple instances can run in parallel across different project directories. The command installs locally via `go install` — container images are pulled from GHCR.io (nightly rebuilt with cosign signing for security verification).

For the threat model and technology survey, see the [Secure Sandbox Environments Research]({{< relref "/docs/secure-sandbox/secure-sandbox-environments" >}}).

---

## 2. Architecture Diagram

```
┌──────────────────────────────────────────────────────────────────────────┐
│  Host Machine (Linux only)                                               │
│                                                                          │
│  $ claude-forge start                (N instances in parallel)           │
│                                                                          │
│  ┌────────────────────────────────────────────────────────────────────┐  │
│  │  Docker Networks (per-instance: forge-<project-id>-<session>)      │  │
│  │                                                                    │  │
│  │  ┌───────────────────────────────┐  ┌───────────────────────────┐  │  │
│  │  │ Container: Agent              │  │ Container: Gateway        │  │  │
│  │  │                               │  │                           │  │  │
│  │  │ Claude Code                   │  │ HTTP proxy for            │  │  │
│  │  │ --dangerously-skip-permissions│  │ github.com traffic        │  │  │
│  │  │                               │  │                           │  │  │
│  │  │ Git (HTTPS, via proxy):       │  │ git-upload-pack (read):   │  │  │
│  │  │  fetch/clone/pull ──────────► │  │  → allowed, any repo     │  │  │
│  │  │  push ──────────────────────► │  │                           │  │  │
│  │  │                               │  │ git-receive-pack (push):  │  │  │
│  │  │ GitHub API (via proxy):       │  │  → only project repo     │  │  │
│  │  │  read ops ──────────────────► │  │                           │  │  │
│  │  │  write ops ─────────────────► │  │ GitHub API:               │  │  │
│  │  │                               │  │  read → any repo          │  │  │
│  │  │ gh (→ forge-gh) via gateway:  │  │  write → only project repo│  │  │
│  │  │  schema discovery ──────────► │  │                           │  │  │
│  │  │                               │  │ Auth: host ~/.ssh/ (ro)   │  │  │
│  │  │ Internet (direct):            │  │       + ~/.config/gh/ (ro)│  │  │
│  │  │  npm, pip, go get ────────►   │  │                           │  │  │
│  │  │  (no proxy)           internet│  │ forge-gh API (:8083)      │  │  │
│  │  │                               │  │ + schema discovery        │  │  │
│  │  │ No MCP servers                │  │                           │  │  │
│  │  │ --privileged (for DinD)       │  │                           │  │  │
│  │  │ non-root user                 │  │                           │  │  │
│  │  └───────────────────────────────┘  └────────────┬──────────────┘  │  │
│  │                                                   │                │  │
│  │  forge_net (internal + internet for agent)        │                │  │
│  └───────────────────────────────────────────────────┼────────────────┘  │
│                                                      │                  │
│  Host state:                                         ▼                  │
│  ~/.config/claude-forge/config.yaml                 GitHub              │
│  ~/.config/claude-forge/settings.json              (project repo only   │
│  ~/.config/claude-forge/gitconfig                   for push/write)     │
│  ~/.claude-forge/<project-id>/ (history)                                 │
│  ~/.claude/.credentials.json (read at startup)                           │
│  ~/.claude/ (skills, agents, rules, commands)                            │
│                                                                          │
└──────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Command Interface

The command lives at `cmd/claude-forge/main.go`.

```
claude-forge
├── start         Start a new Claude Code session in the sandbox
├── resume        Resume a past session or list available sessions
├── stop          Stop running instance(s) for this project
├── status        Show running instances across all projects
├── build         Force pull/rebuild the agent image
├── auth          Verify Claude Code authentication is available
└── version       Print version
```

### Primary Usage

```bash
# Common case: zero config, launches immediately
$ cd ~/projects/my-app
$ claude-forge start
Starting gateway... ok
Starting agent... ok
Claude Code is ready.

# With a worktree (Claude Code's built-in --worktree)
$ claude-forge start --worktree

# With a prompt
$ claude-forge start --prompt "Fix the failing tests in pkg/auth/"

# Opt out of --dangerously-skip-permissions
$ claude-forge start --no-skip-permissions

# List past sessions for this project
$ claude-forge resume --list
SESSION ID    CREATED              FIRST MESSAGE
abc123        2026-05-08 14:30     "Fix auth middleware..."
def456        2026-05-07 09:15     "Add unit tests for..."

# Resume a specific session
$ claude-forge resume abc123

# Resume most recent session
$ claude-forge resume

# Verify auth
$ claude-forge auth
✓ Found credentials at ~/.claude/.credentials.json
✓ Token valid (expires 2027-04-15)

# Stop instance for this project
$ claude-forge stop

# Show all running instances
$ claude-forge status
PROJECT                        CONTAINER        STATUS    UPTIME
~/projects/my-app              forge-a1b2c3     running   15m
~/projects/other-project       forge-d4e5f6     running   3h
```

### Flags for `start`

| Flag | Default | Description |
|---|---|---|
| `--worktree` | `false` | Pass `--worktree` to Claude Code (creates isolated git worktree) |
| `--no-skip-permissions` | `false` | Don't pass `--dangerously-skip-permissions` |
| `--prompt` | `""` | Pass a prompt to Claude Code |

### Flags for `resume`

| Flag | Default | Description |
|---|---|---|
| `--list` | `false` | List available sessions instead of resuming |

---

## 4. Authentication

Claude-forge reuses the user's existing Claude Code credentials. It does NOT generate separate tokens.

### Token Resolution Order

```
1. Check: ANTHROPIC_API_KEY in environment?
   ├── Yes → use it
   └── No ──▼

2. Check: CLAUDE_CODE_OAUTH_TOKEN in environment?
   ├── Yes → use it
   └── No ──▼

3. Check: ~/.claude/.credentials.json exists?
   ├── Yes → read access_token and refresh_token
   │         Pass access_token as CLAUDE_CODE_OAUTH_TOKEN to container
   │         Store refresh_token for token refresh
   └── No ──▼

4. Error: "No Claude Code credentials found.
          Run 'claude' on the host to log in first, or set ANTHROPIC_API_KEY."
```

Claude-forge does NOT run `claude setup-token` itself. The user must have authenticated with Claude Code on the host first (by running `claude` normally). This avoids generating extra tokens and the associated refresh-token race conditions.

### Refresh Token Handling

The `~/.claude/.credentials.json` file is bind-mounted (read-write) into the container at `/home/user/.claude/.credentials.json`. This allows Claude Code inside the container to refresh expired OAuth tokens autonomously during long-running sessions, using the same refresh flow it uses on the host.

---

## 5. Container 1: Agent

| Attribute | Value |
|---|---|
| **Image** | `ghcr.io/michael-freling/claude-forge-agent:latest` |
| **Security** | `--privileged` (required for DinD), non-root user |
| **Network** | `forge_net_<id>` — has direct internet access for package downloads. GitHub traffic (github.com, api.github.com) routes through gateway via git proxy config. |
| **Name** | `forge-agent-<project-id>-<session-id>` (unique per instance) |

### Volumes

| Host path | Container path | Mode | Purpose |
|---|---|---|---|
| Project dir | `/work` | `rw` | Workspace |
| `~/.claude-forge/<project-id>/` | `/home/user/.claude/projects/` | `rw` | Per-project session history and memory (covers both `-work/` and any `-work-.claude-worktrees-<name>/` buckets Claude Code creates) |
| `~/CLAUDE.md` | `/home/user/CLAUDE.md` | `ro` | User-level instructions (if exists) |
| `~/.claude/CLAUDE.md` | `/home/user/.claude/CLAUDE.md` | `ro` | User-level instructions (if exists) |
| `~/.claude/rules/` | `/home/user/.claude/rules/` | `ro` | User-level rules |
| `~/.claude/agents/` | `/home/user/.claude/agents/` | `ro` | User-level sub-agents |
| `~/.claude/commands/` | `/home/user/.claude/commands/` | `ro` | User-level commands |
| `~/.claude/skills/` | `/home/user/.claude/skills/` | `ro` | User-level skills |
| `~/.claude/plugins/` | `/home/user/.claude/plugins/` | `ro` | User-level Claude Code plugins (if exists) |
| `~/.config/claude-forge/settings.json` | `/home/user/.claude/settings.json` | `ro` | Claude Code config (if exists) |
| `~/.config/claude-forge/gitconfig` | `/home/user/.gitconfig` | `ro` | Git config (user identity + proxy routing + `worktree.useRelativePaths`) |

### Environment Variables

```yaml
environment:
  CLAUDE_CODE_OAUTH_TOKEN: "<from credentials>"
  # or: ANTHROPIC_API_KEY: "<from host env>"
  HOME: "/home/user"
  GIT_TERMINAL_PROMPT: "0"
```

### Git Configuration

The git configuration is stored at `~/.config/claude-forge/gitconfig` on the host and mounted into the container as `/home/user/.gitconfig`. It routes GitHub traffic through the gateway:

```ini
[http "https://github.com"]
    proxy = http://gateway:8080

[http "https://api.github.com"]
    proxy = http://gateway:8080

[user]
    name = <from host git config>
    email = <from host git config>
```

This means:
- `git clone https://github.com/any/repo` → routed through gateway (gateway adds auth, allows read)
- `git push origin main` → routed through gateway (gateway validates target repo)
- `npm install` / `pip install` / `go get` → direct internet (no proxy)

### Claude Code Startup Command

```bash
claude --dangerously-skip-permissions [--worktree] [--resume <id>] [-p "<prompt>"]
```

### What is NOT in this Container

- No host SSH keys (`~/.ssh/`)
- No host `~/.claude/.credentials.json` (token passed via env var at startup)
- No host git credentials (PAT, credential stores)
- No MCP server configurations
- No `~/.kube/config`, `~/.config/gcloud/`, or cloud credentials
- No host Docker socket

`gh` is available in the container as an alias for `forge-gh`, which communicates with the gateway API for all GitHub operations.

---

## 6. Container 2: Gateway

| Attribute | Value |
|---|---|
| **Image** | `ghcr.io/michael-freling/claude-forge-gateway:latest` |
| **Purpose** | HTTP proxy for github.com (git + API) with repo-scoped push enforcement |
| **Network** | `forge_net_<id>` (internal, shared with agent) + direct internet |
| **Name** | `forge-gateway-<project-id>-<session-id>` |
| **Ports** | `8080` (HTTP proxy for git), `8083` (GitHub API server) |

### HTTP Proxy (Port 8080): Git Operation Enforcement

The gateway acts as an HTTP proxy specifically for github.com traffic. Agent's git is configured to route github.com requests through this proxy. The gateway inspects the git smart HTTP protocol (recent versions only; no backwards compatibility with legacy protocols):

| Git operation | HTTP endpoint | Gateway behavior |
|---|---|---|
| `git clone` / `git fetch` / `git pull` | `POST /repo.git/git-upload-pack` | **Allowed for any repo.** Gateway injects auth and forwards. |
| `git push` | `POST /repo.git/git-receive-pack` | **Allowed only for project repo.** Otherwise returns 403. |
| Info/refs discovery | `GET /repo.git/info/refs?service=...` | Allowed for any repo (read) or project-only (push). |

The gateway authenticates with GitHub on behalf of the agent using the host's credentials. It supports:
- SSH key-based auth (translates to HTTPS using SSH-based credential lookup)
- PAT from `~/.config/gh/hosts.yml`
- OAuth token from `gh auth`

### GitHub API Server (Port 8083): Schema Discovery and `forge-gh` Interface

The gateway exposes a REST API with an OpenAPI-style schema discovery endpoint. The `forge-gh` binary in the agent container (aliased as `gh`) queries this endpoint to self-discover available operations.

**Schema discovery:**

```
GET /api/schema
```

Returns an OpenAPI-style response describing all available endpoints, their methods, parameters, and whether they are read or write operations. When `forge-gh` is called with any command, it:

1. Queries `GET /api/schema` from the gateway to discover available operations.
2. Maps the requested command to the appropriate API endpoint.
3. Executes the request against the gateway, which enforces read/write policies.

This approach means:
- New GitHub operations can be added to the gateway without updating the `forge-gh` binary.
- The agent can discover what operations are available at runtime.
- The gateway remains the single source of truth for what is allowed.

**Policy enforcement:**

- **Read operations** (list PRs, view issues, repo metadata, CI status, etc.) are allowed for any repository.
- **Write operations** (create PR, comment, merge, create issue, etc.) are allowed only when the target matches the project's repository.

The gateway enforces this policy regardless of how the operation is discovered or invoked.

### Allowed Repository Identification

At startup, claude-forge reads:

```bash
git remote get-url origin
# → git@github.com:michael-freling/claude-code-tools.git
# or: https://github.com/michael-freling/claude-code-tools.git
```

Normalized to `owner=michael-freling`, `repo=claude-code-tools`. Passed to gateway at startup.

### Volumes

| Host path | Container path | Mode | Purpose |
|---|---|---|---|
| `~/.ssh/` | `/home/user/.ssh/` | `ro` | SSH keys (for authenticating with GitHub) |
| `~/.config/gh/` | `/home/user/.config/gh/` | `ro` | GitHub CLI auth config (tokens, hosts) |

---

## 7. Host-Side State

### Per-Project Session Storage

```
~/.claude-forge/
└── <project-id>/                 ← mangled host project path
    ├── -work/                    ← Claude Code's bucket for cwd=/work
    │   ├── <session-id>.jsonl    ← conversation transcript
    │   └── memory/
    │       ├── MEMORY.md         ← auto memory
    │       └── *.md              ← topic files
    └── -work-.claude-worktrees-<name>/   ← bucket for each --worktree cwd
        └── <session-id>.jsonl
```

`<project-id>` on the host is the mangled absolute path of the project on the host (e.g. `-home-user-foo`), which gives each project its own session directory.

The host path `~/.claude-forge/<project-id>/` is mounted into the container at `/home/user/.claude/projects/` (the parent). Claude Code in the container writes session files under a subdirectory derived from its cwd — `-work/` for the main workspace and `-work-.claude-worktrees-<name>/` for each worktree — so all of those buckets persist to the host through a single bind mount.

### Session Listing for `resume`

`claude-forge resume --list` walks one level of subdirectories under `~/.claude-forge/<project-id>/` and reads every `.jsonl`:
- Parses each file for session ID (filename), creation timestamp (first entry), and first user message
- Surfaces both main-workspace and worktree sessions in one table sorted by recency

`claude-forge resume <id>` starts the container with `claude --resume <id>`.

`claude-forge resume` (no argument) starts with `claude --continue` (most recent session).

### Config Files

**1. `~/.config/claude-forge/config.yaml`** — claude-forge settings:

```yaml
# All fields optional — zero config for common case.

# Override container images
images:
  agent: "ghcr.io/michael-freling/claude-forge-agent:latest"
  gateway: "ghcr.io/michael-freling/claude-forge-gateway:latest"

# Default behavior
defaults:
  skip_permissions: true    # pass --dangerously-skip-permissions
  worktree: false
```

**2. `~/.config/claude-forge/settings.json`** — Claude Code config for container:

```json
{
  "hasCompletedOnboarding": true,
  "autoUpdaterStatus": "disabled"
}
```

Mounted as `/home/user/.claude/settings.json`. Optional — if absent, Claude Code uses defaults.

**3. `~/.config/claude-forge/gitconfig`** — Git configuration for container:

```ini
[http "https://github.com"]
    proxy = http://gateway:8080

[http "https://api.github.com"]
    proxy = http://gateway:8080

[user]
    name = Michael Freling
    email = user@example.com
```

Mounted as `/home/user/.gitconfig`. Contains user identity and proxy routing for GitHub traffic. The user should configure their name and email here (or claude-forge can auto-populate from the host's `git config` on first run).

---

## 8. Worktree Mode

`claude-forge start --worktree` passes `--worktree` to Claude Code inside the container. Claude Code handles everything:

1. Creates a worktree at `/work/.claude/worktrees/<name>/` (which is on the host filesystem via bind mount).
2. Works in the worktree — isolated from the main branch.
3. On interactive exit: auto-removes worktree if no uncommitted changes.

### Orphaned Worktree Cleanup

Claude Code cleans orphaned worktrees automatically on startup:
- Checks `.claude/worktrees/` in the project directory.
- Worktrees older than 30 days (`cleanupPeriodDays`) with no uncommitted changes, untracked files, or unpushed commits → removed.
- Worktrees with changes → kept, listed in session picker.

Since worktrees live in the host project directory (via bind mount), this cleanup happens whether or not the container is running — next time the user runs `claude` on the host or `claude-forge start`, orphans are cleaned.

---

## 9. Multi-Instance Support

Multiple `claude-forge start` in different directories run in parallel:

| Resource | Naming | Isolation |
|---|---|---|
| Docker network | `forge_net_<project-id>_<session-id>` | Separate per instance |
| Agent container | `forge-agent-<project-id>-<session-id>` | Unique name |
| Gateway container | `forge-gateway-<project-id>-<session-id>` | Unique name |
| Session history | `~/.claude-forge/<project-id>/<session-id>.jsonl` | Per-project, per-session |

`claude-forge status` lists all running instances across projects.

---

## 10. Container Images

### Image Strategy

The **agent image** is published on GHCR.io with everything included (runtimes, Claude Code, forge-gh, tools). The CLI pulls this image and rebuilds locally only when the remote image digest changes or on explicit `claude-forge build`.

The **gateway image** is also published on GHCR.io (Go binary + SSH/git tooling).

Published on GHCR.io (free for public repos, unlimited storage and pulls):

- `ghcr.io/michael-freling/claude-forge-agent:latest` — full agent image with Claude Code + runtimes + tools
- `ghcr.io/michael-freling/claude-forge-gateway:latest` — gateway binary

### Agent Image

```dockerfile
FROM ubuntu:24.04

# Node.js LTS (via nodesource)
RUN apt-get update && apt-get install -y nodejs npm \
    && rm -rf /var/lib/apt/lists/*

# Python 3
RUN apt-get update && apt-get install -y \
    python3 python3-pip python3-venv \
    && rm -rf /var/lib/apt/lists/*

# Go (latest)
COPY --from=golang:1.26 /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"

# Docker daemon + CLI
RUN apt-get update && apt-get install -y docker.io \
    && rm -rf /var/lib/apt/lists/*

# Common CLIs used by Claude Code
RUN apt-get update && apt-get install -y \
    bash git curl jq make ripgrep \
    tar unzip openssh-client \
    && rm -rf /var/lib/apt/lists/*

# Claude Code
RUN npm install -g @anthropic-ai/claude-code

# forge-gh (aliased as gh)
COPY forge-gh /usr/local/bin/forge-gh
RUN ln -s /usr/local/bin/forge-gh /usr/local/bin/gh

RUN useradd -m -s /bin/bash user
USER user
WORKDIR /work
ENTRYPOINT ["claude"]
```

Includes: Node.js LTS, Python 3, Go 1.26, Docker (daemon + CLI), and common CLIs (ripgrep, git, jq, curl, make, tar, unzip, ssh-client, bash). `gh` is symlinked to `forge-gh` so Claude Code's natural `gh` commands work transparently through the gateway.

Docker daemon (`dockerd`) runs inside the agent container for `docker build` and similar operations. The host Docker socket is NOT mounted.

### Gateway Image (Multi-Stage)

```dockerfile
# Build stage
FROM golang:1.26-alpine AS build
COPY . /src
WORKDIR /src
RUN go build -o /gateway ./cmd/claude-forge-gateway/

# Runtime stage
FROM alpine:3.21
RUN apk add --no-cache bash openssh-client git
COPY --from=build /gateway /usr/local/bin/gateway
RUN adduser -D -s /bin/bash user
USER user
ENTRYPOINT ["gateway"]
```

Includes `bash` and `ssh` for debugging and for SSH-based GitHub auth.

### CI Workflow

GitHub Actions on push to main: build both images, push to GHCR.io with `latest` + version tag. Cost: $0.

### Nightly Rebuild and Security

A scheduled nightly CI job rebuilds both images to pick up:
- Claude Code releases (agent image)
- OS security patches
- Runtime version updates (Go, Node.js LTS, Python)
- Dependency updates

Security verification for published images:
- **Cosign signing**: All images are signed with cosign. Users can verify image provenance before pulling.
- **SHA256 checksums**: Published alongside each release for offline verification.

### Local Image Management

The CLI manages the local agent image:
- **(b) Auto-rebuild**: On `claude-forge start`, checks if the local image digest matches the remote. Pulls/rebuilds if the remote has updated.
- **(c) Manual rebuild**: `claude-forge build` forces a pull of the latest image regardless of digest match.

---

## 11. Security Model

### What the Agent Can Do

- Read and write files in the project workspace
- Run any shell command (`--dangerously-skip-permissions`)
- Git clone/fetch/pull from any repository (via gateway, which adds auth)
- Git push to the project's own repository only (via gateway)
- GitHub API read operations on any repo (via `gh` / `forge-gh`)
- GitHub API write operations on project repo only (via `gh` / `forge-gh`)
- Direct internet access for package installation (npm, pip, go get, cargo, etc.)

### What the Agent Cannot Do

| Blocked action | Enforcement |
|---|---|
| Git push to other repos | Gateway rejects `git-receive-pack` for non-project repos |
| GitHub write ops on other repos | Gateway API server rejects non-project write requests |
| Read host SSH keys | Not mounted in agent container — only gateway has them |
| Read host git credentials | Not mounted in agent container |
| Send Slack/email/calendar | No MCP servers configured |
| Access host Docker socket | Not mounted |
| Escape container | Non-root user, no host socket mounts, network isolation |
| Modify user's Claude Code config | Config mounted read-only |
| Access credentials file | Not mounted; only the token is passed via env var |

### Why `--dangerously-skip-permissions` Is Safe

Claude Code's permission system protects the host machine. Inside the container:
- File destruction is limited to the mounted workspace (recoverable via git).
- Git writes are gateway-scoped to one repo.
- No SSH keys, git credentials, or cloud config accessible.
- No MCP servers to abuse.
- `--privileged` is required for DinD but the container has no host socket mounts — the Docker daemon inside is isolated from the host.

The container IS the permission boundary.

---

## 12. Design Decisions

Resolved questions from earlier iterations:

1. **GitHub auth strategy for gateway.** The gateway uses ALL available auth methods: SSH keys from `~/.ssh/`, PAT from `~/.config/gh/hosts.yml`, and OAuth token from `gh auth`. It tries them in order and uses whatever succeeds.

2. **`forge-gh` schema discovery.** Supports all `gh` subcommands via schema discovery. Does NOT support `gh api` style raw API access — only structured commands. If a command isn't in the schema, it returns an error.

3. **Credential refresh.** `~/.claude/.credentials.json` is bind-mounted (read-write) into each container so that Claude Code can refresh expired OAuth tokens autonomously during long-running sessions. The credential file grants access only to the Claude API — the same service the container is already using — so mounting it does not expand the blast radius beyond what the agent already has via its access token.

4. **Agent image management.** Auto-rebuild when remote digest changes (b) + explicit `claude-forge build` command (c).

5. **Docker daemon.** Runs `dockerd` inside the agent container (DinD). Requires relaxed security: the agent container runs with `--privileged` (or equivalent capabilities) to support the Docker daemon. The host Docker socket is still NOT mounted — isolation is maintained because the in-container daemon has no access to host containers or images.
