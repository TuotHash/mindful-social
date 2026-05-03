#!/usr/bin/env bash
# Initialize the project-local Postgres data directory.
# Idempotent: does nothing if PGDATA already exists.
set -euo pipefail

: "${PGDATA:?PGDATA must be set (run inside nix develop)}"

if [ -d "$PGDATA" ]; then
  echo "PGDATA already initialized at $PGDATA"
  exit 0
fi

initdb \
  --auth-host=trust \
  --auth-local=trust \
  --encoding=UTF8 \
  --locale=C \
  --username="$USER"

echo "Initialized $PGDATA"
