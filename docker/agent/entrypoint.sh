#!/bin/bash
set -e

# Match container user UID/GID to host user if FORGE_UID/FORGE_GID are set.
# This ensures files created in mounted volumes have correct host ownership.
if [ -n "$FORGE_UID" ] && [ -n "$FORGE_GID" ]; then
    current_uid=$(id -u user)
    current_gid=$(id -g user)

    if [ "$current_gid" != "$FORGE_GID" ]; then
        groupmod -g "$FORGE_GID" -o user 2>/dev/null || true
    fi
    if [ "$current_uid" != "$FORGE_UID" ]; then
        usermod -u "$FORGE_UID" -o user 2>/dev/null || true
    fi

    chown -R user:user /home/user 2>/dev/null || true
fi

# Docker-in-Docker: start a dedicated dockerd inside this container. The host
# Docker socket is never mounted, so containers started here are invisible to
# the host daemon and to other sessions. Requires the container to run with
# --privileged (claude-forge sets this when docker.enabled is true).
if [ "$FORGE_ENABLE_DOCKER" = "1" ]; then
    dockerd > /var/log/dockerd.log 2>&1 &

    for _ in $(seq 1 30); do
        [ -S /var/run/docker.sock ] && break
        sleep 0.5
    done

    if [ -S /var/run/docker.sock ]; then
        # dockerd's socket is owned by root:docker; let the non-root user use it.
        usermod -aG docker user 2>/dev/null || true
    else
        echo "Warning: dockerd did not start; docker will be unavailable in this session" >&2
        tail -n 20 /var/log/dockerd.log >&2 || true
    fi
fi

exec runuser -u user -- "$@"
