#!/bin/sh
set -e
# Prepend /mcv when arguments begin with flags so callers can write:
#   docker run quay.io/gkm/mcv:no-gpu --create --image ... --dir ...
# Explicit commands (e.g. /mcv, sh) pass through unchanged.
case "$1" in
  -*) exec /mcv "$@" ;;
  *)  exec "$@" ;;
esac
