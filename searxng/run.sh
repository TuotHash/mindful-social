#!/usr/bin/env bash
# Start a local SearXNG instance for AI node drafting on 127.0.0.1:8888,
# with the JSON API enabled (see settings.yml). Point the app at it with
# SEARXNG_URL=http://127.0.0.1:8888.
#
# Dev convenience only — SearXNG is Linux-oriented, so on macOS this uses
# Docker. In production run it natively via the NixOS services.searx module
# instead. Binds loopback only; nothing else should reach it.
set -euo pipefail

cd "$(dirname "$0")"

PORT="${SEARXNG_PORT:-8888}"

exec docker run --rm \
  -p "127.0.0.1:${PORT}:8080" \
  -v "$PWD/settings.yml:/etc/searxng/settings.yml:ro" \
  -e "SEARXNG_BASE_URL=http://localhost:${PORT}/" \
  searxng/searxng
