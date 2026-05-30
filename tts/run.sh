#!/usr/bin/env bash
# Start the Kokoro TTS sidecar on 127.0.0.1:8090. Requires ./setup.sh to
# have been run at least once. Binds loopback only — production should put
# this behind a NixOS service unit or reverse proxy.
set -euo pipefail

cd "$(dirname "$0")"

HOST="${TTS_HOST:-127.0.0.1}"
PORT="${TTS_PORT:-8090}"

exec uv run uvicorn server:app --host "$HOST" --port "$PORT" --log-level info
