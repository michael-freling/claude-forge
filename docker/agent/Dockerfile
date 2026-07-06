FROM ubuntu:24.04

# Avoid interactive prompts during package installation
ENV DEBIAN_FRONTEND=noninteractive

# Node.js LTS (via nodesource)
RUN apt-get update && apt-get install -y ca-certificates curl gnupg \
    && mkdir -p /etc/apt/keyrings \
    && curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg \
    && echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_22.x nodistro main" > /etc/apt/sources.list.d/nodesource.list \
    && apt-get update && apt-get install -y nodejs \
    && rm -rf /var/lib/apt/lists/*

# Python 3
RUN apt-get update && apt-get install -y \
    python3 python3-pip python3-venv \
    && rm -rf /var/lib/apt/lists/*

# Go (copy from official image)
COPY --from=golang:1.25 /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/home/user/go"
ENV PATH="${GOPATH}/bin:${PATH}"

# Docker daemon + CLI (for DinD)
RUN apt-get update && apt-get install -y docker.io \
    && rm -rf /var/lib/apt/lists/*

# Common CLIs used by Claude Code.
# Install git from ppa:git-core/ppa to get >= 2.48, which is needed for
# worktree.useRelativePaths so Claude Code's --worktree creates a .git file that
# resolves correctly both inside the container (cwd=/work) and on the host.
RUN apt-get update && apt-get install -y software-properties-common \
    && add-apt-repository -y ppa:git-core/ppa \
    && apt-get update && apt-get install -y \
    bash git curl jq make ripgrep \
    tar unzip openssh-client \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user (entrypoint adjusts UID/GID at startup)
RUN useradd -m -s /bin/bash user

# Claude Code (latest) via the standalone installer, installed as the runtime
# user. An npm -g install lives in a root-owned prefix, and Claude Code warns
# "Auto-update failed: no write permission to npm prefix" at startup even with
# auto-updates disabled. The native install under /home/user is writable by
# the user (the entrypoint chowns /home/user), so the warning never fires;
# DISABLE_AUTOUPDATER=1 set by the orchestrator keeps the version installed
# at image build time (nothing pins a specific version here).
USER user
RUN HOME=/home/user bash -c 'curl -fsSL https://claude.ai/install.sh | bash'
USER root
ENV PATH="/home/user/.local/bin:${PATH}"
RUN ln -sf /home/user/.local/bin/claude /usr/local/bin/claude

# Copy claude-forge binary (built in CI or locally)
COPY claude-forge /usr/local/bin/claude-forge

# Entrypoint handles UID/GID mapping and runs as the user
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

WORKDIR /work
ENTRYPOINT ["entrypoint.sh"]
CMD ["claude"]
