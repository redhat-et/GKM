#!/bin/sh

set -e

# GKM compatibility mode: when run as a drop-in for gkm-extract, translate the
# GKM_IMAGE_URL / GKM_CACHE_DIR / NO_GPU / GO_LOG env vars into mcv CLI flags.
# This is triggered when no CLI args are passed but GKM_IMAGE_URL is set.
if [ "$#" -eq 0 ] && [ -n "$GKM_IMAGE_URL" ]; then
    CACHE_DIR="${GKM_CACHE_DIR:-/mnt/kernel-caches}"
    INIT_FILE="${CACHE_DIR}/.initialized"

    mkdir -p "${CACHE_DIR}"
    chown -R 1000:1000 "${CACHE_DIR}" 2>/dev/null || true

    if [ -f "${INIT_FILE}" ]; then
        exit 0
    fi

    ARGS="--extract --image ${GKM_IMAGE_URL} --dir ${CACHE_DIR}"
    [ "$NO_GPU" = "true" ] && ARGS="${ARGS} --no-gpu"
    [ -n "$GO_LOG" ]      && ARGS="${ARGS} --log-level ${GO_LOG}"

    touch "${INIT_FILE}"
    # shellcheck disable=SC2086
    if ! /mcv ${ARGS}; then
        rm -f "${INIT_FILE}"
        sleep 300
        exit 1
    fi
    exit 0
fi

exec /mcv "$@"
