# Claude Code Secure Sandbox — Implementation Prompt

Build a secure, isolated sandbox for running Claude Code with full network tool access (git, gh, kubectl, docker, gcloud) while blocking destructive commands and preventing secret exfiltration. All tools must be free/open-source. The sandbox must preserve Claude Code's conversation history and configuration from the host.

## Architecture Overview

Two Docker containers on an internal network (no direct internet):
1. **sandbox**: Runs Claude Code with all CLI tools and DCG hook
2. **proxy**: Squid forward proxy with domain allowlist — the sandbox's only path to the internet

Host credentials are bind-mounted read-only. Claude Code config/history is bind-mounted read-write at the same absolute path so project history is preserved across sessions.

```
Host
├── ~/.claude/              → same path in container (rw)
├── ~/src/.../project/      → same path in container (rw)
├── ~/.config/gcloud/       → /creds/gcloud (ro)
├── ~/.kube/config          → /creds/kube/config (ro)
├── ~/.ssh/                 → /creds/ssh (ro)
├── ~/.gitconfig            → /creds/.gitconfig (ro)
├── ~/.config/gh/           → /creds/gh (ro)
│
└── Docker internal network (no direct internet)
    ├── [proxy]  Squid — allowlist-only egress to internet
    └── [sandbox] Claude Code + DCG — routes all traffic through proxy
```

## Files to Create

### 1. `Dockerfile`

Base: `ubuntu:24.04`

Install:
- `git`, `gh` (GitHub CLI), `curl`, `wget`, `jq`, `unzip`, `openssh-client`
- `kubectl` (latest stable from dl.k8s.io)
- `gcloud` CLI (from cloud.google.com/sdk)
- `docker` CLI only (not the daemon — just the client binary from download.docker.com)
- Node.js 22 LTS (from nodesource or official)
- Claude Code via `npm install -g @anthropic-ai/claude-code`
- Destructive Command Guard (DCG) — clone from `https://github.com/Dicklesworthstone/destructive_command_guard` and build with cargo (install Rust toolchain, build release binary, copy to /usr/local/bin, then remove Rust toolchain to keep image small)

Create a non-root user matching the host user:
```dockerfile
ARG USER_NAME=michael
ARG USER_ID=1000
ARG GROUP_ID=1000
RUN groupadd -g $GROUP_ID $USER_NAME && useradd -u $USER_ID -g $GROUP_ID -m -s /bin/bash $USER_NAME
USER $USER_NAME
```

### 2. `entrypoint.sh`

Runs as the container entrypoint. Does the following:

1. Symlink read-only credentials to expected locations:
   ```bash
   mkdir -p ~/.config ~/.kube
   ln -sf /creds/gcloud ~/.config/gcloud
   ln -sf /creds/kube/config ~/.kube/config
   ln -sf /creds/ssh ~/.ssh
   ln -sf /creds/.gitconfig ~/.gitconfig
   ln -sf /creds/gh ~/.config/gh
   ```

2. Configure proxy environment variables so all CLI tools route through the squid proxy:
   ```bash
   export http_proxy=http://proxy:3128
   export https_proxy=http://proxy:3128
   export HTTP_PROXY=http://proxy:3128
   export HTTPS_PROXY=http://proxy:3128
   export no_proxy=localhost,127.0.0.1
   ```

3. Configure git to use the proxy:
   ```bash
   git config --global http.proxy http://proxy:3128
   git config --global https.proxy http://proxy:3128
   ```

4. Exec into bash or claude (depending on args):
   ```bash
   exec "$@"
   ```

### 3. `proxy/Dockerfile`

Base: `ubuntu:24.04`
Install: `squid`
Copy in `squid.conf` and `allowed-domains.txt`
Expose port 3128
CMD: `squid -N` (foreground mode)

### 4. `proxy/allowed-domains.txt`

One domain per line. These are the ONLY domains the sandbox can reach:

```
# GitHub
.github.com
.githubusercontent.com
.githubassets.com

# Container registries
.gcr.io
.docker.io
.docker.com
registry.hub.docker.com
ghcr.io
.pkg.dev

# Google Cloud APIs
.googleapis.com
accounts.google.com
oauth2.googleapis.com
.cloud.google.com

# Package registries
.npmjs.org
.npmjs.com
registry.npmjs.org
.pypi.org
files.pythonhosted.org

# Claude Code / Anthropic
.anthropic.com
.claude.ai

# System
.ubuntu.com
.debian.org
```

Also add a placeholder section for user-specific additions:
```
# Add your K8s API server here:
# k8s-api.example.com

# Add other allowed domains below:
```

### 5. `proxy/squid.conf`

```squid
# Squid config for Claude Code sandbox egress filtering
http_port 3128

# Load allowed domains
acl allowed_domains dstdomain "/etc/squid/allowed-domains.txt"

# Allow CONNECT (HTTPS) to allowed domains only
acl SSL_ports port 443
acl CONNECT method CONNECT
http_access allow CONNECT allowed_domains
http_access allow allowed_domains

# Deny everything else
http_access deny all

# Logging
access_log stdio:/dev/stdout
cache_log stdio:/dev/stderr

# No caching — just proxy
cache deny all

# Security
via off
forwarded_for delete
reply_header_access X-Forwarded-For deny all
```

### 6. `dcg/config.toml`

DCG configuration. Enable these security packs (blocking destructive commands):

```toml
[general]
fail_open = false  # IMPORTANT: deny on parse errors, not allow
log_level = "info"

[security_packs]
git = true         # blocks: git push --force, git reset --hard, git clean -fd
cloud_gcp = true   # blocks: gcloud container clusters delete, gcloud projects delete, gcloud compute instances delete
kubernetes = true   # blocks: kubectl delete namespace, kubectl delete node, kubectl delete clusterrole
docker = true       # blocks: docker system prune, docker rm -f $(docker ps -aq), docker rmi -f
filesystem = true   # blocks: rm -rf /, rm -rf ~, chmod -R 777 /
database = true     # blocks: DROP DATABASE, DROP TABLE, TRUNCATE
terraform = true    # blocks: terraform destroy

[custom_rules]
# Add project-specific blocked commands here:
# [[custom_rules.deny]]
# pattern = "gcloud sql instances delete"
# reason = "production database protection"
```

### 7. `claude-settings.json`

Claude Code settings to install inside the container at `~/.claude/settings.json` (merge with existing if present, don't overwrite user's existing settings):

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "dcg check --config /etc/dcg/config.toml"
          }
        ]
      }
    ]
  }
}
```

Note: The DCG hook receives the command as JSON on stdin from Claude Code and exits non-zero to block it. Check DCG docs for exact integration syntax — the above is the general pattern. Adapt the command and config path based on how DCG actually accepts input.

### 8. `docker-compose.yml`

```yaml
services:
  proxy:
    build: ./proxy
    container_name: claude-sandbox-proxy
    networks:
      sandbox-net:
    restart: unless-stopped

  sandbox:
    build: .
    container_name: claude-sandbox
    entrypoint: ["/entrypoint.sh"]
    command: ["bash"]  # or "claude" to launch directly
    stdin_open: true
    tty: true
    working_dir: "${PROJECT_DIR}"
    environment:
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
    volumes:
      # Claude Code config and history (rw — must persist)
      - ${HOME}/.claude:${HOME}/.claude
      # Project directory at SAME PATH for history consistency
      - ${PROJECT_DIR}:${PROJECT_DIR}
      # Credentials (read-only)
      - ${HOME}/.config/gcloud:/creds/gcloud:ro
      - ${HOME}/.kube/config:/creds/kube/config:ro
      - ${HOME}/.ssh:/creds/ssh:ro
      - ${HOME}/.gitconfig:/creds/.gitconfig:ro
      - ${HOME}/.config/gh:/creds/gh:ro
    networks:
      sandbox-net:
    security_opt:
      - no-new-privileges
    cap_drop:
      - ALL
    depends_on:
      - proxy
    # No ports exposed — sandbox has no direct internet access
    # Docker-in-Docker: if needed, add a dind sidecar or use sysbox runtime

  # Optional: Docker-in-Docker sidecar
  # Uncomment if you need `docker build` / `docker run` inside the sandbox
  # dind:
  #   image: docker:dind
  #   container_name: claude-sandbox-dind
  #   privileged: true
  #   environment:
  #     - DOCKER_TLS_CERTDIR=""
  #   networks:
  #     sandbox-net:
  #   # Then in sandbox, set DOCKER_HOST=tcp://dind:2375

networks:
  sandbox-net:
    driver: bridge
    internal: true  # NO direct internet access for containers on this network
    # The proxy container needs internet — it gets a second network:

  # Proxy's external network
  proxy-external:
    driver: bridge
```

**Important**: The proxy container needs to be on BOTH networks — `sandbox-net` (internal, to receive requests from sandbox) and `proxy-external` (to reach the internet). Update the proxy service:

```yaml
  proxy:
    networks:
      sandbox-net:
      proxy-external:
```

The sandbox container is ONLY on `sandbox-net` (internal) so it has zero direct internet access.

### 9. `sandbox.sh`

Launcher script. Usage: `./sandbox.sh [project-directory] [--claude]`

```bash
#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="${1:-$(pwd)}"
PROJECT_DIR="$(cd "$PROJECT_DIR" && pwd)"  # resolve to absolute path

LAUNCH_CLAUDE=false
if [[ "${2:-}" == "--claude" ]] || [[ "${1:-}" == "--claude" ]]; then
    LAUNCH_CLAUDE=true
    if [[ "${1:-}" == "--claude" ]]; then
        PROJECT_DIR="$(pwd)"
    fi
fi

# Validate required env var
if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
    echo "Error: ANTHROPIC_API_KEY environment variable is required"
    exit 1
fi

export PROJECT_DIR
export ANTHROPIC_API_KEY

CMD="bash"
if $LAUNCH_CLAUDE; then
    CMD="claude"
fi

cd "$SCRIPT_DIR"

docker compose run --rm \
    -e PROJECT_DIR="$PROJECT_DIR" \
    -e ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
    sandbox $CMD
```

Make it executable: `chmod +x sandbox.sh`

## Build and run

```bash
# Build
docker compose build

# Launch interactive shell in sandbox
./sandbox.sh /home/michael/src/github.com/my-project

# Launch Claude Code directly in sandbox
./sandbox.sh /home/michael/src/github.com/my-project --claude
```

## Security layers summary

| Layer | Component | What it prevents |
|---|---|---|
| Process isolation | Docker container, `no-new-privileges`, `cap_drop: ALL` | Sandbox escape to host |
| Network isolation | Docker `internal` network | Direct internet access from sandbox |
| Egress filtering | Squid proxy with domain allowlist | Secret exfiltration to arbitrary servers |
| Command policy | DCG PreToolUse hook | Destructive commands (delete cluster, force push, rm -rf) |
| Credential protection | Read-only bind mounts | Modification/deletion of host credentials |
| History preservation | Same-path bind mount of `~/.claude/` | Claude Code sees same project history as host |

## Known limitations and mitigations

1. **Exfil via allowed channels**: Claude could push secrets to a GitHub repo. Mitigation: use a fine-grained GitHub token scoped to specific repos only.
2. **DCG bypass via obfuscation**: Encoding commands in base64 or using aliases might bypass DCG pattern matching. Mitigation: DCG's `fail_open = false` setting blocks unparseable commands. Additionally, the egress proxy limits where data can go even if a command executes.
3. **gcloud token refresh**: gcloud credentials mounted read-only means token refresh writes may fail. Mitigation: run `gcloud auth print-access-token` on the host and pass the short-lived token as an env var instead, or mount just the credentials JSON read-only and let gcloud cache tokens in a writable tmpdir inside the container.
4. **Docker-in-Docker**: If using the DinD sidecar, it runs privileged. Mitigation: it's on the internal network with no direct internet, and DCG blocks destructive docker commands. For stronger isolation, use Sysbox runtime instead.
5. **Claude Code settings merge**: The DCG hook configuration needs to be merged with any existing user settings in `~/.claude/settings.json`, not overwrite them. The entrypoint should handle this (read existing, merge hooks, write back).
