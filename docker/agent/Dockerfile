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
# resolves correctly both inside the container (cwd=/home/user/work) and on the
# host.
RUN apt-get update && apt-get install -y software-properties-common \
    && add-apt-repository -y ppa:git-core/ppa \
    && apt-get update && apt-get install -y \
    bash git curl jq make ripgrep \
    tar unzip openssh-client \
    && rm -rf /var/lib/apt/lists/*

# Create the non-root user as UID/GID 1000, the usual UID of the first user on
# a Linux host (ubuntu:24.04 ships an "ubuntu" user there, removed first). The
# image's default USER is this user, so a container started without --user
# (OpenShell, a plain `docker run`, or a claude-forge release older than the
# --user change) still runs as the host user on most machines and can read
# owner-only files such as .claude/settings.local.json in the project.
# claude-forge itself passes --user with the host's UID:GID, so other UIDs work
# too; root-started DinD sessions are remapped by the entrypoint.
RUN userdel -r ubuntu 2>/dev/null || true \
    && groupadd -g 1000 user \
    && useradd -m -s /bin/bash -u 1000 -g 1000 user

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

# The host project is bind-mounted here (see internal/forge/layout). Created in
# the image so a run without the mount still has a writable working directory.
RUN mkdir -p /home/user/work && chown user:user /home/user/work

# The container runs as the host user's UID:GID, which is not the UID `user`
# was created with. Pre-create the parents of claude-forge's mount targets
# (Docker would otherwise create them root-owned) and let any UID write under
# the home directory, so Claude Code can keep its state there.
RUN mkdir -p /home/user/.claude /home/user/.config /home/user/.cache \
        /home/user/.local/share /home/user/go/bin /home/user/go/pkg \
    && chown -R user:user /home/user \
    && chmod -R a+rwX /home/user

# Default to the non-root user. Docker-in-Docker sessions override this with
# root (see entrypoint.sh); sandboxes such as OpenShell refuse UID/GID 0.
USER user

WORKDIR /home/user/work
ENTRYPOINT ["entrypoint.sh"]
CMD ["claude"]
