#!/usr/bin/env bash
# Start a local SearXNG instance for AI node drafting on 127.0.0.1:8888,
# with the JSON API enabled (see settings.yml). Point the app at it with
# SEARXNG_URL=http://127.0.0.1:8888.
#
# Dev convenience only — SearXNG is Linux-oriented, so on macOS this runs it
# in a container. Works with either Podman or Docker (auto-detected; override
# with CONTAINER_ENGINE). In production run it natively via the NixOS
# services.searx module instead. Binds loopback only.
set -euo pipefail

cd "$(dirname "$0")"

PORT="${SEARXNG_PORT:-8888}"

ENGINE="${CONTAINER_ENGINE:-}"
if [ -z "$ENGINE" ]; then
  if command -v podman >/dev/null 2>&1; then
    ENGINE=podman
  elif command -v docker >/dev/null 2>&1; then
    ENGINE=docker
  else
    echo "error: no container engine found; install podman or docker, or set CONTAINER_ENGINE" >&2
    exit 1
  fi
fi

# Fully-qualified image so Podman doesn't prompt for short-name resolution;
# Docker accepts it too.
exec "$ENGINE" run --rm \
  -p "127.0.0.1:${PORT}:8080" \
  -v "$PWD/settings.yml:/etc/searxng/settings.yml:ro" \
  -e "SEARXNG_BASE_URL=http://localhost:${PORT}/" \
  docker.io/searxng/searxng
