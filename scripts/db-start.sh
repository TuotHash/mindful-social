#!/usr/bin/env bash
# Start Postgres in the foreground.
# Listens on a unix socket in $PGHOST on $PGPORT — no TCP, no auth headaches.
set -euo pipefail

: "${PGDATA:?PGDATA must be set (run inside nix develop)}"
: "${PGHOST:?PGHOST must be set (run inside nix develop)}"
: "${PGPORT:?PGPORT must be set (run inside nix develop)}"

mkdir -p "$PGHOST"

# -h ""  disables TCP listening (we use a unix socket only)
# -k     directory for the unix socket
exec postgres -D "$PGDATA" -h "" -k "$PGHOST" -p "$PGPORT"
