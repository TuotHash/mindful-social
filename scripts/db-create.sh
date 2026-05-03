#!/usr/bin/env bash
# Create the mindful_social database. Run once after Postgres is up.
set -euo pipefail

: "${PGHOST:?PGHOST must be set (run inside nix develop)}"
: "${PGPORT:?PGPORT must be set (run inside nix develop)}"
: "${PGDATABASE:?PGDATABASE must be set (run inside nix develop)}"

if psql -h "$PGHOST" -p "$PGPORT" -d postgres -tc \
    "SELECT 1 FROM pg_database WHERE datname = '$PGDATABASE'" | grep -q 1; then
  echo "database '$PGDATABASE' already exists"
else
  createdb -h "$PGHOST" -p "$PGPORT" "$PGDATABASE"
  echo "created database '$PGDATABASE'"
fi
