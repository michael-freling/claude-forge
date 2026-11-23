#!/bin/bash
set -e

# claude-forge runs the container as the host user's UID:GID (and OpenShell or
# a plain `docker run` as the image's non-root user), so there is nothing to
# remap: run the command directly. Only Docker-in-Docker sessions start as
# root, because dockerd needs it; they continue below and drop to the user.
if [ "$(id -u)" != "0" ]; then
    # FORGE_UID is only set for root-started sessions. A non-root container
    # that still gets it was started by a claude-forge release that expects
    # the entrypoint to remap the user, which is impossible without root:
    # files only the host user can read in the workspace would fail with
    # EACCES. Say so instead of failing obscurely.
    if [ -n "$FORGE_UID" ] && [ "$FORGE_UID" != "$(id -u)" ]; then
        echo "claude-forge: running as UID $(id -u), but the host user is UID $FORGE_UID; files only the host user can read will be inaccessible. Update claude-forge (it now starts the container with --user)." >&2
    fi
    exec "$@"
fi

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

    # Fix ownership of the image's own files under /home/user, but stay out of
    # the bind mounts: -xdev stops find at every mount point (the dependency
    # caches and the session directory, which the host already owns), and the
    # workspace is pruned outright so the user's working tree is never
    # rewritten on the host.
    find /home/user -xdev -path "${FORGE_WORKSPACE:-/home/user/work}" -prune \
        -o -exec chown user:user {} + 2>/dev/null || true
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
