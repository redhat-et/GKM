#!/bin/sh

set -e

# Optional GKM compatibility mode: accepts the same env vars as gkm-extract
# (GKM_IMAGE_URL, GKM_CACHE_DIR, NO_GPU, GO_LOG) and translates them into mcv
# CLI flags. GKM operator deployments normally schedule the gkm-extract Job
# image instead; this mode is for running mcv as a manual drop-in replacement.
# Triggered when no CLI args are passed but GKM_IMAGE_URL is set.
if [ "$#" -eq 0 ] && [ -n "$GKM_IMAGE_URL" ]; then
    CACHE_DIR="${GKM_CACHE_DIR:-/mnt/kernel-caches}"
    INIT_FILE="${CACHE_DIR}/.initialized"

    mkdir -p "${CACHE_DIR}"
    chown 1000:1000 "${CACHE_DIR}" 2>/dev/null || true

    if [ -f "${INIT_FILE}" ]; then
        exit 0
    fi

    ARGS="--extract --image ${GKM_IMAGE_URL} --dir ${CACHE_DIR}"
    [ "$NO_GPU" = "true" ] && ARGS="${ARGS} --no-gpu"
    [ -n "$GO_LOG" ]      && ARGS="${ARGS} --log-level ${GO_LOG}"

    # shellcheck disable=SC2086
    if ! /mcv ${ARGS}; then
        exit 1
    fi
    touch "${INIT_FILE}"
    exit 0
fi

exec /mcv "$@"
