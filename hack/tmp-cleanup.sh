#!/usr/bin/env bash
set -euo pipefail

# Detect OS
OS_NAME="$(uname -s)"

if [[ "$OS_NAME" == "Darwin" ]]; then
    echo "Detected macOS – performing tmp cleanup..."

    for dir in k8s-device-plugin-rocm k8s-device-plugin-nvidia; do
        if [[ -d "$dir" ]]; then
            echo "Removing $dir"
            rm -rf "$dir"
        else
            echo "$dir not found, skipping"
        fi
    done

    echo "macOS tmp cleanup completed."
else
    echo "Non-macOS detected ($OS_NAME) – skipping tmp cleanup."
fi
